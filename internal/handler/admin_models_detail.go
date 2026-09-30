package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	evalpolicy "github.com/user/jobifai/internal/eval/policy"
)

// GET /api/admin/models/policies/detail
func (h *AdminHandlers) ModelsPoliciesDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.svc.DB.QueryContext(ctx, `
		SELECT task, COALESCE(provider,''), COALESCE(model,''), COALESCE(mode,''), COALESCE(effort,''),
			COALESCE(max_tokens,0), COALESCE(timeout_sec,0), COALESCE(max_cost_usd,0),
			COALESCE(previous_json,''), COALESCE(eval_run_id,''), COALESCE(approved_by,''), COALESCE(updated_at,'')
		FROM task_model_policies ORDER BY task`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	var out []map[string]any
	for rows.Next() {
		var task, prov, model, mode, effort, prev, runID, by, updated string
		var maxTok, timeout int
		var maxCost float64
		if rows.Scan(&task, &prov, &model, &mode, &effort, &maxTok, &timeout, &maxCost, &prev, &runID, &by, &updated) != nil {
			continue
		}
		out = append(out, map[string]any{
			"task": task, "provider": prov, "model": model, "mode": mode, "effort": effort,
			"max_tokens": maxTok, "timeout_sec": timeout, "max_cost_usd": maxCost,
			"previous_state": evalpolicy.FormatPolicyStateLabel(prev),
			"previous_json_raw": prev, "eval_run_id": runID, "approved_by": by, "updated_at": updated,
			"rollback_available": prev != "",
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/admin/models/policies/{task}/rollback-preview
func (h *AdminHandlers) ModelsRollbackPreview(w http.ResponseWriter, r *http.Request) {
	task := chi.URLParam(r, "task")
	var prov, model, mode, effort, prev string
	var maxTok, timeout int
	var maxCost float64
	err := h.svc.DB.QueryRowContext(r.Context(), `
		SELECT COALESCE(provider,''), COALESCE(model,''), COALESCE(mode,''), COALESCE(effort,''),
			COALESCE(max_tokens,0), COALESCE(timeout_sec,0), COALESCE(max_cost_usd,0), COALESCE(previous_json,'')
		FROM task_model_policies WHERE task=?`, task).Scan(&prov, &model, &mode, &effort, &maxTok, &timeout, &maxCost, &prev)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "no policy"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	if prev == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "no previous policy to restore"})
		return
	}
	gs := config.ResolveOperationalSettings(h.svc.Config, domain.SystemUserID)
	globalModel := gs.LLM.Model
	restoreLabel := evalpolicy.FormatPolicyStateLabel(prev)
	writeJSON(w, http.StatusOK, map[string]any{
		"task": task,
		"current": map[string]any{"provider": prov, "model": model, "mode": mode, "effort": effort},
		"restore_label": restoreLabel,
		"restore_inherit": evalpolicy.IsPreviousPolicyInherit(prev),
		"global_model": globalModel,
		"message": rollbackPreviewMessage(task, model, restoreLabel, globalModel),
	})
}

func rollbackPreviewMessage(task, currentModel, restoreLabel, globalModel string) string {
	if restoreLabel == "Inherited global configuration" {
		return "Roll back " + task + " from " + currentModel + " to inherited global " + globalModel + "?"
	}
	return "Roll back " + task + " from " + currentModel + " to " + restoreLabel + "?"
}

// GET /api/admin/models/recommendations/{id}
func (h *AdminHandlers) ModelsRecommendationGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var task, outcome, reason, baselineJ, candJ, metricsJ, runID string
	var deploy int
	err := h.svc.DB.QueryRowContext(r.Context(), `
		SELECT task, outcome, reason, baseline_json, candidate_json, metrics_json, deployable, eval_run_id
		FROM model_eval_recommendations WHERE id=?`, id).Scan(
		&task, &outcome, &reason, &baselineJ, &candJ, &metricsJ, &deploy, &runID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
		return
	}
	var runStatus, runnerType, purpose, dataset, summary string
	_ = h.svc.DB.QueryRowContext(r.Context(), `
		SELECT status, COALESCE(runner_type,''), COALESCE(run_purpose,''), COALESCE(dataset_version,''),
			COALESCE(summary_json,'')
		FROM model_eval_runs WHERE id=?`, runID).Scan(&runStatus, &runnerType, &purpose, &dataset, &summary)
	var metrics map[string]any
	_ = json.Unmarshal([]byte(metricsJ), &metrics)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "task": task, "outcome": outcome, "reason": reason, "deployable": deploy == 1,
		"baseline": json.RawMessage(baselineJ), "candidate": json.RawMessage(candJ), "metrics": metrics,
		"eval_run_id": runID, "run_status": runStatus, "runner_type": runnerType,
		"run_purpose": purpose, "dataset": dataset, "run_summary": summary,
	})
}

// GET /api/admin/models/evals/{id}/detail — enriched eval run for admin UI.
func (h *AdminHandlers) ModelsEvalDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var task, status, summary, runnerType, purpose, datasetHash, baselineJ, candJ, created, completed string
	var budgetMicro, spentMicro, planned, completedN int64
	err := h.svc.DB.QueryRowContext(r.Context(), `
		SELECT task, status, COALESCE(summary_json,'{}'), COALESCE(runner_type,'fake'), COALESCE(run_purpose,'smoke'),
			COALESCE(dataset_hash,''), COALESCE(baseline_spec_json,'{}'), COALESCE(candidate_spec_json,'[]'),
			COALESCE(created_at,''), COALESCE(completed_at,''),
			COALESCE(max_budget_usd_micro,0), COALESCE(actual_cost_usd_micro,0),
			COALESCE(cases_planned,0), COALESCE(cases_completed,0)
		FROM model_eval_runs WHERE id=?`, id).Scan(
		&task, &status, &summary, &runnerType, &purpose, &datasetHash, &baselineJ, &candJ,
		&created, &completed, &budgetMicro, &spentMicro, &planned, &completedN)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
		return
	}
	var sum map[string]any
	_ = json.Unmarshal([]byte(summary), &sum)
	var resultCount int
	_ = h.svc.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM model_eval_results WHERE eval_run_id=?`, id).Scan(&resultCount)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "task": task, "status": status, "runner_type": runnerType, "run_purpose": purpose,
		"dataset_hash": datasetHash, "baseline": json.RawMessage(baselineJ), "candidates": json.RawMessage(candJ),
		"created_at": created, "completed_at": completed,
		"max_budget_usd_micro": budgetMicro, "spent_usd_micro": spentMicro,
		"cases_planned": planned, "cases_completed": completedN, "result_count": resultCount,
		"summary": sum,
	})
}
