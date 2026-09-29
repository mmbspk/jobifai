package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/engine"
	"github.com/user/jobifai/internal/eval/policy"
	"github.com/user/jobifai/internal/llmpolicy"
	"github.com/user/jobifai/internal/pricing"
)

// GET /api/admin/models/policies
func (h *AdminHandlers) ModelsPolicies(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.DB.QueryContext(r.Context(), `
		SELECT task, COALESCE(provider,''), COALESCE(model,''), COALESCE(mode,''), COALESCE(effort,''), state
		FROM task_model_policies ORDER BY task`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	type row struct {
		Task, Provider, Model, Mode, Effort, State string
	}
	var out []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.Task, &x.Provider, &x.Model, &x.Mode, &x.Effort, &x.State); err != nil {
			continue
		}
		out = append(out, x)
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/admin/models/evals
func (h *AdminHandlers) ModelsEvalsList(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.DB.QueryContext(r.Context(), `
		SELECT id, task, dataset_version, status, COALESCE(actual_cost_usd_micro,0), created_at
		FROM model_eval_runs ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	type row struct {
		ID        string `json:"id"`
		Task      string `json:"task"`
		Dataset   string `json:"dataset"`
		Status    string `json:"status"`
		Created   string `json:"created"`
		CostMicro int64  `json:"cost_micro"`
	}
	var out []row
	for rows.Next() {
		var x row
		_ = rows.Scan(&x.ID, &x.Task, &x.Dataset, &x.Status, &x.CostMicro, &x.Created)
		out = append(out, x)
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/admin/models/evals/{id}
func (h *AdminHandlers) ModelsEvalGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var status, summary string
	err := h.svc.DB.QueryRowContext(r.Context(), `
		SELECT status, COALESCE(summary_json,'{}') FROM model_eval_runs WHERE id=?`, id).Scan(&status, &summary)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": status, "summary": summary})
}

type evalStartBody struct {
	Task           string           `json:"task"`
	Dataset        string           `json:"dataset"`
	MaxCostUSD     float64          `json:"max_cost_usd"`
	Baseline       candidate.Spec   `json:"baseline"`
	Candidates     []candidate.Spec `json:"candidates"`
}

// POST /api/admin/models/evals
func (h *AdminHandlers) ModelsEvalStart(w http.ResponseWriter, r *http.Request) {
	var body evalStartBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid json"})
		return
	}
	if body.MaxCostUSD <= 0 {
		body.MaxCostUSD = 1
	}
	if body.MaxCostUSD > 50 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "max eval budget exceeded"})
		return
	}
	svc := &engine.Service{
		DB:     h.svc.DB,
		Run:    engine.FakeRunner{},
		Config: engine.Config{MaxBudgetMicro: int64(body.MaxCostUSD * 1_000_000), MinCases: 3},
	}
	adminID := adminUserID(r)
	id, err := svc.CreateRun(r.Context(), body.Task, body.Dataset, body.Baseline, body.Candidates, body.MaxCostUSD, adminID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id})
}

type approveBody struct {
	EvalRunID string         `json:"eval_run_id"`
	Candidate candidate.Spec `json:"candidate"`
}

// POST /api/admin/models/policies/{task}/approve
func (h *AdminHandlers) ModelsPolicyApprove(w http.ResponseWriter, r *http.Request) {
	task := chi.URLParam(r, "task")
	var body approveBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid json"})
		return
	}
	approver := &policy.Approver{
		DB: h.svc.DB, Catalog: pricing.DefaultCatalog(), BaseProvider: "claude",
	}
	if err := approver.ApproveSameProvider(r.Context(), task, body.EvalRunID, adminUserID(r), body.Candidate); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

// POST /api/admin/models/policies/{task}/rollback
func (h *AdminHandlers) ModelsPolicyRollback(w http.ResponseWriter, r *http.Request) {
	task := chi.URLParam(r, "task")
	approver := &policy.Approver{DB: h.svc.DB}
	if err := approver.Rollback(r.Context(), task, adminUserID(r)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rolled_back"})
}

// GET /api/admin/models/recommendations?task=
func (h *AdminHandlers) ModelsRecommendations(w http.ResponseWriter, r *http.Request) {
	task := r.URL.Query().Get("task")
	q := `SELECT id, eval_run_id, task, outcome, reason, deployable, created_at FROM model_eval_recommendations`
	args := []any{}
	if task != "" {
		q += ` WHERE task=?`
		args = append(args, task)
	}
	q += ` ORDER BY created_at DESC LIMIT 50`
	rows, err := h.svc.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	var out []map[string]any
	for rows.Next() {
		var id, runID, t, outcome, reason, created string
		var deploy int
		_ = rows.Scan(&id, &runID, &t, &outcome, &reason, &deploy, &created)
		out = append(out, map[string]any{
			"id": id, "eval_run_id": runID, "task": t, "outcome": outcome,
			"reason": reason, "deployable": deploy == 1, "created_at": created,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/admin/models/catalog
func (h *AdminHandlers) ModelsCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"source": "builtin"})
}

func adminUserID(r *http.Request) string {
	if id := auth.UserIDFromCtx(r.Context()); id != "" {
		return id
	}
	return "admin"
}

// GET /api/admin/models/effective — shows resolved models per stable task (read-only).
func (h *AdminHandlers) ModelsEffective(w http.ResponseWriter, r *http.Request) {
	gs := domain.GeneralSettings{LLM: domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"}}
	store := &llmpolicy.Store{DB: h.svc.DB}
	tasks := []string{
		domain.TaskJobScoring, domain.TaskEmploymentEthics, domain.TaskResumeExtract,
		domain.TaskResumeTailoring, domain.TaskCoverLetter, domain.TaskFormAnswer,
		domain.TaskFormVision, domain.TaskApplicationQuestions,
	}
	type row struct {
		Task, Model, Source string
	}
	var out []row
	for _, t := range tasks {
		pol, _ := store.ApprovedPolicy(t)
		res, err := llmpolicy.ResolveTaskModel(t, gs.LLM, nil, pol, pricing.DefaultCatalog(), "claude")
		if err != nil {
			continue
		}
		out = append(out, row{Task: t, Model: res.Model, Source: res.Source})
	}
	writeJSON(w, http.StatusOK, out)
}
