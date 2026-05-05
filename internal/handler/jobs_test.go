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
