package handler_test

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"testing"
)

func TestTopMatches_RequiresScoreEvenForListingReview(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "listing-review@example.com", "password123")
	var me map[string]any
	require.NoError(t, json.NewDecoder(authGet(t, router, "/api/me", token).Body).Decode(&me))
	userID := me["id"].(string)
	for _, row := range []struct {
		id, user, pack string
		score          int
	}{
		{"review", userID, `{"listing_review_required":true,"hold_reason":"Could not verify listing"}`, 0},
		{"low-score", userID, `{}`, 3},
		{"low-score-review", userID, `{"listing_review_required":true}`, 5},
		{"qualified", userID, `{}`, 8},
		{"qualified-review", userID, `{"listing_review_required":true}`, 9},
		{"foreign", "other-user", `{"listing_review_required":true}`, 0},
	} {
		_, err := db.Exec(`INSERT INTO jobs_pending_review(job_id,user_id,company,role,platform,link,suitability_score,easy_apply,document_refs_json,created_at) VALUES(?,?, 'Company','Role','linkedin','https://www.linkedin.com/jobs/view/123',?,0,?,datetime('now'))`, row.id, row.user, row.score, row.pack)
		require.NoError(t, err)
	}
	w := authGet(t, router, "/api/jobs/top-matches", token)
	require.Equal(t, 200, w.Code)
	var rows []domain.PendingReview
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rows))
	require.Len(t, rows, 2)
	require.Equal(t, "qualified-review", rows[0].JobID)
	require.Equal(t, "qualified", rows[1].JobID)
	w = authGet(t, router, "/api/jobs/stats", token)
	require.Equal(t, 200, w.Code)
	var stats domain.JobStats
	require.NoError(t, json.NewDecoder(w.Body).Decode(&stats))
	require.Equal(t, 2, stats.TopMatchesCount)
	w = authGet(t, router, "/api/jobs/top-matches?needs_verification=true", token)
	require.Equal(t, 200, w.Code)
	rows = nil
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rows))
	require.Len(t, rows, 2)
	require.Equal(t, "low-score-review", rows[0].JobID)
	require.Equal(t, "review", rows[1].JobID)
	require.Zero(t, rows[1].SuitabilityScore)
	require.Contains(t, rows[1].DocumentRefsJSON, "Could not verify listing")
}
