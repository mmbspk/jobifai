package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/engine"
	"github.com/user/jobifai/internal/eval/policy"
	"github.com/user/jobifai/internal/eval/providers"
	"github.com/user/jobifai/internal/eval/runmeta"
	"github.com/user/jobifai/internal/llmpolicy"
	"github.com/user/jobifai/internal/pricing"
)

func (h *AdminHandlers) baseProvider() string {
	gs := config.ResolveOperationalSettings(h.svc.Config, "__default__")
	return gs.LLM.Provider
}

func (h *AdminHandlers) evalService(real bool) *engine.Service {
	svc := &engine.Service{DB: h.svc.DB, Config: engine.Config{MaxConcurrency: 3}}
	if real {
		gs := config.ResolveOperationalSettings(h.svc.Config, "__default__")
		svc.Run = engine.ProviderRunner{Factory: &providers.Factory{
			Catalog: pricing.DefaultCatalog(),
			BaseLLM: gs.LLM,
			KeyResolver: func(provider string) (string, bool) {
				if provider == gs.LLM.Provider {
					k, err := config.ResolveLLMAPIKey(h.svc.Secrets, "__default__", gs.LLM.UseProxy)
					return k, err == nil && k != ""
				}
				k, err := h.svc.Secrets.Get("__default__", "eval_"+provider+"_api_key")
				return k, err == nil && k != ""
			},
		}}
	} else {
		svc.Run = engine.FakeRunner{}
	}
	return svc
}

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
		SELECT id, task, dataset_version, status, COALESCE(actual_cost_usd_micro,0),
		       COALESCE(runner_type,'fake'), COALESCE(run_purpose,'smoke'), created_at
		FROM model_eval_runs ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	type row struct {
		ID         string `json:"id"`
		Task       string `json:"task"`
		Dataset    string `json:"dataset"`
		Status     string `json:"status"`
		RunnerType string `json:"runner_type"`
		Purpose    string `json:"run_purpose"`
		Created    string `json:"created"`
		CostMicro  int64  `json:"cost_micro"`
	}
	var out []row
	for rows.Next() {
		var x row
		_ = rows.Scan(&x.ID, &x.Task, &x.Dataset, &x.Status, &x.CostMicro, &x.RunnerType, &x.Purpose, &x.Created)
		out = append(out, x)
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/admin/models/evals/{id}
func (h *AdminHandlers) ModelsEvalGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var status, summary, runnerType, purpose, datasetHash string
	err := h.svc.DB.QueryRowContext(r.Context(), `
		SELECT status, COALESCE(summary_json,'{}'), COALESCE(runner_type,'fake'),
		       COALESCE(run_purpose,'smoke'), COALESCE(dataset_hash,'')
		FROM model_eval_runs WHERE id=?`, id).Scan(&status, &summary, &runnerType, &purpose, &datasetHash)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "status": status, "summary": summary,
		"runner_type": runnerType, "run_purpose": purpose, "dataset_hash": datasetHash,
	})
}

type evalStartBody struct {
	Task           string           `json:"task"`
	Dataset        string           `json:"dataset"`
	DatasetSource  string           `json:"dataset_source"`
	Purpose        string           `json:"purpose"`
	UseFakeRunner  bool             `json:"use_fake_runner"`
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
	runnerType := runmeta.RunnerReal
	if body.UseFakeRunner {
		runnerType = runmeta.RunnerFake
	}
	purpose := body.Purpose
	if purpose == "" {
		purpose = runmeta.PurposeSmoke
	}
	svc := h.evalService(!body.UseFakeRunner)
	id, err := svc.CreateRun(r.Context(), engine.RunParams{
		Task: body.Task, DatasetVersion: body.Dataset, DatasetSource: body.DatasetSource,
		Purpose: purpose, RunnerType: runnerType,
		Baseline: body.Baseline, Candidates: body.Candidates,
		BudgetUSD: body.MaxCostUSD, InitiatedBy: adminUserID(r),
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id, "runner_type": runnerType})
}

// POST /api/admin/models/evals/{id}/cancel
func (h *AdminHandlers) ModelsEvalCancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	svc := &engine.Service{DB: h.svc.DB}
	if err := svc.CancelRun(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
}

