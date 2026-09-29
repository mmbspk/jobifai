package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/eval/runmeta"
	"github.com/user/jobifai/internal/eval/validators"
)

// RunParams describes a new eval run.
type RunParams struct {
	Task           string
	DatasetVersion string
	DatasetSource  string
	Purpose        string
	RunnerType     string
	Baseline       candidate.Spec
	Candidates     []candidate.Spec
	BudgetUSD      float64
	InitiatedBy    string
	// StartAsync runs ExecuteRun in a background goroutine (default true).
	StartAsync     *bool
}

// CreateRun validates dataset and inserts a pending run.
func (s *Service) CreateRun(ctx context.Context, p RunParams) (string, error) {
	if p.RunnerType == "" {
		return "", fmt.Errorf("runner_type required")
	}
	if p.Purpose == "" {
		p.Purpose = runmeta.PurposeSmoke
	}
	if !validators.HasValidator(p.Task) {
		return "", fmt.Errorf("no validator registered for task %q", p.Task)
	}
	bundle, err := dataset.Load(dataset.LoadRequest{
		Task: p.Task, Version: p.DatasetVersion, Source: p.DatasetSource,
	})
	if err != nil {
		return "", err
	}
	budgetMicro := int64(p.BudgetUSD * 1_000_000)
	if budgetMicro <= 0 {
		return "", fmt.Errorf("budget required")
	}
	id := uuid.NewString()
	cj, _ := json.Marshal(p.Candidates)
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO model_eval_runs (
			id, task, baseline_model, candidate_models, dataset_version, status,
			baseline_provider, baseline_effort, dataset_name, dataset_hash, candidate_spec_json,
			max_budget_usd_micro, initiated_by, runner_type, run_purpose, dataset_source, cases_planned
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, p.Task, p.Baseline.Model, string(cj), p.DatasetVersion, runmeta.StatusPending,
		p.Baseline.Provider, p.Baseline.Effort, p.DatasetVersion, bundle.Manifest.SHA256, string(cj),
		budgetMicro, p.InitiatedBy, p.RunnerType, p.Purpose, p.DatasetSource, len(bundle.Cases)* (1+len(p.Candidates)),
	)
	if err != nil {
		return "", err
	}
	async := true
	if p.StartAsync != nil {
		async = *p.StartAsync
	}
	if async {
		go func() {
			ctx2, cancel := context.WithCancel(context.Background())
			Runs.Register(id, cancel)
			defer Runs.Unregister(id)
			_ = s.ExecuteRun(ctx2, id)
		}()
	}
	return id, nil
}
