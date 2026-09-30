package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/engine"
	"github.com/user/jobifai/internal/eval/providers"
	"github.com/user/jobifai/internal/eval/runmeta"
	"github.com/user/jobifai/internal/pricing"
)

func cmdSmokeClaude(args []string) {
	fs := flag.NewFlagSet("smoke-claude", flag.ExitOnError)
	dbPath := fs.String("db", envOr("JOBIFAI_DB", "data/jobifai.db"), "SQLite path")
	maxTotalUSD := fs.Float64("max-total-usd", 5, "hard cap on summed eval run spend")
	perRunCap := fs.Float64("max-run-usd", 0.85, "per-task eval engine budget ceiling")
	onlyTask := fs.String("task", "", "run a single task (default: full smoke sequence)")
	skipSonnet55 := fs.Bool("skip-sonnet-5-5", true, "omit claude-sonnet-5-5 candidates (e.g. unsupported on proxy)")
	targeted := fs.Bool("targeted", false, "run corrected-case remediation sequence (<= ~$1 total)")
	_ = fs.Parse(args)

	sqldb, err := db.Open(*dbPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer func() { _ = sqldb.Close() }()

	machineKey, err := config.MachineKey(sqldb)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	cfgStore := config.NewStore(sqldb)
	secrets := config.NewSecretsStore(sqldb, machineKey)
	gs := config.ResolveOperationalSettings(cfgStore, domain.SystemUserID)

	factory := &providers.Factory{
		Catalog:     pricing.DefaultCatalog(),
		EvalPricing: &pricing.EvalCatalog{Approved: pricing.DefaultCatalog(), DB: sqldb},
		BaseLLM:     gs.LLM,
		KeyResolver: func(provider string) (string, bool) {
			if provider == gs.LLM.Provider {
				k, err := config.ResolveLLMAPIKey(secrets, domain.SystemUserID, gs.LLM.UseProxy)
				return k, err == nil && k != ""
			}
			k, err := secrets.Get(domain.SystemUserID, "eval_"+provider+"_api_key")
			return k, err == nil && k != ""
		},
	}
	evalCat := factory.EvalPricing
	svc := &engine.Service{
		DB:          sqldb,
		Run:         engine.ProviderRunner{Factory: factory},
		Config:      engine.Config{MaxConcurrency: 2},
		EvalPricing: evalCat,
	}

	baseline := candidate.Spec{
		Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium",
		MaxTokens: 8192, TimeoutSec: 120,
	}

	var tasks []smokeTaskPlan
	if *targeted {
		tasks = targetedRemediationTasks()
		*maxTotalUSD = 1.0
		if *perRunCap > 0.35 {
			*perRunCap = 0.35
		}
	} else {
		tasks = []smokeTaskPlan{
			{domain.TaskJobScoring, structuredCandidates()},
			{domain.TaskEmploymentEthics, structuredCandidates()},
			{domain.TaskFormAnswer, structuredCandidates()},
			{domain.TaskFormVision, visionCandidates()},
			{domain.TaskResumeExtract, structuredCandidates()},
			{domain.TaskApplicationQuestions, structuredCandidates()},
			{domain.TaskResumeTailoring, subjectiveCandidates()},
			{domain.TaskCoverLetter, subjectiveCandidates()},
		}
	}

	var sessionSpendMicro int64
	ctx := context.Background()
	sync := false

	for _, tp := range tasks {
		if *onlyTask != "" && tp.task != *onlyTask {
			continue
		}
		cands := tp.candidates
		if *skipSonnet55 {
			cands = withoutSonnet55(cands)
		}
		if float64(sessionSpendMicro)/1_000_000 >= *maxTotalUSD {
			fmt.Printf("\n[STOP] session spend $%.4f reached cap $%.2f before %s\n",
				float64(sessionSpendMicro)/1_000_000, *maxTotalUSD, tp.task)
			break
		}
		remaining := *maxTotalUSD - float64(sessionSpendMicro)/1_000_000
		runBudget := *perRunCap
		if runBudget > remaining {
			runBudget = remaining
		}
		if runBudget < 0.05 {
			break
		}

		fmt.Printf("\n=== smoke task=%s budget=$%.2f (session $%.4f / $%.2f) ===\n",
			tp.task, runBudget, float64(sessionSpendMicro)/1_000_000, *maxTotalUSD)

		id, err := svc.CreateRun(ctx, engine.RunParams{
			Task: tp.task, DatasetVersion: "smoke", DatasetSource: dataset.SourceSynthetic,
			Purpose: runmeta.PurposeSmoke, RunnerType: runmeta.RunnerReal,
			Baseline: baseline, Candidates: cands,
			BudgetUSD: runBudget, InitiatedBy: "smoke-claude-cli",
			StartAsync: &sync,
		})
		if err != nil {
			fmt.Printf("[ABORT] CreateRun: %v\n", err)
			break
		}
		if err := svc.ExecuteRun(ctx, id); err != nil {
			fmt.Printf("[ABORT] ExecuteRun: %v\n", err)
			printRunReport(sqldb, id, baseline, *targeted)
			break
		}
		printRunReport(sqldb, id, baseline, *targeted)

		var runCost int64
		var status string
		_ = sqldb.QueryRow(`SELECT status, COALESCE(actual_cost_usd_micro,0) FROM model_eval_runs WHERE id=?`, id).
			Scan(&status, &runCost)
		sessionSpendMicro += runCost
		if status == runmeta.StatusFailed || status == runmeta.StatusBudgetExhausted {
			fmt.Printf("[WARN] run ended status=%s — review before continuing\n", status)
		}
	}

	fmt.Printf("\n=== session total raw eval spend: $%.4f (cap $%.2f) ===\n",
		float64(sessionSpendMicro)/1_000_000, *maxTotalUSD)
}

func structuredCandidates() []candidate.Spec {
	return []candidate.Spec{
		{Provider: "claude", Model: "claude-haiku-4-5-20251001", MaxTokens: 8192, TimeoutSec: 120},
		{Provider: "claude", Model: "claude-sonnet-5-5", Effort: "low", MaxTokens: 8192, TimeoutSec: 120},
		{Provider: "claude", Model: "claude-sonnet-5-5", Effort: "medium", MaxTokens: 8192, TimeoutSec: 120},
		{Provider: "claude", Model: "claude-sonnet-5-5", Effort: "high", MaxTokens: 8192, TimeoutSec: 120},
	}
}

func visionCandidates() []candidate.Spec {
	// Haiku is not used for vision in production routing; skip for form_vision smoke.
	return []candidate.Spec{
		{Provider: "claude", Model: "claude-sonnet-5-5", Effort: "low", MaxTokens: 8192, TimeoutSec: 120},
		{Provider: "claude", Model: "claude-sonnet-5-5", Effort: "medium", MaxTokens: 8192, TimeoutSec: 120},
	}
}

func subjectiveCandidates() []candidate.Spec {
	return []candidate.Spec{
		{Provider: "claude", Model: "claude-sonnet-5-5", Effort: "medium", MaxTokens: 8192, TimeoutSec: 120},
	}
}

type smokeTaskPlan struct {
	task       string
	candidates []candidate.Spec
}

func targetedRemediationTasks() []smokeTaskPlan {
	haiku := []candidate.Spec{
		{Provider: "claude", Model: "claude-haiku-4-5-20251001", MaxTokens: 4096, TimeoutSec: 120},
	}
	sonnet46 := []candidate.Spec{
		{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium", MaxTokens: 8192, TimeoutSec: 120},
	}
	return []smokeTaskPlan{
		{domain.TaskEmploymentEthics, haiku},
		{domain.TaskFormAnswer, haiku},
		{domain.TaskResumeExtract, haiku},
		{domain.TaskApplicationQuestions, sonnet46},
		{domain.TaskResumeTailoring, sonnet46},
		{domain.TaskCoverLetter, sonnet46},
	}
}

func printRunReport(sqldb *sql.DB, runID string, baseline candidate.Spec, verboseCases bool) {
	var status string
	var planned, completed int
	var spent int64
	_ = sqldb.QueryRow(`
		SELECT status, cases_planned, cases_completed, COALESCE(actual_cost_usd_micro,0)
		FROM model_eval_runs WHERE id=?`, runID).Scan(&status, &planned, &completed, &spent)
	fmt.Printf("run_id=%s status=%s cases=%d/%d run_cost_usd=%.4f\n",
		runID, status, completed, planned, float64(spent)/1_000_000)

	rows, err := sqldb.Query(`
		SELECT COALESCE(provider,''), COALESCE(requested_model,''), COALESCE(actual_model,''), COALESCE(effort,''),
		       COUNT(*),
		       SUM(CASE WHEN success=1 THEN 1 ELSE 0 END),
		       SUM(CASE WHEN validation_errors='[]' OR validation_errors='null' OR validation_errors='' THEN 1 ELSE 0 END),
		       SUM(COALESCE(input_tokens,0)), SUM(COALESCE(output_tokens,0)),
		       SUM(COALESCE(raw_cost_usd_micro,0)),
		       AVG(latency_ms)
		FROM model_eval_results WHERE eval_run_id=?
		GROUP BY provider, requested_model, actual_model, effort
		ORDER BY requested_model, effort`, runID)
	if err != nil {
		fmt.Println("results:", err)
		return
	}
	defer func() { _ = rows.Close() }()

	type agg struct {
		provider, req, actual, effort string
		n, ok, pass                   int
		inTok, outTok, cost           int64
		meanLat                       float64
	}
	var groups []agg
	for rows.Next() {
		var g agg
		_ = rows.Scan(&g.provider, &g.req, &g.actual, &g.effort, &g.n, &g.ok, &g.pass,
			&g.inTok, &g.outTok, &g.cost, &g.meanLat)
		groups = append(groups, g)
	}

	var baseAgg *agg
	for i := range groups {
		g := &groups[i]
		if g.req == baseline.Model && strings.EqualFold(g.effort, baseline.Effort) {
			baseAgg = g
		}
	}

	for _, g := range groups {
		meanCost := float64(0)
		if g.n > 0 {
			meanCost = float64(g.cost) / float64(g.n) / 1_000_000
		}
		costLabel := fmt.Sprintf("cost_usd=%.4f", float64(g.cost)/1_000_000)
		if g.cost == 0 && g.ok > 0 && (g.inTok+g.outTok) > 0 {
			costLabel = "cost=UNRESOLVED pricing_unresolved=true"
		}
		line := fmt.Sprintf("  model=%s effort=%q actual=%s cases=%d api_ok=%d validator_pass=%d in=%d out=%d %s mean_cost/case=%.5f mean_lat_ms=%.0f",
			g.req, g.effort, g.actual, g.n, g.ok, g.pass, g.inTok, g.outTok, costLabel, meanCost, g.meanLat)
		if baseAgg != nil && baseAgg.cost > 0 && g.req != baseline.Model {
			line += fmt.Sprintf(" cost_delta_vs_baseline=%+.1f%%", (float64(g.cost)/float64(baseAgg.cost)-1)*100)
		}
		fmt.Println(line)
	}

	scoringConfusion(sqldb, runID)
	printRecommendations(sqldb, runID)
	printCriticalFailures(sqldb, runID)
	if verboseCases {
		printCaseDetails(sqldb, runID)
	}
}

func printCaseDetails(sqldb *sql.DB, runID string) {
	rows, err := sqldb.Query(`
		SELECT case_id, COALESCE(requested_model,''), COALESCE(actual_model,''),
		       COALESCE(input_tokens,0), COALESCE(output_tokens,0), COALESCE(raw_cost_usd_micro,0),
		       COALESCE(latency_ms,0), COALESCE(validation_errors,'[]'), COALESCE(metric_json,'{}')
		FROM model_eval_results WHERE eval_run_id=? ORDER BY case_id, requested_model`, runID)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var caseID, req, actual, valErrs, metricJSON string
		var inTok, outTok, cost, lat int64
		_ = rows.Scan(&caseID, &req, &actual, &inTok, &outTok, &cost, &lat, &valErrs, &metricJSON)
		var m map[string]any
		_ = json.Unmarshal([]byte(metricJSON), &m)
		resolved, _ := m["pricing_resolved"].(bool)
		canon, _ := m["canonical_pricing_model"].(string)
		src, _ := m["pricing_source"].(string)
		rawActual, _ := m["actual_model_raw"].(string)
		if rawActual == "" {
			rawActual = actual
		}
		costOut := fmt.Sprintf("%.6f", float64(cost)/1_000_000)
		if !resolved && inTok+outTok > 0 {
			costOut = "UNRESOLVED"
		}
		pass := valErrs == "[]" || valErrs == "" || valErrs == "null"
		fmt.Printf("  case=%s requested=%s actual_raw=%s canonical_pricing=%s pricing_source=%s pricing_resolved=%v tokens=%d/%d cost_usd=%s latency_ms=%d validator_pass=%v errors=%s\n",
			caseID, req, rawActual, canon, src, resolved, inTok, outTok, costOut, lat, pass, valErrs)
	}
}

func scoringConfusion(sqldb *sql.DB, runID string) {
	rows, err := sqldb.Query(`
		SELECT requested_model, COALESCE(effort,''), metric_json
		FROM model_eval_results WHERE eval_run_id=?`, runID)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	type cell struct{ tp, tn, fp, fn int }
	byModel := map[string]*cell{}
	for rows.Next() {
		var model, effort, mj string
		_ = rows.Scan(&model, &effort, &mj)
		var m map[string]any
		_ = json.Unmarshal([]byte(mj), &m)
		if m["scoring_cell"] == nil {
			continue
		}
		key := model + "|" + effort
		if byModel[key] == nil {
			byModel[key] = &cell{}
		}
		switch m["scoring_cell"] {
		case "tp":
			byModel[key].tp++
		case "tn":
			byModel[key].tn++
		case "fp":
			byModel[key].fp++
		case "fn":
			byModel[key].fn++
		}
	}
	for k, c := range byModel {
		total := c.tp + c.tn + c.fp + c.fn
		if total == 0 {
			continue
		}
		acc := float64(c.tp+c.tn) / float64(total)
		var fnr, fpr float64
		if c.tp+c.fn > 0 {
			fnr = float64(c.fn) / float64(c.tp+c.fn)
		}
		if c.tn+c.fp > 0 {
			fpr = float64(c.fp) / float64(c.tn+c.fp)
		}
		fmt.Printf("  scoring %s: TP=%d TN=%d FP=%d FN=%d accuracy=%.2f fn_rate=%.2f fp_rate=%.2f\n",
			k, c.tp, c.tn, c.fp, c.fn, acc, fnr, fpr)
	}
}

func printRecommendations(sqldb *sql.DB, runID string) {
	rows, err := sqldb.Query(`
		SELECT outcome, deployable, reason, candidate_json, metrics_json
		FROM model_eval_recommendations WHERE eval_run_id=?`, runID)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var outcome, reason, candJSON, metricsJSON string
		var deploy int
		_ = rows.Scan(&outcome, &deploy, &reason, &candJSON, &metricsJSON)
		fmt.Printf("  recommendation outcome=%s deployable=%d reason=%q\n", outcome, deploy, reason)
	}
}

