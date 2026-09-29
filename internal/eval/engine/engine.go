package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/recommend"
	"github.com/user/jobifai/internal/eval/validators"
	"github.com/user/jobifai/internal/llm"
)

type acc struct {
	spec                           candidate.Spec
	passes, successes, critical, n int
	latencies, costs               []int64
}

// Runner executes LLM calls for eval (non-billing clients).
type Runner interface {
	RunCase(ctx context.Context, task string, spec candidate.Spec, c dataset.Case) (output string, obs llm.UsageObservation, err error)
}

// Config controls eval execution limits.
type Config struct {
	MaxBudgetMicro int64
	MaxConcurrency int
	MinCases       int
}

// Service executes eval runs against SQLite.
type Service struct {
	DB     *sql.DB
	Run    Runner
	Config Config
}

// ExecuteRun loads dataset, runs candidates, persists results, writes recommendation.
func (s *Service) ExecuteRun(ctx context.Context, runID string) error {
	run, err := s.loadRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status != "pending" && run.Status != "running" {
		return nil
	}
	_ = s.markStatus(ctx, runID, "running", "")
	bundle, err := dataset.Load(run.Task, run.DatasetVersion)
	if err != nil {
		return s.failRun(ctx, runID, err)
	}
	cands, err := candidate.ParseList(run.CandidateJSON)
	if err != nil {
		return s.failRun(ctx, runID, err)
	}
	baseline := candidate.Spec{Provider: run.BaselineProvider, Model: run.BaselineModel, Effort: run.BaselineEffort}
	all := append([]candidate.Spec{baseline}, cands...)
	if s.Config.MaxConcurrency <= 0 {
		s.Config.MaxConcurrency = 3
	}
	sem := make(chan struct{}, s.Config.MaxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var spent int64
	aggregates := map[string]*acc{}

	for _, spec := range all {
		aggregates[spec.ID()] = &acc{spec: spec}
	}

	for _, spec := range all {
		for _, c := range bundle.Cases {
			if ctx.Err() != nil {
				break
			}
			mu.Lock()
			if s.Config.MaxBudgetMicro > 0 && spent >= s.Config.MaxBudgetMicro {
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
				mu.Lock()
				defer mu.Unlock()
				spent += obs.RawCostMicro
				success := err == nil
				vr := validators.Validate(run.Task, out, c.Expect, c.Critical)
				if err != nil {
					vr.Errors = append(vr.Errors, err.Error())
				}
				lat := obs.LatencyMS
				_ = s.insertResult(ctx, runID, spec, c, out, obs, success, vr, lat)
				a := aggregates[spec.ID()]
				a.n++
				if success {
					a.successes++
				}
				if vr.Pass {
					a.passes++
				}
				if vr.CriticalFail {
					a.critical++
				}
				a.costs = append(a.costs, obs.RawCostMicro)
				a.latencies = append(a.latencies, lat)
			}(spec, c)
		}
	}
	wg.Wait()
	baseKey := baseline.ID()
	baseM := metricsFromAcc(aggregates[baseKey])
	for k, a := range aggregates {
		if k == baseKey {
			continue
		}
		fm := metricsFromAcc(a)
		fm.QualityDelta = fm.DetPassRate - baseM.DetPassRate
		if baseM.TotalCostMicro > 0 {
			fm.CostDeltaPct = (float64(fm.TotalCostMicro)/float64(baseM.TotalCostMicro) - 1) * 100
		}
		cross := fm.Candidate.Provider != baseline.Provider
		rec := recommend.Decide(run.Task, baseM, fm, s.Config.MinCases, 0, -0.02, cross)
		_ = s.insertRecommendation(ctx, runID, rec)
	}
	summary, _ := json.Marshal(map[string]any{"spent_micro": spent, "cases": len(bundle.Cases)})
	_, err = s.DB.ExecContext(ctx, `
		UPDATE model_eval_runs SET status='completed', completed_at=CURRENT_TIMESTAMP,
			actual_cost_usd_micro=?, summary_json=? WHERE id=?`, spent, string(summary), runID)
	return err
}

func metricsFromAcc(a *acc) recommend.Metrics {
	if a == nil || a.n == 0 {
		return recommend.Metrics{}
	}
	return recommend.Aggregate(a.spec, a.passes, a.successes, a.critical, a.latencies, a.costs)
}

type runRow struct {
	Task             string
	DatasetVersion   string
	BaselineProvider string
	BaselineModel    string
	BaselineEffort   string
	CandidateJSON    string
	Status           string
}

func (s *Service) loadRun(ctx context.Context, id string) (runRow, error) {
	var r runRow
	err := s.DB.QueryRowContext(ctx, `
		SELECT task, dataset_version, COALESCE(baseline_provider,''), baseline_model,
		       COALESCE(baseline_effort,''), COALESCE(candidate_spec_json,'[]'), status
		FROM model_eval_runs WHERE id=?`, id).Scan(
		&r.Task, &r.DatasetVersion, &r.BaselineProvider, &r.BaselineModel,
		&r.BaselineEffort, &r.CandidateJSON, &r.Status)
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
		UPDATE model_eval_runs SET status='failed', error_message=?, completed_at=CURRENT_TIMESTAMP WHERE id=?`,
		err.Error(), id)
	return err
}

func (s *Service) insertResult(ctx context.Context, runID string, spec candidate.Spec, c dataset.Case, out string, obs llm.UsageObservation, success bool, vr validators.Result, lat int64) error {
	errs, _ := json.Marshal(vr.Errors)
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO model_eval_results (
			id, eval_run_id, case_id, model, success, score, metric_json,
			input_tokens, output_tokens, raw_cost_usd_micro, latency_ms, validation_errors,
			provider, requested_model, actual_model, effort, deterministic_score, actual_model_verified
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		uuid.NewString(), runID, c.ID, spec.Model, boolInt(success), vr.DeterministicScore, "{}",
		obs.Tokens.InputTokens, obs.Tokens.OutputTokens, obs.RawCostMicro, lat, string(errs),
		spec.Provider, spec.Model, obs.ActualModel, spec.Effort, vr.DeterministicScore, boolInt(obs.ActualModelVerified),
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

// CreateRun inserts a pending eval run row.
func (s *Service) CreateRun(ctx context.Context, task, datasetVersion string, baseline candidate.Spec, cands []candidate.Spec, budgetUSD float64, by string) (string, error) {
	id := uuid.NewString()
	cj, _ := json.Marshal(cands)
	budgetMicro := int64(budgetUSD * 1_000_000)
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO model_eval_runs (
			id, task, baseline_model, candidate_models, dataset_version, status,
			baseline_provider, baseline_effort, dataset_name, candidate_spec_json, max_budget_usd_micro, initiated_by
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, task, baseline.Model, string(cj), datasetVersion, "pending",
		baseline.Provider, baseline.Effort, datasetVersion, string(cj), budgetMicro, by,
	)
	if err != nil {
		return "", err
	}
	go func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_ = s.ExecuteRun(ctx2, id)
	}()
	return id, nil
}

// Ensure domain import for task constants in runner implementations.
var _ = domain.TaskJobScoring