type approveBody struct {
	RecommendationID string `json:"recommendation_id"`
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
		DB: h.svc.DB, Catalog: pricing.DefaultCatalog(), BaseProvider: h.baseProvider(),
	}
	if err := approver.ApproveRecommendation(r.Context(), task, body.RecommendationID, adminUserID(r)); err != nil {
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
	q := `SELECT r.id, r.eval_run_id, r.task, r.outcome, r.reason, r.deployable, r.created_at,
	       COALESCE(e.runner_type,'fake'), COALESCE(e.run_purpose,'smoke')
	       FROM model_eval_recommendations r
	       JOIN model_eval_runs e ON e.id = r.eval_run_id`
	args := []any{}
	if task != "" {
		q += ` WHERE r.task=?`
		args = append(args, task)
	}
	q += ` ORDER BY r.created_at DESC LIMIT 50`
	rows, err := h.svc.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	var out []map[string]any
	for rows.Next() {
		var id, runID, t, outcome, reason, created, runnerType, purpose string
		var deploy int
		_ = rows.Scan(&id, &runID, &t, &outcome, &reason, &deploy, &created, &runnerType, &purpose)
		out = append(out, map[string]any{
			"id": id, "eval_run_id": runID, "task": t, "outcome": outcome,
			"reason": reason, "deployable": deploy == 1, "created_at": created,
			"runner_type": runnerType, "run_purpose": purpose,
			"approvable": deploy == 1 && outcome == "recommend" && runnerType == runmeta.RunnerReal && purpose == runmeta.PurposeBenchmark,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/admin/models/catalog
func (h *AdminHandlers) ModelsCatalog(w http.ResponseWriter, r *http.Request) {
	var source, lastErr, refreshed string
	_ = h.svc.DB.QueryRowContext(r.Context(), `
		SELECT COALESCE(source,'builtin'), COALESCE(last_error,''), COALESCE(last_refresh_at,'')
		FROM model_catalog_meta WHERE id=1`).Scan(&source, &lastErr, &refreshed)
	rows, err := h.svc.DB.QueryContext(r.Context(), `
		SELECT provider, model, discovery_state, COALESCE(input_price,0), COALESCE(output_price,0), deprecated
		FROM model_catalog_candidates ORDER BY last_seen_at DESC LIMIT 200`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	var candidates []map[string]any
	for rows.Next() {
		var prov, model, state string
		var inP, outP float64
		var dep int
		_ = rows.Scan(&prov, &model, &state, &inP, &outP, &dep)
		candidates = append(candidates, map[string]any{
			"provider": prov, "model": model, "state": state,
			"input_price": inP, "output_price": outP, "deprecated": dep == 1,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"approved_source": "builtin",
		"discovery_source": source,
		"last_refresh_at": refreshed,
		"last_error": lastErr,
		"candidates": candidates,
	})
}

// POST /api/admin/models/catalog/refresh
func (h *AdminHandlers) ModelsCatalogRefresh(w http.ResponseWriter, r *http.Request) {
	src := &pricing.LiteLLMSource{}
	res, err := pricing.StageLiteLLMDiscovery(r.Context(), h.svc.DB, src, pricing.DefaultCatalog())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "result": res, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res})
}

func adminUserID(r *http.Request) string {
	if id := auth.UserIDFromCtx(r.Context()); id != "" {
		return id
	}
	return "admin"
}

// GET /api/admin/models/effective
func (h *AdminHandlers) ModelsEffective(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = "__default__"
	}
	gs := config.ResolveOperationalSettings(h.svc.Config, userID)
	store := &llmpolicy.Store{DB: h.svc.DB}
	catalog := pricing.DefaultCatalog()
	tasks := []string{
		domain.TaskJobScoring, domain.TaskEmploymentEthics, domain.TaskResumeExtract,
		domain.TaskResumeTailoring, domain.TaskCoverLetter, domain.TaskFormAnswer,
		domain.TaskFormVision, domain.TaskApplicationQuestions,
	}
	type row struct {
		Task, Provider, Model, Effort, Source string
		MaxTokens, TimeoutSec                 int
		MaxCostUSD                            float64
	}
	var out []row
	for _, t := range tasks {
		pol, _ := store.ApprovedPolicy(t)
		res, err := llmpolicy.ResolveTaskModel(t, gs.LLM, gs.LLM.TaskModels, pol, catalog, gs.LLM.Provider)
		if err != nil {
			continue
		}
		out = append(out, row{
			Task: t, Provider: res.Provider, Model: res.Model, Effort: res.Effort,
			Source: res.Source, MaxTokens: res.MaxTokens, TimeoutSec: res.TimeoutSec, MaxCostUSD: res.MaxCostUSD,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
