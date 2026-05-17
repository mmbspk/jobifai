package handler_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

// ── stubs ────────────────────────────────────────────────────────────────────

type stubBrowserMgr struct {
	launchID   string
	launchErr  error
	cookies    []byte
	captureErr error
}

func (s *stubBrowserMgr) Launch(_, _, _ string, _ bool) (string, error) {
	return s.launchID, s.launchErr
}

func (s *stubBrowserMgr) CaptureCookies(_ context.Context, _, _ string) ([]byte, error) {
	return s.cookies, s.captureErr
}

type stubSessionStore struct {
	sessions  map[string]*domain.PlatformSession
	saveErr   error
	deleteErr error
}

func newStubSessionStore() *stubSessionStore {
	return &stubSessionStore{sessions: make(map[string]*domain.PlatformSession)}
}

func (s *stubSessionStore) Save(_, platform, loginMethod string, _ []byte) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.sessions[platform] = &domain.PlatformSession{
		Platform:    domain.Platform(platform),
		HasSession:  true,
		LoginMethod: domain.LoginMethod(loginMethod),
	}
	return nil
}

func (s *stubSessionStore) Status(_, platform string) (*domain.PlatformSession, error) {
	if p, ok := s.sessions[platform]; ok {
		return p, nil
	}
	return nil, domain.ErrSessionNotFound
}

func (s *stubSessionStore) Delete(_, platform string) error {
	if _, ok := s.sessions[platform]; !ok {
		return domain.ErrSessionNotFound
	}
	delete(s.sessions, platform)
	return s.deleteErr
}

type stubBotCtrl struct{}

func (stubBotCtrl) Start(_ context.Context, _ string, _ domain.Platform) error { return nil }
func (stubBotCtrl) Stop(_ string)                                               {}
func (stubBotCtrl) Pause(_ string)                                              {}
func (stubBotCtrl) Resume(_ string)                                             {}
func (stubBotCtrl) Status(_ string) domain.BotStatus {
	return domain.BotStatus{State: domain.BotStateIdle}
}
func (stubBotCtrl) SubmitNow(_ string, _ bot.SubmitRequest)                              {}
func (stubBotCtrl) SubmitSync(_ context.Context, _ string, _ bot.SubmitRequest) error    { return nil }
func (stubBotCtrl) ApplyFromURL(_ context.Context, _, _, _ string, _ bool) (bot.ApplyFromURLResult, error) {
	return bot.ApplyFromURLResult{}, nil
}
func (stubBotCtrl) InvalidateSeekBrowser(_ string) {}

func newAuthTestServices(t *testing.T, bm *stubBrowserMgr, ss *stubSessionStore) (*handler.Services, string) {
	t.Helper()
	svc, _ := newTestServices(t)
	svc.BrowserMgr = bm
	svc.SessionStore = ss
	svc.Bot = stubBotCtrl{}
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, t.Name()+"@example.com", "password123")
	return svc, token
}

// ── LaunchBrowser ────────────────────────────────────────────────────────────

func TestAuth_LaunchBrowser_Success(t *testing.T) {
	bm := &stubBrowserMgr{launchID: "sess-001"}
	svc, token := newAuthTestServices(t, bm, newStubSessionStore())
	router := handler.NewRouter(svc)

	w := authPost(t, router, "/api/auth/launch-browser", token, map[string]any{
		"platform": "linkedin",
	})
	assert.Equal(t, 200, w.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "sess-001", resp["session_id"])
}

func TestAuth_LaunchBrowser_MissingPlatform(t *testing.T) {
	bm := &stubBrowserMgr{launchID: "sess-001"}
	svc, token := newAuthTestServices(t, bm, newStubSessionStore())
	router := handler.NewRouter(svc)

	w := authPost(t, router, "/api/auth/launch-browser", token, map[string]any{})
	assert.Equal(t, 400, w.Code)
}

func TestAuth_LaunchBrowser_AlreadyOpen(t *testing.T) {
	bm := &stubBrowserMgr{launchErr: domain.ErrAlreadyOpen}
	svc, token := newAuthTestServices(t, bm, newStubSessionStore())
	router := handler.NewRouter(svc)

	w := authPost(t, router, "/api/auth/launch-browser", token, map[string]any{
		"platform": "linkedin",
	})
	assert.Equal(t, 409, w.Code)
}

