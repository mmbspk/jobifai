package handler

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llmpolicy"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/version"
)

// GET /api/admin/overview
func (h *AdminHandlers) Overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	todayStart, _ := adminSince("today")

	var migVersion int64
	_ = h.svc.DB.QueryRowContext(ctx, `SELECT version_id FROM goose_db_version ORDER BY version_id DESC LIMIT 1`).Scan(&migVersion)

	uptimeSec := int64(0)
	if !h.svc.StartedAt.IsZero() {
		uptimeSec = int64(time.Since(h.svc.StartedAt).Seconds())
	}

	env := os.Getenv("JOBIFAI_ENV")
	if env == "" {
		env = "local"
	}

	var userCount int
	_ = h.svc.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&userCount)

	var aiCalls, aiSuccess, aiRaw, aiLoaded, aiCredits int64
	var aiAvgLat float64
	_ = h.svc.DB.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN success=1 THEN 1 ELSE 0 END),0),
			COALESCE(SUM(raw_cost_usd_micro),0), COALESCE(SUM(loaded_cost_usd_micro),0),
			COALESCE(SUM(credits_burned),0), COALESCE(AVG(latency_ms),0)
		FROM llm_usage_events WHERE created_at >= ?`, todayStart).Scan(
		&aiCalls, &aiSuccess, &aiRaw, &aiLoaded, &aiCredits, &aiAvgLat)

	auto := map[string]any{
		"applications_today": adminCountWhere(ctx, h.svc.DB, `SELECT COUNT(*) FROM jobs_applied WHERE applied_at >= ?`, todayStart),
		"skipped_today": adminCountWhere(ctx, h.svc.DB, `
			SELECT COUNT(*) FROM jobs_skipped WHERE viewed_at >= ? AND `+sqlSkippedNormalOnly, todayStart),
		"cannot_apply_today": adminCountWhere(ctx, h.svc.DB, `
			SELECT COUNT(*) FROM jobs_skipped WHERE viewed_at >= ? AND `+sqlCannotApplyFilter, todayStart),
		"pending_review_count": adminCountWhere(ctx, h.svc.DB, `SELECT COUNT(*) FROM jobs_pending_review WHERE easy_apply = 1`, nil),
		"top_matches_count":    adminCountWhere(ctx, h.svc.DB, `SELECT COUNT(*) FROM jobs_pending_review WHERE easy_apply = 0`, nil),
		"recent_llm_users_15min": adminCountWhere(ctx, h.svc.DB,
			`SELECT COUNT(DISTINCT user_id) FROM llm_usage_events WHERE created_at >= datetime('now','-15 minutes')`, nil),
	}

	gs := config.ResolveOperationalSettings(h.svc.Config, domain.SystemUserID)
	store := &llmpolicy.Store{DB: h.svc.DB}
	catalog := pricing.DefaultCatalog()
	tasks := []string{
		domain.TaskJobScoring, domain.TaskEmploymentEthics, domain.TaskResumeExtract,
		domain.TaskResumeTailoring, domain.TaskCoverLetter, domain.TaskFormAnswer,
		domain.TaskFormVision, domain.TaskApplicationQuestions,
	}
	type modelRow struct {
		Task           string `json:"task"`
		Provider       string `json:"provider"`
		Model          string `json:"model"`
		Effort         string `json:"effort"`
		Source         string `json:"source"`
		SourceLabel    string `json:"source_label"`
		PolicyOverride bool   `json:"policy_override"`
	}
	var models []modelRow
	for _, t := range tasks {
		pol, _ := store.ApprovedPolicy(t)
		res, err := llmpolicy.ResolveTaskModel(t, gs.LLM, gs.LLM.TaskModels, pol, catalog, gs.LLM.Provider)
		if err != nil {
			continue
		}
		var label string
		switch res.Source {
		case "policy_approved":
			label = "Approved policy"
		case "user_override":
			label = "User override"
		default:
			label = "Global"
		}
		models = append(models, modelRow{
			Task: t, Provider: res.Provider, Model: res.Model, Effort: res.Effort,
			Source: res.Source, SourceLabel: label, PolicyOverride: res.Source == "policy_approved",
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"system": map[string]any{
			"version":           version.Version,
			"git_sha":           version.GitSHA,
			"migration_version": migVersion,
			"uptime_seconds":    uptimeSec,
			"environment":       env,
			"server_time_utc":   time.Now().UTC().Format(time.RFC3339),
			"user_count":        userCount,
		},
		"ai_today": map[string]any{
			"calls": aiCalls, "success_calls": aiSuccess,
			"success_rate": pct(aiSuccess, aiCalls),
			"raw_cost_usd_micro": aiRaw, "loaded_cost_usd_micro": aiLoaded,
			"credits_burned": aiCredits, "avg_latency_ms": aiAvgLat,
		},
		"automation":       auto,
		"effective_models": models,
	})
}

func adminCountWhere(ctx context.Context, db *sql.DB, q string, arg any) int64 {
	var n int64
	var err error
	if arg == nil {
		err = db.QueryRowContext(ctx, q).Scan(&n)
	} else {
		err = db.QueryRowContext(ctx, q, arg).Scan(&n)
	}
	if err != nil {
		log.Error().Err(err).Str("query", q).Msg("admin metric count failed")
		return 0
	}
	return n
}

func pct(num, den int64) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}
