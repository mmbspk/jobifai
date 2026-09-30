package handler

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	evalpolicy "github.com/user/jobifai/internal/eval/policy"
)

// GET /api/admin/operations/jobs/summary
func (h *AdminHandlers) OperationsJobsSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	writeJSON(w, http.StatusOK, map[string]any{
		"applied":          adminCountWhere(ctx, h.svc.DB, `SELECT COUNT(*) FROM jobs_applied`, nil),
		"skipped":          adminCountWhere(ctx, h.svc.DB, `SELECT COUNT(*) FROM jobs_skipped`, nil),
		"cannot_apply":     adminCountWhere(ctx, h.svc.DB, `SELECT COUNT(*) FROM jobs_skipped WHERE `+sqlCannotApplyFilter, nil),
		"pending_review":   adminCountWhere(ctx, h.svc.DB, `SELECT COUNT(*) FROM jobs_pending_review`, nil),
		"top_matches":      adminCountWhere(ctx, h.svc.DB, `SELECT COUNT(*) FROM jobs_pending_review WHERE easy_apply = 1`, nil),
		"approved_queue":   adminCountWhere(ctx, h.svc.DB, `SELECT COUNT(*) FROM jobs_approved_queue`, nil),
	})
}

// GET /api/admin/operations/jobs/recent?limit=50
func (h *AdminHandlers) OperationsJobsRecent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit, _ := adminLimitOffset(r, 50, 100)
	rows, err := h.svc.DB.QueryContext(ctx, `
		SELECT user_id, platform, company, role, suitability_score, 'applied' AS state, created_at, job_id
		FROM jobs_applied
		UNION ALL
		SELECT user_id, platform, company, role, COALESCE(suitability_score,0), 'skipped', created_at, job_id
		FROM jobs_skipped
		UNION ALL
		SELECT user_id, platform, company, role, suitability_score, 'pending_review', created_at, job_id
		FROM jobs_pending_review
		ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	var out []map[string]any
	for rows.Next() {
		var uid, platform, company, role, state, jobID string
		var score int
		var created time.Time
		if rows.Scan(&uid, &platform, &company, &role, &score, &state, &created, &jobID) != nil {
			continue
		}
		out = append(out, map[string]any{
			"user_id": uid, "platform": platform, "company": company, "role": role,
			"score": score, "state": state, "created_at": created.UTC().Format(time.RFC3339), "job_id": jobID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

// GET /api/admin/operations/scoring/recent
func (h *AdminHandlers) OperationsScoringRecent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit, _ := adminLimitOffset(r, 50, 100)
	minScore, _ := strconv.Atoi(r.URL.Query().Get("min_score"))
	maxScore, _ := strconv.Atoi(r.URL.Query().Get("max_score"))
	modelFilter := r.URL.Query().Get("model")

	rows, err := h.svc.DB.QueryContext(ctx, `
		SELECT s.user_id, s.platform, s.company, s.role, s.suitability_score,
			COALESCE(s.suitability_reasoning,''), s.created_at, s.job_id, 'skipped' AS bucket
		FROM jobs_skipped s WHERE s.suitability_score > 0
		UNION ALL
		SELECT p.user_id, p.platform, p.company, p.role, p.suitability_score,
			COALESCE(p.suitability_reasoning,''), p.created_at, p.job_id, 'pending_review'
		FROM jobs_pending_review p
		UNION ALL
		SELECT a.user_id, a.platform, a.company, a.role, COALESCE(a.suitability_score,0),
			'', a.created_at, a.job_id, 'applied'
		FROM jobs_applied a WHERE COALESCE(a.suitability_score,0) > 0
		ORDER BY created_at DESC LIMIT ?`, limit*3)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()

	type row struct {
		UserID, Platform, Company, Role, Reasoning, JobID, Bucket, Created string
		Score                                                              int
		Model                                                              string
	}
	var items []row
	for rows.Next() {
		var rrow row
		var created time.Time
		if rows.Scan(&rrow.UserID, &rrow.Platform, &rrow.Company, &rrow.Role, &rrow.Score,
			&rrow.Reasoning, &created, &rrow.JobID, &rrow.Bucket) != nil {
			continue
		}
		rrow.Created = created.UTC().Format(time.RFC3339)
		if minScore > 0 && rrow.Score < minScore {
			continue
		}
		if maxScore > 0 && rrow.Score > maxScore {
			continue
		}
		_ = h.svc.DB.QueryRowContext(ctx, `
			SELECT actual_model FROM llm_usage_events
			WHERE task='job_scoring' AND job_id=? ORDER BY created_at DESC LIMIT 1`, rrow.JobID).Scan(&rrow.Model)
		if modelFilter != "" && rrow.Model != modelFilter {
			continue
		}
		if len(items) >= limit {
			break
		}
		items = append(items, rrow)
	}

	buckets := map[string]int{"0-3": 0, "4-5": 0, "6": 0, "7": 0, "8-10": 0}
	for _, it := range items {
		switch {
		case it.Score <= 3:
			buckets["0-3"]++
		case it.Score <= 5:
			buckets["4-5"]++
		case it.Score == 6:
			buckets["6"]++
		case it.Score == 7:
			buckets["7"]++
		default:
			buckets["8-10"]++
		}
	}

	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]any{
			"user_id": it.UserID, "platform": it.Platform, "company": it.Company, "role": it.Role,
			"score": it.Score, "reasoning": truncateSafe(it.Reasoning, 280),
			"decision_bucket": it.Bucket, "scoring_model": it.Model,
			"created_at": it.Created, "job_id": it.JobID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "score_distribution": buckets})
}

func truncateSafe(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// GET /api/admin/operational-errors
func (h *AdminHandlers) OperationalErrors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period := adminPeriod(r)
	since, allTime := adminSince(period)
	where, args := llmUsageFilters(r, since, allTime)
	where += " AND success = 0"
	limit, _ := adminLimitOffset(r, 100, 200)

	rows, err := h.svc.DB.QueryContext(ctx, `
		SELECT created_at, 'llm' AS subsystem, task, COALESCE(error_code,''), user_id,
			COALESCE(job_id,''), COALESCE(automation_run_id,'')
		FROM llm_usage_events WHERE `+where+`
		ORDER BY created_at DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	var out []map[string]any
	for rows.Next() {
		var sub, task, code, uid, jobID, runID string
		var created time.Time
		if rows.Scan(&created, &sub, &task, &code, &uid, &jobID, &runID) != nil {
			continue
		}
		msg := code
		if msg == "" {
			msg = "llm_call_failed"
		}
		out = append(out, map[string]any{
			"timestamp": created.UTC().Format(time.RFC3339), "subsystem": sub, "task": task,
			"error_code": code, "message": msg, "user_id": uid, "job_id": jobID, "automation_run_id": runID,
		})
	}

	failEval, _ := h.svc.DB.QueryContext(ctx, `
		SELECT created_at, task, COALESCE(error_message,''), id FROM model_eval_runs
		WHERE status IN ('failed','budget_exhausted') ORDER BY created_at DESC LIMIT ?`, limit/2)
	if failEval != nil {
		defer func() { _ = failEval.Close() }()
		for failEval.Next() {
			var task, msg, id string
			var created time.Time
			if failEval.Scan(&created, &task, &msg, &id) == nil {
				out = append(out, map[string]any{
					"timestamp": created.UTC().Format(time.RFC3339), "subsystem": "eval",
					"task": task, "error_code": "eval_run_failed", "message": truncateSafe(msg, 200),
					"eval_run_id": id,
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"errors": out})
}

// GET /api/admin/health
func (h *AdminHandlers) Health(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	gs := config.ResolveOperationalSettings(h.svc.Config, domain.SystemUserID)
	hasKey := h.svc.Secrets.Has(domain.SystemUserID, "llm_api_key")
	hasProxy := h.svc.Secrets.Has(domain.SystemUserID, "proxy_key")
	var catSource, catErr, catRefresh string
	_ = h.svc.DB.QueryRowContext(ctx, `
		SELECT COALESCE(source,'builtin'), COALESCE(last_error,''), COALESCE(last_refresh_at,'')
		FROM model_catalog_meta WHERE id=1`).Scan(&catSource, &catErr, &catRefresh)
	stripeConfigured := h.svc.Secrets.Has(domain.SystemUserID, "stripe_secret_key") ||
		os.Getenv("STRIPE_SECRET_KEY") != ""
	webhookConfigured := h.svc.Secrets.Has(domain.SystemUserID, "stripe_webhook_secret") ||
		os.Getenv("STRIPE_WEBHOOK_SECRET") != ""

	writeJSON(w, http.StatusOK, map[string]any{
		"provider": gs.LLM.Provider, "global_model": gs.LLM.Model,
		"api_credential_configured": hasKey, "proxy_enabled": gs.LLM.UseProxy, "proxy_key_configured": hasProxy,
		"pricing_catalog_source": catSource, "pricing_catalog_last_refresh": catRefresh,
		"pricing_catalog_last_error": catErr,
		"stripe_configured": stripeConfigured, "stripe_webhook_configured": webhookConfigured,
	})
}

// GET /api/admin/models/policy-audit
func (h *AdminHandlers) ModelsPolicyAudit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	task := r.URL.Query().Get("task")
	limit, _ := adminLimitOffset(r, 50, 200)
	q := `SELECT created_at, task, changed_by, COALESCE(reason,''), COALESCE(eval_run_id,''),
		COALESCE(previous_json,''), COALESCE(new_json,'') FROM model_policy_audit`
	var args []any
	if task != "" {
		q += ` WHERE task=?`
		args = append(args, task)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := h.svc.DB.QueryContext(ctx, q, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	var out []map[string]any
	for rows.Next() {
		var created time.Time
		var t, by, reason, runID, prev, newJ string
		if rows.Scan(&created, &t, &by, &reason, &runID, &prev, &newJ) != nil {
			continue
		}
		action := reason
		if action == "" {
			action = "change"
		}
		out = append(out, map[string]any{
			"timestamp": created.UTC().Format(time.RFC3339), "task": t, "changed_by": by,
			"action": action, "previous": evalpolicy.FormatPolicyStateLabel(prev),
			"new": evalpolicy.FormatPolicyStateLabel(newJ), "eval_run_id": runID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": out})
}

// GET /api/admin/quota/summary
func (h *AdminHandlers) QuotaSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.svc.DB.QueryContext(ctx, `SELECT plan, COUNT(*) FROM user_quota GROUP BY plan`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	byPlan := map[string]int64{}
	for rows.Next() {
		var plan string
		var n int64
		if rows.Scan(&plan, &n) == nil {
			byPlan[plan] = n
		}
	}
	var creditsBurned int64
	_ = h.svc.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(credits_burned),0) FROM llm_usage_events`).Scan(&creditsBurned)
	def := domain.QuotaDefaults{EnforcementDefault: true, CreditsPerUSD: 1000, TrialCredits: 500, TrialDays: 7}
	if h.svc.Quota != nil {
		def = h.svc.Quota.LoadDefaults()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"users_by_plan": byPlan, "total_credits_burned": creditsBurned, "defaults": def,
	})
}
