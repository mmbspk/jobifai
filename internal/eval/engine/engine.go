package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/recommend"
	"github.com/user/jobifai/internal/eval/runmeta"
	"github.com/user/jobifai/internal/eval/validators"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/pricing"
)

type acc struct {
	spec                           candidate.Spec
	passes, successes, critical, n int
	scoringTP, scoringTN, scoringFP, scoringFN int
	scoringBorderline, scoringBorderlineSurfaced int
	latencies, costs               []int64
}

// Runner executes LLM calls for eval (non-billing clients).
type Runner interface {
	RunCase(ctx context.Context, task string, spec candidate.Spec, c dataset.Case) (output string, obs llm.UsageObservation, err error)
}

// Config optional operator ceilings (run DB budget is authoritative).
type Config struct {
	MaxConcurrency int
}

// Service executes eval runs against SQLite.
type Service struct {
	DB          *sql.DB
	Run         Runner
	Config      Config
	EvalPricing *pricing.EvalCatalog // optional; requested-model preflight for real runs

	persistMu sync.Mutex // serializes result inserts under parallel RunCase workers (SQLite)
	execMu    sync.Mutex // serializes ExecuteRun (SQLite lifecycle + terminal status)
}

func (s *Service) insertResultExtended(ctx context.Context, runID string, spec candidate.Spec, c dataset.Case, obs llm.UsageObservation, success bool, vr validators.Result, outputText string) error {
	charge := obs.BudgetChargeMicro
	if charge <= 0 && obs.PricingResolved {
		charge = obs.RawCostMicro
	}
	errs, _ := json.Marshal(vr.Errors)
	if vr.Metrics == nil {
		vr.Metrics = map[string]any{}
	}
	vr.Metrics["pricing_resolved"] = obs.PricingResolved
	vr.Metrics["pricing_source"] = obs.PricingSource
	vr.Metrics["canonical_pricing_model"] = obs.CanonicalPricingModel
	vr.Metrics["actual_model_raw"] = obs.ActualModelRaw
	vr.Metrics["budget_charge_usd_micro"] = charge
	if obs.PricingError != "" {
		vr.Metrics["pricing_error"] = obs.PricingError
	}
	metrics, _ := json.Marshal(vr.Metrics)
	args := []any{
		uuid.NewString(), runID, c.ID, spec.Model, boolInt(success), vr.DeterministicScore, string(metrics),
		obs.Tokens.InputTokens, obs.Tokens.OutputTokens, obs.RawCostMicro, charge, obs.LatencyMS, string(errs),
		spec.Provider, spec.Model, obs.ActualModel, spec.Effort, vr.DeterministicScore, boolInt(obs.ActualModelVerified),
		spec.MaxTokens, spec.TimeoutSec, outputText,
	}
	const q = `
		INSERT INTO model_eval_results (
			id, eval_run_id, case_id, model, success, score, metric_json,
			input_tokens, output_tokens, raw_cost_usd_micro, budget_charge_usd_micro, latency_ms, validation_errors,
			provider, requested_model, actual_model, effort, deterministic_score, actual_model_verified,
			candidate_max_tokens, candidate_timeout_sec, output_text
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		_, err = s.DB.ExecContext(ctx, q, args...)
		if err == nil || !strings.Contains(err.Error(), "locked") {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return err
}

// ExecuteRun loads dataset, runs candidates, persists results, writes recommendations.
func (s *Service) ExecuteRun(ctx context.Context, runID string) error {
	s.execMu.Lock()
	defer s.execMu.Unlock()
	persist := context.WithoutCancel(ctx)
	run, err := s.loadRun(persist, runID)
	if err != nil {
		return err
	}
	if run.Status != runmeta.StatusPending && run.Status != runmeta.StatusRunning {
		return nil
	}
	finalized := false
	defer func() {
		if finalized {
			return
		}
		persist := context.WithoutCancel(ctx)
		var st string
		if err := s.DB.QueryRowContext(persist, `SELECT status FROM model_eval_runs WHERE id=?`, runID).Scan(&st); err != nil {
			return
		}
		if st != runmeta.StatusPending && st != runmeta.StatusRunning {
			return
		}
		msg := "eval run exited before persisting terminal status"
		status := runmeta.StatusFailed
		if ctx.Err() != nil || s.cancelRequested(persist, runID) {
			status = runmeta.StatusCancelled
		}
		_, _ = s.DB.ExecContext(persist, `
			UPDATE model_eval_runs SET status=?, error_message=?, completed_at=CURRENT_TIMESTAMP WHERE id=?`,
			status, msg, runID)
	}()
	if err := s.markStatus(persist, runID, runmeta.StatusRunning, ""); err != nil {
		finalized = true
		return s.failRun(ctx, runID, err)
	}
	bundle, err := dataset.Load(dataset.LoadRequest{
		Task: run.Task, Version: run.DatasetVersion, Source: run.DatasetSource,
	})
	if err != nil {
		finalized = true
		return s.failRun(ctx, runID, err)
	}
	_, err = s.DB.ExecContext(persist, `
		UPDATE model_eval_runs SET dataset_name=?, dataset_hash=?, cases_planned=? WHERE id=?`,
		run.DatasetVersion, bundle.Manifest.SHA256, len(bundle.Cases)*(1+len(run.Candidates)), runID) // baseline + candidates
	if err != nil {
		finalized = true
		return s.failRun(ctx, runID, err)
	}
	baseline := run.Baseline
	if baseline.Model == "" {
		baseline = candidate.Spec{Provider: run.BaselineProvider, Model: run.BaselineModel, Effort: run.BaselineEffort}
	}
	run.Baseline = baseline
	workTotal := len(bundle.Cases) * (1 + len(run.Candidates))

	finalStatus, summaryMap, persistErr := s.runCases(ctx, runID, run, bundle)
	if persistErr == nil && finalStatus != "" {
		persistStatus := context.WithoutCancel(ctx)
		_, _ = s.DB.ExecContext(persistStatus, `
			UPDATE model_eval_runs SET status=?, completed_at=COALESCE(completed_at, CURRENT_TIMESTAMP)
			WHERE id=? AND status IN (?, ?)`,
			finalStatus, runID, runmeta.StatusPending, runmeta.StatusRunning)
	}
	completed := 0
	if summaryMap != nil {
		switch v := summaryMap["completed"].(type) {
		case int:
			completed = v
		case float64:
			completed = int(v)
		}
	}
	if _, err := s.DB.ExecContext(persist, `UPDATE model_eval_runs SET cases_completed=? WHERE id=?`, completed, runID); err != nil {
		finalized = true
		return s.failRun(ctx, runID, fmt.Errorf("cases_completed: %w", err))
	}
	if persistErr != nil {
		finalized = true
		return s.failRun(ctx, runID, persistErr)
	}
	if finalStatus == runmeta.StatusCompleted && completed > workTotal {
		finalized = true
		return s.failRun(ctx, runID, fmt.Errorf("completed %d exceeds planned %d case executions", completed, workTotal))
	}
	if finalStatus == runmeta.StatusFailed {
		msg, _ := summaryMap["completion_error"].(string)
		if msg == "" {
			msg = "benchmark incomplete"
		}
		finalized = true
		return s.failRun(ctx, runID, fmt.Errorf("%s", msg))
	}
	return nil
}

func (s *Service) cancelRequested(ctx context.Context, runID string) bool {
	if ctx.Err() != nil {
		return true
	}
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT cancel_requested FROM model_eval_runs WHERE id=?`, runID).Scan(&n)
	return n == 1
}

