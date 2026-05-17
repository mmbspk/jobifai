package handler_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

func TestJobs_Applied_EmptyReturnsArray(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "jobs1@example.com", "password123")

	w := authGet(t, router, "/api/jobs/applied", token)
	assert.Equal(t, 200, w.Code)

	var out []domain.AppliedJob
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	assert.Equal(t, 0, len(out))
}

func TestJobs_Applied_ReturnsInsertedRow(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "jobs2@example.com", "password123")

	// Retrieve the user ID to scope the DB insert.
	wMe := authGet(t, router, "/api/me", token)
	require.Equal(t, 200, wMe.Code)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	userID := me["id"].(string)

	_, err := db.Exec(
		`INSERT INTO jobs_applied(id,user_id,platform,company,role,location,link,applied_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		"job-001", userID, "linkedin", "Acme Corp", "Project Manager",
		"Sydney", "https://example.com/job/1",
		time.Now().UTC().Format(time.RFC3339),
	)
	require.NoError(t, err)

	w := authGet(t, router, "/api/jobs/applied", token)
	assert.Equal(t, 200, w.Code)

	var out []domain.AppliedJob
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Len(t, out, 1)
	assert.Equal(t, "Acme Corp", out[0].Company)
	assert.Equal(t, "Project Manager", out[0].Role)
}

func TestJobs_Stats_ZeroValues(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "jobs3@example.com", "password123")

	w := authGet(t, router, "/api/jobs/stats", token)
	assert.Equal(t, 200, w.Code)

	var stats domain.JobStats
	require.NoError(t, json.NewDecoder(w.Body).Decode(&stats))
	assert.Equal(t, 0, stats.TotalApplied)
	assert.Equal(t, 0, stats.AppliedToday)
}

func TestJobs_DeleteApplied(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "jobs4@example.com", "password123")

	wMe := authGet(t, router, "/api/me", token)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	userID := me["id"].(string)

	jobID := "job-to-delete"
	_, err := db.Exec(
		`INSERT INTO jobs_applied(id,user_id,platform,company,role,location,link,applied_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		jobID, userID, "seek", "Delete Co", "Analyst", "", "https://example.com", time.Now().UTC().Format(time.RFC3339),
	)
	require.NoError(t, err)

	wDel := authDelete(t, router, fmt.Sprintf("/api/jobs/applied/%s", jobID), token)
	assert.Equal(t, 200, wDel.Code)

	w := authGet(t, router, "/api/jobs/applied", token)
	var out []domain.AppliedJob
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	assert.Empty(t, out)
}

func TestJobs_Applied_RequiresAuth(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)

	w := authGet(t, router, "/api/jobs/applied", "")
	assert.Equal(t, 401, w.Code)
}

func TestJobs_Skipped_EmptyReturnsArray(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "skipped1@example.com", "password123")

	w := authGet(t, router, "/api/jobs/skipped", token)
	assert.Equal(t, 200, w.Code)

	var out []domain.SkippedJob
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	assert.Equal(t, 0, len(out))
}

func TestJobs_CannotApply_EmptyReturnsArray(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "cannotapply1@example.com", "password123")

	w := authGet(t, router, "/api/jobs/cannot-apply", token)
	assert.Equal(t, 200, w.Code)

	var out []domain.SkippedJob
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	assert.Equal(t, 0, len(out))
}

func TestJobs_TopMatches_EmptyReturnsArray(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "topmatches1@example.com", "password123")

	w := authGet(t, router, "/api/jobs/top-matches", token)
	assert.Equal(t, 200, w.Code)

	var out []domain.PendingReview
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	assert.Equal(t, 0, len(out))
}

func TestJobs_GetJob_Applied(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "getjob1@example.com", "password123")

	wMe := authGet(t, router, "/api/me", token)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	userID := me["id"].(string)

	_, err := db.Exec(
		`INSERT INTO jobs_applied(id,user_id,platform,company,role,link,applied_at)
		 VALUES(?,?,?,?,?,?,datetime('now'))`,
		"job-applied-001", userID, "linkedin", "Acme", "Consultant", "https://example.com",
	)
	require.NoError(t, err)

	w := authGet(t, router, "/api/jobs/job-applied-001", token)
	assert.Equal(t, 200, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "applied", resp["status"])
}

func TestJobs_GetJob_Skipped(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "getjob2@example.com", "password123")

	wMe := authGet(t, router, "/api/me", token)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	userID := me["id"].(string)

	_, err := db.Exec(
		`INSERT INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,viewed_at)
		 VALUES(?,?,?,?,?,?,?,datetime('now'))`,
		"job-skipped-001", userID, "seek", "Corp", "Analyst", "https://example.com", "low score",
	)
	require.NoError(t, err)

	w := authGet(t, router, "/api/jobs/job-skipped-001", token)
	assert.Equal(t, 200, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "skipped", resp["status"])
}

