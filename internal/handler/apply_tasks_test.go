package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/handler"
)

type applyTaskBot struct {
	stubBotCtrl
	started chan context.Context
	release chan struct{}
	calls   atomic.Int32
	fail    bool
}

func (b *applyTaskBot) ApplyFromURL(ctx context.Context, _, _, _ string, _ bool) (bot.ApplyFromURLResult, error) {
	b.calls.Add(1)
	b.started <- ctx
	select {
	case <-ctx.Done():
		return bot.ApplyFromURLResult{}, ctx.Err()
	case <-b.release:
		if b.fail {
			return bot.ApplyFromURLResult{}, errors.New("resume library needs review")
		}
		return bot.ApplyFromURLResult{JobID: "new-job", Company: "Company", Role: "Engineer", Score: 5, ScoreWarning: true}, nil
	}
}

func TestApplyTask_SurvivesRequestCancellationAndDeduplicates(t *testing.T) {
	svc, db := newTestServices(t)
	b := &applyTaskBot{started: make(chan context.Context, 1), release: make(chan struct{})}
	svc.Bot = b
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "task@example.com", "password123")
	other := registerAndLogin(t, router, "other-task@example.com", "password123")
	var me map[string]any
	require.NoError(t, json.NewDecoder(authGet(t, router, "/api/me", token).Body).Decode(&me))
	uid := me["id"].(string)
	_, err := db.Exec("INSERT INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,viewed_at) VALUES('old-job',?,'seek','Company','Engineer','https://au.seek.com/job/123','seek apply: old failure',datetime('now'))", uid)
	require.NoError(t, err)
	id := uuid.NewString()
	body := map[string]any{"id": id, "url": "https://au.seek.com/job/123", "market": "Australia"}
	encoded, _ := json.Marshal(body)
	clientCtx, cancelClient := context.WithCancel(context.Background())
	req := httptest.NewRequest("POST", "/api/bot/apply-tasks", strings.NewReader(string(encoded))).WithContext(clientCtx)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 202, w.Code, w.Body.String())
	serverCtx := <-b.started
	cancelClient()
	require.NoError(t, serverCtx.Err(), "leaving the page must not cancel the operation")
	require.Equal(t, 200, authPost(t, router, "/api/bot/apply-tasks", token, body).Code)
	require.Equal(t, int32(1), b.calls.Load())
	require.Equal(t, 409, authPost(t, router, "/api/bot/apply-tasks", token, map[string]any{"id": uuid.NewString(), "url": "https://au.seek.com/job/456"}).Code)
	require.Equal(t, 404, authGet(t, router, "/api/bot/apply-tasks/"+id, other).Code)
	require.Equal(t, 404, authPost(t, router, "/api/bot/apply-tasks/"+id+"/cancel", other, nil).Code)
	require.Equal(t, 409, authPost(t, router, "/api/auth/launch-browser", token, map[string]any{"platform": "seek"}).Code)
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM jobs_skipped WHERE id='old-job'").Scan(&count))
	require.Equal(t, 1, count, "running work keeps the Cannot Apply listing")
	close(b.release)
	require.Eventually(t, func() bool {
		var state string
		return db.QueryRow("SELECT state FROM apply_url_tasks WHERE id=?", id).Scan(&state) == nil && state == "succeeded"
	}, 3*time.Second, 10*time.Millisecond)
	var task handler.ApplyTask
	require.NoError(t, json.NewDecoder(authGet(t, router, "/api/bot/apply-tasks/"+id, token).Body).Decode(&task))
	require.Contains(t, string(task.Response), "score_warning")
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM jobs_skipped WHERE id='old-job'").Scan(&count))
	require.Zero(t, count, "completed classification replaces the stale failure")
}

func TestApplyTask_CancelAndFailureKeepListing(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "cancel"}[cancelled], func(t *testing.T) {
			svc, db := newTestServices(t)
			b := &applyTaskBot{started: make(chan context.Context, 1), release: make(chan struct{}), fail: true}
			svc.Bot = b
			router := handler.NewRouter(svc)
			token := registerAndLogin(t, router, t.Name()+"@example.com", "password123")
			var me map[string]any
			require.NoError(t, json.NewDecoder(authGet(t, router, "/api/me", token).Body).Decode(&me))
			_, err := db.Exec("INSERT INTO jobs_skipped(id,user_id,platform,company,role,link,skip_reason,viewed_at) VALUES('old-job',?,'seek','Company','Engineer','https://au.seek.com/job/123','seek apply: old failure',datetime('now'))", me["id"])
			require.NoError(t, err)
			id := uuid.NewString()
			require.Equal(t, 202, authPost(t, router, "/api/bot/apply-tasks", token, map[string]any{"id": id, "url": "https://au.seek.com/job/123"}).Code)
			<-b.started
			if cancelled {
				require.Equal(t, 202, authPost(t, router, "/api/bot/apply-tasks/"+id+"/cancel", token, nil).Code)
			} else {
				close(b.release)
			}
			want := "failed"
			if cancelled {
				want = "cancelled"
			}
			require.Eventually(t, func() bool {
				var state string
				return db.QueryRow("SELECT state FROM apply_url_tasks WHERE id=?", id).Scan(&state) == nil && state == want
			}, 3*time.Second, 10*time.Millisecond)
			var count int
			require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM jobs_skipped WHERE id='old-job'").Scan(&count))
			require.Equal(t, 1, count)
		})
	}
}

func TestApplyTask_RestartPreservesInterruptedResultWithoutReplay(t *testing.T) {
	svc, db := newTestServices(t)
	id := uuid.NewString()
	_, err := db.Exec("INSERT INTO apply_url_tasks(id,user_id,url,state,started_at) VALUES(?,'user','https://au.seek.com/job/123','running',datetime('now'))", id)
	require.NoError(t, err)
	handler.NewBotHandlers(svc)
	var state, message string
	require.NoError(t, db.QueryRow("SELECT state,error FROM apply_url_tasks WHERE id=?", id).Scan(&state, &message))
	require.Equal(t, "interrupted", state)
	require.Contains(t, message, "Check the job site's application status")
}
