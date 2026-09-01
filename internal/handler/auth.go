package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/domain"
)

// AuthHandlers groups all auth-related handlers.
type AuthHandlers struct{ svc *Services }

func NewAuthHandlers(svc *Services) *AuthHandlers { return &AuthHandlers{svc: svc} }

// POST /api/auth/launch-browser
func (h *AuthHandlers) LaunchBrowser(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		Platform    string `json:"platform"`
		UseProfile  bool   `json:"use_profile"`
		ProfilePath string `json:"profile_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON"})
		return
	}
	if req.Platform == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "platform is required"})
		return
	}

	// Always use a persistent Chrome profile for Connect browser so Auth0 SPA
	// localStorage survives. Prefer General settings path; otherwise ProfileDir default.
	profilePath := req.ProfilePath
	var gs domain.GeneralSettings
	if err := h.svc.Config.Get(userID, keyGeneralSettings, &gs); err == nil && profilePath == "" {
		profilePath = gs.Browser.ChromeProfilePath
	}
	// req.UseProfile is accepted for API compatibility but session capture always
	// persists a profile — cookie-only sessions are unreliable for Seek Auth0.
	_ = req.UseProfile

	// Free the platform profile: stop automation and close any cached headless
	// browser still holding SingletonLock on this user-data-dir.
	if h.svc.Bot != nil {
		h.svc.Bot.Stop(userID)
		h.invalidatePlatformBrowser(userID, req.Platform)
	}
	profileDir := browser.ProfileDir(userID, req.Platform, profilePath)
	browser.ReleaseProfileForLaunch(profileDir)

	sess, err := h.svc.BrowserMgr.Launch(userID, req.Platform, profilePath, true)
	if errors.Is(err, domain.ErrAlreadyOpen) {
		conflict(w, "a browser session is already open for "+req.Platform)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"session_id": sess,
		"message":    "Chrome opened, log in manually then call /api/auth/save-session",
	})
}

// POST /api/auth/save-session
func (h *AuthHandlers) SaveSession(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		SessionID string `json:"session_id"`
		Platform  string `json:"platform"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON"})
		return
	}

	cookies, err := h.svc.BrowserMgr.CaptureCookies(r.Context(), userID, req.SessionID)
	if errors.Is(err, domain.ErrNotFound) {
		notFound(w, "no open browser session with id "+req.SessionID)
		return
	}
	if errors.Is(err, domain.ErrSessionOwnership) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if errors.Is(err, domain.ErrNotLoggedIn) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"message": "not logged in yet — finish signing in to " + req.Platform + " in the browser window (and complete any MFA), then click Save session again",
		})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	if err := h.svc.SessionStore.Save(userID, req.Platform, "manual", cookies); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	// Invalidate cached browsers so the next SubmitNow / bot run picks up the
	// fresh cookies rather than reusing a stale jar.
	h.invalidatePlatformBrowser(userID, req.Platform)

	cookieCount := 0
	if parsed, uerr := browser.UnmarshalCookies(cookies); uerr == nil {
		cookieCount = len(parsed)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"cookies_saved": cookieCount,
	})
}

// GET /api/auth/{platform}/status
func (h *AuthHandlers) PlatformStatus(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	platform := chi.URLParam(r, "platform")

	info, err := h.svc.SessionStore.Status(userID, platform)
	if errors.Is(err, domain.ErrSessionNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{
			"platform":    platform,
			"has_session": false,
		})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"platform":     info.Platform,
		"has_session":  true,
		"login_method": info.LoginMethod,
		"created_at":   info.CreatedAt,
	})
}

// DELETE /api/auth/{platform}/session
func (h *AuthHandlers) DeleteSession(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	platform := chi.URLParam(r, "platform")

	if err := h.svc.SessionStore.Delete(userID, platform); errors.Is(err, domain.ErrSessionNotFound) {
		notFound(w, "no session found for "+platform)
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	h.invalidatePlatformBrowser(userID, platform)

	okMsg(w, "session cleared for "+platform)
}

func (h *AuthHandlers) invalidatePlatformBrowser(userID, platform string) {
	if h.svc.Bot == nil {
		return
	}
	switch platform {
	case "seek":
		h.svc.Bot.InvalidateSeekBrowser(userID)
	case "linkedin":
		h.svc.Bot.InvalidateLinkedInBrowser(userID)
	}
}