func printCriticalFailures(sqldb *sql.DB, runID string) {
	rows, err := sqldb.Query(`
		SELECT case_id, requested_model, COALESCE(effort,''), validation_errors
		FROM model_eval_results
		WHERE eval_run_id=? AND validation_errors NOT IN ('[]','','null')
		LIMIT 20`, runID)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var caseID, model, effort, errs string
		_ = rows.Scan(&caseID, &model, &effort, &errs)
		fmt.Printf("  critical/validation case=%s model=%s effort=%s errors=%s\n", caseID, model, effort, errs)
	}
}

func withoutSonnet55(in []candidate.Spec) []candidate.Spec {
	var out []candidate.Spec
	for _, s := range in {
		if strings.Contains(s.Model, "sonnet-5-5") {
			continue
		}
		out = append(out, s)
	}
	return out
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func cmdMatcherRetest(args []string) {
	fs := flag.NewFlagSet("matcher-retest", flag.ExitOnError)
	dbPath := fs.String("db", envOr("JOBIFAI_DB", "data/jobifai.db"), "SQLite path")
	_ = fs.Parse(args)
	svc, sqldb := evalService(*dbPath)
	defer func() { _ = sqldb.Close() }()
	baseline := candidate.Spec{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium", MaxTokens: 8192, TimeoutSec: 120}
	sync := false
	for _, tp := range []struct {
		task    string
		budget  float64
		cands   []candidate.Spec
	}{
		{domain.TaskApplicationQuestions, 0.15, []candidate.Spec{baseline}},
		{domain.TaskFormAnswer, 0.10, []candidate.Spec{{Provider: "claude", Model: "claude-haiku-4-5-20251001", MaxTokens: 4096, TimeoutSec: 120}}},
	} {
		id, err := svc.CreateRun(context.Background(), engine.RunParams{
			Task: tp.task, DatasetVersion: "smoke", DatasetSource: dataset.SourceSynthetic,
			Purpose: runmeta.PurposeSmoke, RunnerType: runmeta.RunnerReal,
			Baseline: baseline, Candidates: tp.cands, BudgetUSD: tp.budget,
			InitiatedBy: "matcher-retest-cli", StartAsync: &sync,
		})
		if err != nil {
			fmt.Println(err)
			continue
		}
		_ = svc.ExecuteRun(context.Background(), id)
		printRunReport(sqldb, id, baseline, true)
		printSyntheticFailures(sqldb, id)
	}
}

func cmdSmokeFormVisionHaiku(args []string) {
	fs := flag.NewFlagSet("smoke-form-vision-haiku", flag.ExitOnError)
	dbPath := fs.String("db", envOr("JOBIFAI_DB", "data/jobifai.db"), "SQLite path")
	maxUSD := fs.Float64("max-usd", 0.35, "budget cap")
	_ = fs.Parse(args)
	svc, sqldb := evalService(*dbPath)
	defer func() { _ = sqldb.Close() }()
	baseline := candidate.Spec{Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium", MaxTokens: 8192, TimeoutSec: 120}
	cands := []candidate.Spec{{Provider: "claude", Model: "claude-haiku-4-5-20251001", MaxTokens: 8192, TimeoutSec: 120}}
	sync := false
	id, err := svc.CreateRun(context.Background(), engine.RunParams{
		Task: domain.TaskFormVision, DatasetVersion: "smoke", DatasetSource: dataset.SourceSynthetic,
		Purpose: runmeta.PurposeSmoke, RunnerType: runmeta.RunnerReal,
		Baseline: baseline, Candidates: cands, BudgetUSD: *maxUSD,
		InitiatedBy: "smoke-form-vision-haiku", StartAsync: &sync,
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	_ = svc.ExecuteRun(context.Background(), id)
	printRunReport(sqldb, id, baseline, true)
	printSyntheticFailures(sqldb, id)
}

func printSyntheticFailures(sqldb *sql.DB, runID string) {
	rows, err := sqldb.Query(`
		SELECT case_id, requested_model, COALESCE(effort,''), validation_errors, COALESCE(output_text,''), metric_json
		FROM model_eval_results
		WHERE eval_run_id=? AND validation_errors NOT IN ('[]','','null')
		ORDER BY case_id`, runID)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var caseID, model, effort, valErrs, out, mj string
		_ = rows.Scan(&caseID, &model, &effort, &valErrs, &out, &mj)
		if len(out) > 600 {
			out = out[:600] + "…"
		}
		fmt.Printf("  [synthetic-failure] case=%s model=%s effort=%s errors=%s output=%q\n", caseID, model, effort, valErrs, out)
	}
}

func printJobScoringBenchmarkSummary(sqldb *sql.DB, runID string) {
	var summary string
	_ = sqldb.QueryRow(`SELECT COALESCE(summary_json,'{}') FROM model_eval_runs WHERE id=?`, runID).Scan(&summary)
	fmt.Printf("summary=%s\n", summary)
	scoringConfusion(sqldb, runID)
	printRecommendations(sqldb, runID)
}

var _ = time.Second
