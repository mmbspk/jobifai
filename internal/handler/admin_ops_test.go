package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/handler"
)

func seedAdminPipeline(t *testing.T, db *sql.DB, userID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO jobs_applied (id, user_id, platform, company, role, link, applied_at, suitability_score)
		VALUES ('adm-applied-1', ?, 'seek', 'Applied Co', 'Role A', 'https://example.com/a', ?, 8)`,
		userID, now)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO jobs_skipped (id, user_id, platform, company, role, link, skip_reason, suitability_score, suitability_reasoning, viewed_at)
		VALUES ('adm-skip-1', ?, 'seek', 'Skip Co', 'Role S', 'https://example.com/s', 'Below threshold', 4, 'Not a fit', ?)`,
		userID, now)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO jobs_skipped (id, user_id, platform, company, role, link, skip_reason, suitability_score, viewed_at)
		VALUES ('adm-cannot-1', ?, 'linkedin', 'Manual Co', 'Role M', 'https://example.com/m', 'easy apply: extra steps', 8, ?)`,
		userID, now)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO jobs_pending_review (job_id, user_id, company, role, platform, link, resume_path, cover_letter_path, suitability_score, easy_apply, created_at)
		VALUES ('adm-pending-1', ?, 'Review Co', 'Role P', 'seek', 'https://example.com/p', '', '', 9, 1, ?)`,
		userID, now)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO jobs_pending_review (job_id, user_id, company, role, platform, link, resume_path, cover_letter_path, suitability_score, easy_apply, created_at)
		VALUES ('adm-top-1', ?, 'Top Co', 'Role T', 'seek', 'https://example.com/t', '', '', 9, 0, ?)`,
		userID, now)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO llm_usage_events (
			id, created_at, user_id, task, provider, requested_model, actual_model, actual_model_verified,
			input_tokens, output_tokens, raw_cost_usd_micro, loaded_cost_usd_micro, credits_burned, latency_ms,
			success, correlation_id, job_id
		) VALUES (?, ?, ?, 'job_scoring', 'claude', 'claude-haiku-4-5-20251001', 'claude-haiku-4-5-20251001', 1,
			100, 20, 10, 15, 1, 50, 1, '', 'adm-skip-1')`,
		uuid.NewString(), now, userID)
	require.NoError(t, err)
}

func adminTokenFor(t *testing.T, router http.Handler, db *sql.DB, email string) string {
	t.Helper()
	token := registerAndLogin(t, router, email, "password123")
	setUserAdmin(t, db, email)
	wMe := authGet(t, router, "/api/me", token)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	return token
}

func TestAdmin_OverviewAndHealth(t *testing.T) {
	svc, db := newTestServices(t)
	svc.StartedAt = time.Now().Add(-time.Hour)
	router := handler.NewRouter(svc)
	adminToken := adminTokenFor(t, router, db, "admin-overview@example.com")

	w := authGet(t, router, "/api/admin/overview", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var overview map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&overview))
	assert.NotNil(t, overview["system"])
	assert.NotNil(t, overview["ai_today"])
	assert.NotNil(t, overview["effective_models"])

	wH := authGet(t, router, "/api/admin/health", adminToken)
	require.Equal(t, http.StatusOK, wH.Code)
}

func TestAdmin_PipelineMetricsSeeded(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	email := "admin-pipeline@example.com"
	adminToken := adminTokenFor(t, router, db, email)
	wMe := authGet(t, router, "/api/me", adminToken)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	userID := me["id"].(string)
	seedAdminPipeline(t, db, userID)

	w := authGet(t, router, "/api/admin/overview", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var overview map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&overview))
	auto := overview["automation"].(map[string]any)
	assert.Equal(t, float64(1), auto["applications_today"])
	assert.Equal(t, float64(1), auto["skipped_today"])
	assert.Equal(t, float64(1), auto["cannot_apply_today"])
	assert.Equal(t, float64(1), auto["pending_review_count"])
	assert.Equal(t, float64(1), auto["top_matches_count"])

	wSum := authGet(t, router, "/api/admin/operations/jobs/summary", adminToken)
	require.Equal(t, http.StatusOK, wSum.Code)
	var summary map[string]any
	require.NoError(t, json.NewDecoder(wSum.Body).Decode(&summary))
	assert.Equal(t, float64(1), summary["applied"])
	assert.Equal(t, float64(1), summary["skipped"])
	assert.Equal(t, float64(1), summary["cannot_apply"])
	assert.Equal(t, float64(1), summary["pending_review"])
	assert.Equal(t, float64(1), summary["top_matches"])
}

