package handler_test

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/handler"
	"testing"
)

func TestSkippedScoreAPIKeepsEvaluatedZeroAndHidesUnscored(t *testing.T) {
	svc, db := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "zero-score@example.com", "password123")
	var me map[string]any
	require.NoError(t, json.Unmarshal(authGet(t, router, "/api/me", token).Body.Bytes(), &me))
	for _, row := range []struct {
		id, reason string
		score      any
	}{
		{"zero", "score 0 < 7", 0}, {"null", "blacklisted", nil}, {"legacy", "blacklisted", 0},
		{"failure", "seek apply: [evaluation_failed] scoring timed out", 0}, {"positive", "score 6 < 7", 6},
	} {
		_, err := db.Exec(`INSERT INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,suitability_score,viewed_at) VALUES(?,?,'seek','Company','Role','https://au.seek.com/job/1',?,?,datetime('now'))`, row.id, me["id"], row.reason, row.score)
		require.NoError(t, err)
	}
	for _, path := range []string{"/api/jobs/skipped", "/api/jobs/cannot-apply"} {
		w := authGet(t, router, path, token)
		require.Equal(t, 200, w.Code, w.Body.String())
		var rows []map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rows))
		for _, row := range rows {
			switch row["id"] {
			case "zero":
				require.Equal(t, float64(0), row["suitability_score"])
			case "positive":
				require.Equal(t, float64(6), row["suitability_score"])
			default:
				require.NotContains(t, row, "suitability_score")
			}
		}
		if path == "/api/jobs/skipped" {
			require.Len(t, rows, 4)
		} else {
			require.Len(t, rows, 1)
		}
	}
}
