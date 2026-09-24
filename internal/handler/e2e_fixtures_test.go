package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

func TestE2E_SeedJobs_DisabledWithoutEnv(t *testing.T) {
	t.Setenv("JOBIFAI_E2E", "")

	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "e2e-off@example.com", "password123")

	w := authPost(t, router, "/api/e2e/seed-jobs", token, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestE2E_SeedJobs_InsertsFixtureRows(t *testing.T) {
	t.Setenv("JOBIFAI_E2E", "1")

	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "e2e-on@example.com", "password123")

	w := authPost(t, router, "/api/e2e/seed-jobs", token, nil)
	require.Equal(t, http.StatusOK, w.Code)

	var seeded struct {
		Applied     int `json:"applied"`
		Skipped     int `json:"skipped"`
		Pending     int `json:"pending"`
		CannotApply int `json:"cannot_apply"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&seeded))
	assert.Equal(t, 2, seeded.Applied)
	assert.Equal(t, 1, seeded.Skipped)
	assert.Equal(t, 1, seeded.Pending)
	assert.Equal(t, 1, seeded.CannotApply)

	wApplied := authGet(t, router, "/api/jobs/applied", token)
	var applied []domain.AppliedJob
	require.NoError(t, json.NewDecoder(wApplied.Body).Decode(&applied))
	assert.Len(t, applied, 2)

	var pendingCount int
	require.NoError(t, svc.DB.QueryRow(
		`SELECT COUNT(*) FROM jobs_pending_review WHERE user_id = ? AND job_id LIKE 'e2e-pending-%'`, userIDFromToken(t, router, token),
	).Scan(&pendingCount))
	assert.Equal(t, 1, pendingCount)
}