func TestAdmin_OperationsJobsRecentSeeded(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	adminToken := adminTokenFor(t, router, db, "admin-recent@example.com")
	wMe := authGet(t, router, "/api/me", adminToken)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	seedAdminPipeline(t, db, me["id"].(string))

	w := authGet(t, router, "/api/admin/operations/jobs/recent?limit=20", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	jobs := body["jobs"].([]any)
	require.NotEmpty(t, jobs)
	ids := map[string]bool{}
	for _, j := range jobs {
		row := j.(map[string]any)
		ids[row["job_id"].(string)] = true
		assert.NotEmpty(t, row["created_at"])
	}
	assert.True(t, ids["adm-applied-1"])
	assert.True(t, ids["adm-skip-1"])
}

func TestAdmin_OperationsScoringRecentSeeded(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	adminToken := adminTokenFor(t, router, db, "admin-scoring@example.com")
	wMe := authGet(t, router, "/api/me", adminToken)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	seedAdminPipeline(t, db, me["id"].(string))

	w := authGet(t, router, "/api/admin/operations/scoring/recent?limit=20", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	items := body["items"].([]any)
	buckets := map[string]string{}
	for _, it := range items {
		row := it.(map[string]any)
		buckets[row["job_id"].(string)] = row["decision_bucket"].(string)
		if row["job_id"] == "adm-skip-1" {
			assert.Equal(t, "claude-haiku-4-5-20251001", row["scoring_model"])
		}
	}
	assert.Equal(t, "pending_review", buckets["adm-pending-1"])
	assert.Equal(t, "top_match", buckets["adm-top-1"])
	assert.Equal(t, "skipped", buckets["adm-skip-1"])
}

func TestAdmin_LLMSummary_ZeroCallsErrorRate(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	adminToken := adminTokenFor(t, router, db, "admin-llm-zero@example.com")

	w := authGet(t, router, "/api/admin/llm-usage/summary?period=30d", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, float64(0), body["calls"])
	assert.Equal(t, float64(0), body["error_rate"])
}

func TestAdmin_LLMSummary_MixedSuccessFailure(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	adminToken := adminTokenFor(t, router, db, "admin-llm-mix@example.com")
	wMe := authGet(t, router, "/api/me", adminToken)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	userID := me["id"].(string)
	now := time.Now().UTC().Format(time.RFC3339)
	for i, ok := range []bool{true, false} {
		success := 0
		if ok {
			success = 1
		}
		code := ""
		if !ok {
			code = "rate_limit"
		}
		_, err := db.Exec(`
			INSERT INTO llm_usage_events (
				id, created_at, user_id, task, provider, requested_model, actual_model,
				input_tokens, output_tokens, raw_cost_usd_micro, loaded_cost_usd_micro, credits_burned, latency_ms,
				success, error_code, correlation_id
			) VALUES (?, ?, ?, 'form_answer', 'claude', 'm', 'm', 1, 1, 1, 1, 1, 1, ?, ?, '')`,
			uuid.NewString(), now, userID, success, code)
		require.NoError(t, err)
		_ = i
	}

	w := authGet(t, router, "/api/admin/llm-usage/summary?period=30d", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, float64(2), body["calls"])
	assert.InDelta(t, 0.5, body["error_rate"].(float64), 0.001)
}

func TestAdmin_PolicyAudit(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	adminToken := adminTokenFor(t, router, db, "admin-audit@example.com")
	w := authGet(t, router, "/api/admin/models/policy-audit", adminToken)
	require.Equal(t, http.StatusOK, w.Code)
}
