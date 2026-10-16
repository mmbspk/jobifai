package handler_test

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/handler"
	"testing"
)

func TestCannotApplyRetry_BlocksUncertainAndActiveSubmissions(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "retry-safety@example.com", "password123")
	var me map[string]any
	require.NoError(t, json.NewDecoder(authGet(t, router, "/api/me", token).Body).Decode(&me))
	uid := me["id"].(string)
	for _, tc := range []struct{ id, outcome string }{{"unknown", "submission_unknown"}, {"active", "in_progress"}, {"safe", "failed"}} {
		_, err := db.Exec(`INSERT INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,viewed_at) VALUES(?,?,'linkedin','Company','Role',?,'easy apply: extraction failed',datetime('now'))`, tc.id, uid, "https://www.linkedin.com/jobs/view/"+tc.id)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO apply_attempts(id,user_id,platform,job_id,url,outcome) VALUES(?,?,'linkedin',?,?,?)`, tc.id, uid, tc.id, "https://www.linkedin.com/jobs/view/"+tc.id, tc.outcome)
		require.NoError(t, err)
	}
	for _, action := range []string{"retry", "requeue"} {
		require.Equal(t, 409, authPost(t, router, "/api/jobs/cannot-apply/unknown/"+action, token, nil).Code)
	}
	require.Equal(t, 409, authPost(t, router, "/api/jobs/cannot-apply/active/retry", token, nil).Code)
	response := authPost(t, router, "/api/jobs/cannot-apply/retry-all", token, nil)
	require.Equal(t, 200, response.Code)
	var summary map[string]int
	require.NoError(t, json.NewDecoder(response.Body).Decode(&summary))
	require.Equal(t, 1, summary["queued"])
	require.Equal(t, 2, summary["blocked"])
	var queued int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_approved_queue WHERE user_id=? AND needs_evaluation=1`, uid).Scan(&queued))
	require.Equal(t, 1, queued)
	var retained int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_skipped WHERE user_id=?`, uid).Scan(&retained))
	require.Equal(t, 2, retained)
}