func metricsFromAcc(a *acc) recommend.Metrics {
	if a == nil || a.n == 0 {
		return recommend.Metrics{}
	}
	m := recommend.Aggregate(a.spec, a.passes, a.successes, a.critical, a.latencies, a.costs, a.scoringTP, a.scoringTN, a.scoringFP, a.scoringFN)
	m.ScoringBorderline = a.scoringBorderline
	m.ScoringBorderlineSurfaced = a.scoringBorderlineSurfaced
	return m
}

type runRow struct {
	Task             string
	DatasetVersion   string
	DatasetSource    string
	BaselineProvider string
	BaselineModel    string
	BaselineEffort   string
	Baseline         candidate.Spec
	Candidates       []candidate.Spec
	Status           string
	RunnerType       string
	Purpose          string
	MaxBudgetMicro   int64
}

func (s *Service) loadRun(ctx context.Context, id string) (runRow, error) {
	var r runRow
	var candJSON, baselineJSON string
	err := s.DB.QueryRowContext(ctx, `
		SELECT task, dataset_version, COALESCE(dataset_source,'synthetic'),
		       COALESCE(baseline_provider,''), baseline_model, COALESCE(baseline_effort,''),
		       COALESCE(candidate_spec_json,'[]'), COALESCE(baseline_spec_json,'{}'), status, COALESCE(runner_type,'fake'),
		       COALESCE(run_purpose,'smoke'), COALESCE(max_budget_usd_micro,0)
		FROM model_eval_runs WHERE id=?`, id).Scan(
		&r.Task, &r.DatasetVersion, &r.DatasetSource,
		&r.BaselineProvider, &r.BaselineModel, &r.BaselineEffort,
		&candJSON, &baselineJSON, &r.Status, &r.RunnerType, &r.Purpose, &r.MaxBudgetMicro)
	if err != nil {
		return r, err
	}
	r.Candidates, err = candidate.ParseList(candJSON)
	if err != nil {
		return r, err
	}
	if strings.TrimSpace(baselineJSON) != "" && baselineJSON != "{}" {
		_ = json.Unmarshal([]byte(baselineJSON), &r.Baseline)
	}
	return r, err
}

func (s *Service) markStatus(ctx context.Context, id, status, msg string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE model_eval_runs SET status=?, error_message=?, started_at=COALESCE(started_at,CURRENT_TIMESTAMP) WHERE id=?`,
		status, msg, id)
	return err
}

func (s *Service) failRun(ctx context.Context, id string, err error) error {
	persist := context.WithoutCancel(ctx)
	_, _ = s.DB.ExecContext(persist, `
		UPDATE model_eval_runs SET status=?, error_message=?, completed_at=CURRENT_TIMESTAMP WHERE id=?`,
		runmeta.StatusFailed, err.Error(), id)
	return err
}

func (s *Service) insertRecommendation(ctx context.Context, runID string, rec recommend.Recommendation) error {
	base, err := recommend.MarshalJSON(rec.Baseline)
	if err != nil {
		return err
	}
	cand, err := recommend.MarshalJSON(rec.Candidate)
	if err != nil {
		return err
	}
	metrics, err := recommend.MarshalJSON(rec.Metrics)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO model_eval_recommendations (id, eval_run_id, task, outcome, baseline_json, candidate_json, metrics_json, reason, deployable)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		uuid.NewString(), runID, rec.Task, rec.Outcome,
		base, cand, metrics, rec.Reason, boolInt(rec.Deployable),
	)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CancelRun requests cancellation for an active run.
func (s *Service) CancelRun(ctx context.Context, runID string) error {
	Runs.Cancel(runID)
	_, err := s.DB.ExecContext(ctx, `UPDATE model_eval_runs SET cancel_requested=1 WHERE id=?`, runID)
	return err
}

var _ = time.Second
var _ = sql.ErrNoRows
