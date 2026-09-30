package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"

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

func cmdBenchmarkJobScoring(args []string) {
	fs := flag.NewFlagSet("benchmark-job-scoring", flag.ExitOnError)
	dbPath := fs.String("db", envOr("JOBIFAI_DB", "data/jobifai.db"), "SQLite path")
	maxUSD := fs.Float64("max-usd", 2.0, "hard eval budget cap")
	_ = fs.Parse(args)

	svc, sqldb := evalService(*dbPath)
	defer func() { _ = sqldb.Close() }()

	baseline := candidate.Spec{
		Provider: "claude", Model: "claude-sonnet-4-6", Effort: "medium",
		MaxTokens: 8192, TimeoutSec: 120,
	}
	candidates := []candidate.Spec{
		{Provider: "claude", Model: "claude-haiku-4-5-20251001", MaxTokens: 8192, TimeoutSec: 120},
	}
	sync := false
	id, err := svc.CreateRun(context.Background(), engine.RunParams{
		Task: domain.TaskJobScoring, DatasetVersion: "full", DatasetSource: dataset.SourceSynthetic,
		Purpose: runmeta.PurposeBenchmark, RunnerType: runmeta.RunnerReal,
		Baseline: baseline, Candidates: candidates,
		BudgetUSD: *maxUSD, InitiatedBy: "benchmark-job-scoring-cli",
		StartAsync: &sync,
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Printf("benchmark run_id=%s task=job_scoring dataset=full budget=$%.2f\n", id, *maxUSD)
	if err := svc.ExecuteRun(context.Background(), id); err != nil {
		fmt.Printf("ExecuteRun error: %v\n", err)
	}
	printRunReport(sqldb, id, baseline, true)
	printSyntheticFailures(sqldb, id)
	printJobScoringBenchmarkSummary(sqldb, id)
	printJobScoringProductionGate(sqldb, id)
}

func printJobScoringProductionGate(sqldb *sql.DB, runID string) {
	var summary string
	_ = sqldb.QueryRow(`SELECT COALESCE(summary_json,'{}') FROM model_eval_runs WHERE id=?`, runID).Scan(&summary)
	var sum map[string]any
	_ = json.Unmarshal([]byte(summary), &sum)
	type modelMetrics struct {
		key                                                     string
		tp, tn, fp, fn, critFN, critFP, borderline, blSurfaced int
		costMicro                                               int64
		apiOK, apiTotal                                           int
	}
	// parse from DB
	rows, err := sqldb.Query(`
		SELECT requested_model, COALESCE(effort,''), metric_json, validation_errors, success,
			raw_cost_usd_micro, latency_ms, case_id
		FROM model_eval_results WHERE eval_run_id=?`, runID)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	by := map[string]*modelMetrics{}
	criticalCases := map[string]bool{}
	if b, err := dataset.Load(dataset.LoadRequest{Task: domain.TaskJobScoring, Version: "full", Source: dataset.SourceSynthetic}); err == nil {
		for _, c := range b.Cases {
			if c.Critical {
				criticalCases[c.ID] = true
			}
		}
	}
	for rows.Next() {
		var model, effort, mj, valErrs, caseID string
		var success int
		var cost, lat int64
		_ = rows.Scan(&model, &effort, &mj, &valErrs, &success, &cost, &lat, &caseID)
		key := model + "|" + effort
		if by[key] == nil {
			by[key] = &modelMetrics{key: key}
		}
		m := by[key]
		m.apiTotal++
		if success == 1 {
			m.apiOK++
		}
		m.costMicro += cost
		var met map[string]any
		_ = json.Unmarshal([]byte(mj), &met)
		switch met["scoring_cell"] {
		case "tp":
			m.tp++
		case "tn":
			m.tn++
		case "fp":
			m.fp++
			if criticalCases[caseID] {
				m.critFP++
			}
		case "fn":
			m.fn++
			if criticalCases[caseID] {
				m.critFN++
			}
		case "borderline":
			m.borderline++
			if met["predicted_pass"] == true {
				m.blSurfaced++
			}
		}
	}
	fmt.Println("--- production_gate_assessment (no policy written) ---")
	sonnet := by["claude-sonnet-4-6|medium"]
	haiku := by["claude-haiku-4-5-20251001|"]
	if sonnet == nil || haiku == nil {
		fmt.Println("candidate_ready_for_human_production_approval=false (missing model metrics)")
		return
	}
	classifiedRows := sonnet.tp + sonnet.tn + sonnet.fp + sonnet.fn
	hardEffectiveN := classifiedRows
	if ss, ok := sum["sample_stats"].(map[string]any); ok {
		if v, ok := ss["hard_effective_sample_size"].(float64); ok {
			hardEffectiveN = int(v)
		}
	}
	var sFNR, hFNR, sFPR, hFPR float64
	if sonnet.tp+sonnet.fn > 0 {
		sFNR = float64(sonnet.fn) / float64(sonnet.tp+sonnet.fn)
	}
	if haiku.tp+haiku.fn > 0 {
		hFNR = float64(haiku.fn) / float64(haiku.tp+haiku.fn)
	}
	if sonnet.tn+sonnet.fp > 0 {
		sFPR = float64(sonnet.fp) / float64(sonnet.tn+sonnet.fp)
	}
	if haiku.tn+haiku.fp > 0 {
		hFPR = float64(haiku.fp) / float64(haiku.tn+haiku.fp)
	}
	costSave := 0.0
	if sonnet.costMicro > 0 {
		costSave = (1 - float64(haiku.costMicro)/float64(sonnet.costMicro)) * 100
	}
	ready := hardEffectiveN >= 40 && haiku.apiOK == haiku.apiTotal &&
		hFNR <= sFNR+0.01 && haiku.critFN <= sonnet.critFN &&
		hFPR <= sFPR+0.01 && costSave >= 30
	fmt.Printf("hard_effective_n=%d classified_result_rows=%d borderline_tracked=%d\n",
		hardEffectiveN, classifiedRows, sonnet.borderline)
	fmt.Printf("FNR delta (Haiku-Sonnet): %.3f  FPR delta: %.3f  cost_save: %.1f%%\n", hFNR-sFNR, hFPR-sFPR, costSave)
	fmt.Printf("critical_FN Sonnet=%d Haiku=%d  critical_FP Sonnet=%d Haiku=%d\n",
		sonnet.critFN, haiku.critFN, sonnet.critFP, haiku.critFP)
	if ready {
		fmt.Println("candidate_ready_for_human_production_approval=true")
	} else {
		fmt.Println("candidate_ready_for_human_production_approval=false")
	}
}

func evalService(dbPath string) (*engine.Service, *sql.DB) {
	sqldb, err := db.Open(dbPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	machineKey, err := config.MachineKey(sqldb)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	cfgStore := config.NewStore(sqldb)
	secrets := config.NewSecretsStore(sqldb, machineKey)
	gs := config.ResolveOperationalSettings(cfgStore, domain.SystemUserID)
	evalCat := &pricing.EvalCatalog{Approved: pricing.DefaultCatalog(), DB: sqldb}
	factory := &providers.Factory{
		Catalog: pricing.DefaultCatalog(), EvalPricing: evalCat, BaseLLM: gs.LLM,
		KeyResolver: func(provider string) (string, bool) {
			if provider == gs.LLM.Provider {
				k, err := config.ResolveLLMAPIKey(secrets, domain.SystemUserID, gs.LLM.UseProxy)
				return k, err == nil && k != ""
			}
			k, err := secrets.Get(domain.SystemUserID, "eval_"+provider+"_api_key")
			return k, err == nil && k != ""
		},
	}
	svc := &engine.Service{
		DB: sqldb, Run: engine.ProviderRunner{Factory: factory},
		Config: engine.Config{MaxConcurrency: 3}, EvalPricing: evalCat,
	}
	return svc, sqldb
}
