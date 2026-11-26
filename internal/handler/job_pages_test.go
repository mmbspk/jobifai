package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/jobpage"
)

func TestAdminJobPagesAuthorizationRevisionAndCanary(t *testing.T) {
	svc, db := newTestServices(t)
	svc.JobPages = jobpage.NewSQLStore(db)
	router := handler.NewRouter(svc)
	candidate := registerAndLogin(t, router, "candidate-pages@example.com", "password123")
	admin := registerAndLogin(t, router, "admin-pages@example.com", "password123")
	setUserAdmin(t, db, "admin-pages@example.com")
	var adminID string
	require.NoError(t, db.QueryRow("SELECT id FROM users WHERE email=?", "admin-pages@example.com").Scan(&adminID))
	for _, path := range []string{"/api/admin/job-page-reading", "/api/admin/job-page-reading/recipes", "/api/admin/job-page-reading/events", "/api/admin/job-page-reading/audit"} {
		require.Equal(t, http.StatusForbidden, authGet(t, router, path, candidate).Code)
		require.Equal(t, http.StatusOK, authGet(t, router, path, admin).Code)
	}
	var initial jobpage.Config
	require.NoError(t, json.Unmarshal(authGet(t, router, "/api/admin/job-page-reading", admin).Body.Bytes(), &initial))
	require.Equal(t, jobpage.ModeOff, initial.Recovery)
	require.Equal(t, jobpage.ModeOff, initial.Reuse)
	updated := initial
	updated.Recovery = jobpage.ModePilot
	updated.PilotUsers = []string{adminID}
	require.Equal(t, 200, authPut(t, router, "/api/admin/job-page-reading", admin, updated).Code)
	require.Equal(t, 409, authPut(t, router, "/api/admin/job-page-reading", admin, initial).Code, "stale changes cannot overwrite current rollout")
	var calls int
	svc.ReadJobPage = func(ctx context.Context, user, url string) (jobpage.Result, error) {
		calls++
		require.Equal(t, adminID, user)
		require.Equal(t, adminID, auth.UserIDFromCtx(ctx))
		require.Equal(t, "https://www.linkedin.com/jobs/view/123", url)
		return jobpage.Result{Description: "verified fixture", Source: jobpage.SourceRecipe}, nil
	}
	require.Equal(t, 403, authPost(t, router, "/api/admin/job-page-reading/canary", candidate, map[string]string{"url": "https://www.linkedin.com/jobs/view/123"}).Code)
	require.Equal(t, 400, authPost(t, router, "/api/admin/job-page-reading/canary", admin, map[string]string{"url": "https://attacker.test/jobs/view/123"}).Code)
	require.Zero(t, calls)
	response := authPost(t, router, "/api/admin/job-page-reading/canary", admin, map[string]string{"url": "https://www.linkedin.com/jobs/view/123"})
	require.Equal(t, 200, response.Code)
	require.Equal(t, 1, calls)
	require.Contains(t, response.Body.String(), "verified_recipe")
}

func TestGenerateJobPageUsesOwnedWorkerReader(t *testing.T) {
	router, svc, token := newResumeRouter(t)
	saveResumeProfile(t, router, token, sampleResumeProfile())
	workerCalls := 0
	svc.FetchVerifiedJobPage = func(ctx context.Context, user, url string) (string, error) {
		workerCalls++
		require.NotEmpty(t, user)
		require.Equal(t, auth.UserIDFromCtx(ctx), user)
		return "Current verified description", nil
	}
	// Fetch endpoint must select the authenticated worker hook rather than
	// the former public HTTP text reader, even without a generation request.
	svc.FetchJobPage = func(context.Context, string) (string, error) {
		t.Fatal("public HTTP fallback selected for supported worker read")
		return "", nil
	}
	response := authPostMultipart(t, router, "/api/resume/evaluate", token, map[string]string{"job_url": "https://www.linkedin.com/jobs/view/123"})

	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, 1, workerCalls)
}

func TestGeneratePreservesJobPageFailureWithoutScoring(t *testing.T) {
	router, svc, token := newResumeRouter(t)
	saveResumeProfile(t, router, token, sampleResumeProfile())
	svc.FetchVerifiedJobPage = func(context.Context, string, string) (string, error) {
		return "", &jobpage.ReadError{Code: jobpage.FailureAuth}
	}
	response := authPostMultipart(t, router, "/api/resume/evaluate", token, map[string]string{"job_url": "https://www.linkedin.com/jobs/view/123"})
	require.Equal(t, http.StatusUnprocessableEntity, response.Code)
	require.Contains(t, response.Body.String(), "authentication_required")
	require.NotContains(t, response.Body.String(), "url_unreachable")
}