func TestJobs_GetJob_NotFound(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "getjob3@example.com", "password123")

	w := authGet(t, router, "/api/jobs/nonexistent-id", token)
	assert.Equal(t, 404, w.Code)
}

func TestJobs_DeleteSkipped(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "delskip1@example.com", "password123")

	wMe := authGet(t, router, "/api/me", token)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	userID := me["id"].(string)

	_, err := db.Exec(
		`INSERT INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,viewed_at)
		 VALUES(?,?,?,?,?,?,?,datetime('now'))`,
		"skip-del-001", userID, "linkedin", "Corp", "Role", "https://example.com", "low score",
	)
	require.NoError(t, err)

	w := authDelete(t, router, "/api/jobs/skipped/skip-del-001", token)
	assert.Equal(t, 200, w.Code)

	w2 := authGet(t, router, "/api/jobs/skip-del-001", token)
	assert.Equal(t, 404, w2.Code)
}

func TestJobs_DeletePendingReview_MovesToSkipped(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "delpending1@example.com", "password123")

	wMe := authGet(t, router, "/api/me", token)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	userID := me["id"].(string)

	_, err := db.Exec(
		`INSERT INTO jobs_pending_review(job_id,user_id,company,role,platform,link,suitability_score,easy_apply,created_at)
		 VALUES(?,?,?,?,?,?,?,?,datetime('now'))`,
		"pending-001", userID, "TechCorp", "Engineer", "linkedin", "https://example.com", 8, 0,
	)
	require.NoError(t, err)

	w := authDelete(t, router, "/api/jobs/pending-review/pending-001", token)
	assert.Equal(t, 200, w.Code)

	// Should now appear in skipped.
	w2 := authGet(t, router, "/api/jobs/skipped", token)
	var skipped []domain.SkippedJob
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&skipped))
	require.Len(t, skipped, 1)
	assert.Equal(t, "TechCorp", skipped[0].Company)
}

func TestJobs_MarkApplied_MovesFromPendingToApplied(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "markapplied1@example.com", "password123")

	wMe := authGet(t, router, "/api/me", token)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	userID := me["id"].(string)

	_, err := db.Exec(
		`INSERT INTO jobs_pending_review(job_id,user_id,company,role,platform,link,suitability_score,easy_apply,created_at)
		 VALUES(?,?,?,?,?,?,?,?,datetime('now'))`,
		"pending-mark-001", userID, "Acme", "Designer", "seek", "https://example.com", 9, 0,
	)
	require.NoError(t, err)

	w := authPost(t, router, "/api/jobs/pending-review/pending-mark-001/mark-applied", token, nil)
	assert.Equal(t, 200, w.Code)

	// Should now appear in applied.
	w2 := authGet(t, router, "/api/jobs/applied", token)
	var applied []domain.AppliedJob
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&applied))
	require.Len(t, applied, 1)
	assert.Equal(t, "Acme", applied[0].Company)
}

func TestJobs_BlacklistCompany_AddsToPrefs(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "blacklist1@example.com", "password123")

	wMe := authGet(t, router, "/api/me", token)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	userID := me["id"].(string)

	_, err := db.Exec(
		`INSERT INTO jobs_pending_review(job_id,user_id,company,role,platform,link,suitability_score,easy_apply,created_at)
		 VALUES(?,?,?,?,?,?,?,?,datetime('now'))`,
		"pending-bl-001", userID, "BadCorp", "Role", "linkedin", "https://example.com", 5, 0,
	)
	require.NoError(t, err)

	w := authPost(t, router, "/api/jobs/pending-review/pending-bl-001/blacklist", token, nil)
	assert.Equal(t, 200, w.Code)

	// Verify company appears in work_preferences blacklist.
	w2 := authGet(t, router, "/api/settings/preferences", token)
	var prefs domain.WorkPreferences
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&prefs))
	assert.Contains(t, prefs.CompanyBlacklist, "BadCorp")
}

func TestJobs_RequeueCannotApply(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "requeue1@example.com", "password123")

	wMe := authGet(t, router, "/api/me", token)
	var me map[string]any
	require.NoError(t, json.NewDecoder(wMe.Body).Decode(&me))
	userID := me["id"].(string)

	// Insert a "cannot apply" job (skip_reason starts with "easy apply:")
	_, err := db.Exec(
		`INSERT INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,suitability_score,viewed_at)
		 VALUES(?,?,?,?,?,?,?,?,datetime('now'))`,
		"cannot-001", userID, "seek", "EasyApplyCo", "Manager", "https://example.com",
		"easy apply: form not filled", 7,
	)
	require.NoError(t, err)

	w := authPost(t, router, "/api/jobs/cannot-apply/cannot-001/requeue", token, nil)
	assert.Equal(t, 200, w.Code)

	// Should no longer be in cannot-apply.
	w2 := authGet(t, router, "/api/jobs/cannot-apply", token)
	var out []domain.SkippedJob
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&out))
	assert.Empty(t, out)
}
