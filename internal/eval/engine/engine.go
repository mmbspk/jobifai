package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/recommend"
	"github.com/user/jobifai/internal/eval/runmeta"
	"github.com/user/jobifai/internal/eval/validators"
	"github.com/user/jobifai/internal/llm"
)

type acc struct {
	spec                           candidate.Spec
	passes, successes, critical, n int
	scoringTP, scoringTN, scoringFP, scoringFN int
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
	DB     *sql.DB
	Run    Runner
	Config Config
}

// ExecuteRun loads dataset, runs candidates, persists results, writes recommendations.
func (s *Service) ExecuteRun(ctx context.Context, runID string) error {
	run, err := s.loadRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status != runmeta.StatusPending && run.Status != runmeta.StatusRunning {
		return nil
	}
	if err := s.markStatus(ctx, runID, runmeta.StatusRunning, ""); err != nil {
		return s.failRun(ctx, runID, err)
	}
	bundle, err := dataset.Load(dataset.LoadRequest{
		Task: run.Task, Version: run.DatasetVersion, Source: run.DatasetSource,
	})
	if err != nil {
		return s.failRun(ctx, runID, err)
	}
	_, err = s.DB.ExecContext(ctx, `
		UPDATE model_eval_runs SET dataset_name=?, dataset_hash=?, cases_planned=? WHERE id=?`,
		run.DatasetVersion, bundle.Manifest.SHA256, len(bundle.Cases)*(1+len(run.Candidates)), runID)
	if err != nil {
		return s.failRun(ctx, runID, err)
	}
	cands := run.Candidates
	baseline := candidate.Spec{Provider: run.BaselineProvider, Model: run.BaselineModel, Effort: run.BaselineEffort}
	all := append([]candidate.Spec{baseline}, cands...)
	concurrency := s.Config.MaxConcurrency
	if concurrency <= 0 {
		concurrency = 3
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var spent int64
	var persistErr error
	var completed int
	budget := run.MaxBudgetMicro
	aggregates := map[string]*acc{}
	for _, spec := range all {
		aggregates[spec.ID()] = &acc{spec: spec}
	}

	stopScheduling := false
	for _, spec := range all {
		for _, c := range bundle.Cases {
			if ctx.Err() != nil || stopScheduling {
				break
			}
			if s.cancelRequested(ctx, runID) {
				stopScheduling = true
				break
			}
			mu.Lock()
			if budget > 0 && spent >= budget {
				stopScheduling = true
				mu.Unlock()
				break
			}
			mu.Unlock()
			wg.Add(1)
			sem <- struct{}{}
			go func(spec candidate.Spec, c dataset.Case) {
				defer wg.Done()
				defer func() { <-sem }()
				out, obs, err := s.Run.RunCase(ctx, run.Task, spec, c)
				vr := validators.Validate(run.Task, out, c.Expect, c.Critical)
				if err != nil {
					vr.Errors = append(vr.Errors, err.Error())
				}
				mu.Lock()
				defer mu.Unlock()
				if persistErr != nil {
					return
				}
				if err := s.insertResult(ctx, runID, spec, c, obs, err == nil, vr); err != nil {
					persistErr = err
					return
				}
				spent += obs.RawCostMicro
				completed++
				a := aggregates[spec.ID()]
				a.n++
				if err == nil {
					a.successes++
				}
				if vr.Pass {
					a.passes++
				}
				if vr.CriticalFail {
					a.critical++
				}
				switch vr.Metrics["scoring_cell"] {
				case "tp":
					a.scoringTP++
				case "tn":
					a.scoringTN++
				case "fp":
					a.scoringFP++
				case "fn":
					a.scoringFN++
				}
				a.costs = append(a.costs, obs.RawCostMicro)
				a.latencies = append(a.latencies, obs.LatencyMS)
			}(spec, c)
		}
	}
	wg.Wait()
	if _, err := s.DB.ExecContext(ctx, `UPDATE model_eval_runs SET cases_completed=? WHERE id=?`, completed, runID); err != nil {
		return s.failRun(ctx, runID, fmt.Errorf("cases_completed: %w", err))
	}
	if persistErr != nil {
		return s.failRun(ctx, runID, persistErr)
	}
	finalStatus := runmeta.StatusCompleted
	if ctx.Err() != nil || s.cancelRequested(ctx, runID) {
		finalStatus = runmeta.StatusCancelled
	} else if stopScheduling && budget > 0 && spent >= budget {
		finalStatus = runmeta.StatusBudgetExhausted
	}
	baseM := metricsFromAcc(aggregates[baseline.ID()])
	var candMetrics []recommend.Metrics
	for k, a := range aggregates {
		if k == baseline.ID() {
			continue
		}
		candMetrics = append(candMetrics, metricsFromAcc(a))
	}
	smokeOrFake := run.RunnerType != runmeta.RunnerReal || run.Purpose != runmeta.PurposeBenchmark
	minCases := runmeta.MinBenchmarkCases(run.Task)
	if smokeOrFake {
		minCases = 1_000_000 // force non-deployable
	}
	recs := recommend.SelectAll(recommend.SelectInput{
		Task: run.Task, Baseline: baseM, Candidates: candMetrics,
		MinCases: minCases, MaxCritical: 0,
		SmokeOrFake: smokeOrFake, QualityFloor: recommend.DefaultQualityFloor,
	})
	if finalStatus != runmeta.StatusCompleted {
		for i := range recs {
			recs[i].Deployable = false
			if recs[i].Outcome == recommend.OutcomeRecommend {
				recs[i].Outcome = recommend.OutcomeManualReview
				recs[i].Reason = fmt.Sprintf("run ended with status %s; not deployable", finalStatus)
			}
		}
	}
	for _, rec := range recs {
		if err := s.insertRecommendation(ctx, runID, rec); err != nil {
			return s.failRun(ctx, runID, fmt.Errorf("recommendation persist: %w", err))
		}
	}
	summary, _ := json.Marshal(map[string]any{
		"spent_micro": spent, "cases": len(bundle.Cases), "completed": completed,
	})
	if _, err = s.DB.ExecContext(ctx, `
		UPDATE model_eval_runs SET status=?, completed_at=CURRENT_TIMESTAMP,
			actual_cost_usd_micro=?, summary_json=? WHERE id=?`,
		finalStatus, spent, string(summary), runID); err != nil {
		return s.failRun(ctx, runID, fmt.Errorf("completion update: %w", err))
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
	return recommend.Aggregate(a.spec, a.passes, a.successes, a.critical, a.latencies, a.costs, a.scoringTP, a.scoringTN, a.scoringFP, a.scoringFN)
}

type runRow struct {
	Task             string
	DatasetVersion   string
	DatasetSource    string
	BaselineProvider string
	BaselineModel    string
	BaselineEffort   string
	Candidates       []candidate.Spec
	Status           string
	RunnerType       string
	Purpose          string
	MaxBudgetMicro   int64
}

func (s *Service) loadRun(ctx context.Context, id string) (runRow, error) {
	var r runRow
	var candJSON string
	err := s.DB.QueryRowContext(ctx, `
		SELECT task, dataset_version, COALESCE(dataset_source,'synthetic'),
		       COALESCE(baseline_provider,''), baseline_model, COALESCE(baseline_effort,''),
		       COALESCE(candidate_spec_json,'[]'), status, COALESCE(runner_type,'fake'),
		       COALESCE(run_purpose,'smoke'), COALESCE(max_budget_usd_micro,0)
		FROM model_eval_runs WHERE id=?`, id).Scan(
		&r.Task, &r.DatasetVersion, &r.DatasetSource,
		&r.BaselineProvider, &r.BaselineModel, &r.BaselineEffort,
		&candJSON, &r.Status, &r.RunnerType, &r.Purpose, &r.MaxBudgetMicro)
	if err != nil {
		return r, err
	}
	r.Candidates, err = candidate.ParseList(candJSON)
	return r, err
}

func (s *Service) markStatus(ctx context.Context, id, status, msg string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE model_eval_runs SET status=?, error_message=?, started_at=COALESCE(started_at,CURRENT_TIMESTAMP) WHERE id=?`,
		status, msg, id)
	return err
}

func (s *Service) failRun(ctx context.Context, id string, err error) error {
	_, _ = s.DB.ExecContext(ctx, `
		UPDATE model_eval_runs SET status=?, error_message=?, completed_at=CURRENT_TIMESTAMP WHERE id=?`,
		runmeta.StatusFailed, err.Error(), id)
	return err
}

func (s *Service) insertResult(ctx context.Context, runID string, spec candidate.Spec, c dataset.Case, obs llm.UsageObservation, success bool, vr validators.Result) error {
	errs, _ := json.Marshal(vr.Errors)
	metrics, _ := json.Marshal(vr.Metrics)
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO model_eval_results (
			id, eval_run_id, case_id, model, success, score, metric_json,
			input_tokens, output_tokens, raw_cost_usd_micro, latency_ms, validation_errors,
			provider, requested_model, actual_model, effort, deterministic_score, actual_model_verified,
			candidate_max_tokens, candidate_timeout_sec
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		uuid.NewString(), runID, c.ID, spec.Model, boolInt(success), vr.DeterministicScore, string(metrics),
		obs.Tokens.InputTokens, obs.Tokens.OutputTokens, obs.RawCostMicro, obs.LatencyMS, string(errs),
		spec.Provider, spec.Model, obs.ActualModel, spec.Effort, vr.DeterministicScore, boolInt(obs.ActualModelVerified),
		spec.MaxTokens, spec.TimeoutSec,
	)
	return err
}

func (s *Service) insertRecommendation(ctx context.Context, runID string, rec recommend.Recommendation) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO model_eval_recommendations (id, eval_run_id, task, outcome, baseline_json, candidate_json, metrics_json, reason, deployable)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		uuid.NewString(), runID, rec.Task, rec.Outcome,
		recommend.MustJSON(rec.Baseline), recommend.MustJSON(rec.Candidate), recommend.MustJSON(rec.Metrics),
		rec.Reason, boolInt(rec.Deployable),
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