// ── SaveSession ───────────────────────────────────────────────────────────────

func TestAuth_SaveSession_Success(t *testing.T) {
	cookies := []byte(`[{"name":"auth","value":"tok","domain":".linkedin.com","path":"/"}]`)
	bm := &stubBrowserMgr{cookies: cookies}
	ss := newStubSessionStore()
	svc, token := newAuthTestServices(t, bm, ss)
	router := handler.NewRouter(svc)

	w := authPost(t, router, "/api/auth/save-session", token, map[string]any{
		"session_id": "sess-001",
		"platform":   "linkedin",
	})
	assert.Equal(t, 200, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["success"])
}

func TestAuth_SaveSession_NotFound(t *testing.T) {
	bm := &stubBrowserMgr{captureErr: domain.ErrNotFound}
	svc, token := newAuthTestServices(t, bm, newStubSessionStore())
	router := handler.NewRouter(svc)

	w := authPost(t, router, "/api/auth/save-session", token, map[string]any{
		"session_id": "missing",
		"platform":   "linkedin",
	})
	assert.Equal(t, 404, w.Code)
}

// ── PlatformStatus ────────────────────────────────────────────────────────────

func TestAuth_PlatformStatus_NoSession(t *testing.T) {
	svc, token := newAuthTestServices(t, &stubBrowserMgr{}, newStubSessionStore())
	router := handler.NewRouter(svc)

	w := authGet(t, router, "/api/auth/linkedin/status", token)
	assert.Equal(t, 200, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, false, resp["has_session"])
}

func TestAuth_PlatformStatus_WithSession(t *testing.T) {
	ss := newStubSessionStore()
	ss.sessions["seek"] = &domain.PlatformSession{
		Platform:    "seek",
		HasSession:  true,
		LoginMethod: "manual",
	}
	svc, token := newAuthTestServices(t, &stubBrowserMgr{}, ss)
	router := handler.NewRouter(svc)

	w := authGet(t, router, "/api/auth/seek/status", token)
	assert.Equal(t, 200, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["has_session"])
	assert.Equal(t, "manual", resp["login_method"])
}

// ── DeleteSession ─────────────────────────────────────────────────────────────

func TestAuth_DeleteSession_Success(t *testing.T) {
	ss := newStubSessionStore()
	ss.sessions["linkedin"] = &domain.PlatformSession{Platform: domain.Platform("linkedin")}
	svc, token := newAuthTestServices(t, &stubBrowserMgr{}, ss)
	router := handler.NewRouter(svc)

	w := authDelete(t, router, "/api/auth/linkedin/session", token)
	assert.Equal(t, 200, w.Code)
	assert.Empty(t, ss.sessions["linkedin"])
}

func TestAuth_DeleteSession_NotFound(t *testing.T) {
	svc, token := newAuthTestServices(t, &stubBrowserMgr{}, newStubSessionStore())
	router := handler.NewRouter(svc)

	w := authDelete(t, router, "/api/auth/linkedin/session", token)
	assert.Equal(t, 404, w.Code)
}

// ── RequiresAuth ──────────────────────────────────────────────────────────────

func TestAuth_RequiresAuth(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.BrowserMgr = &stubBrowserMgr{}
	svc.SessionStore = newStubSessionStore()
	svc.Bot = stubBotCtrl{}
	router := handler.NewRouter(svc)

	paths := []struct {
		method string
		path   string
	}{
		{"POST", "/api/auth/launch-browser"},
		{"POST", "/api/auth/save-session"},
		{"GET", "/api/auth/linkedin/status"},
		{"DELETE", "/api/auth/linkedin/session"},
	}
	for _, tc := range paths {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := authPost(t, router, tc.path, "", nil)
			if tc.method == "GET" {
				w = authGet(t, router, tc.path, "")
			} else if tc.method == "DELETE" {
				w = authDelete(t, router, tc.path, "")
			}
			assert.Equal(t, 401, w.Code)
		})
	}
}
