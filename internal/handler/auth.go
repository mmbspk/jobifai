package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/browser"
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

	sess, err := h.svc.BrowserMgr.Launch(userID, req.Platform, req.ProfilePath, req.UseProfile)
	if errors.Is(err, browser.ErrAlreadyOpen) {
		conflict(w, "a browser session is already open for "+req.Platform)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"session_id": sess.ID,
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
	if errors.Is(err, browser.ErrNotFound) {
		notFound(w, "no open browser session with id "+req.SessionID)
		return
	}
	if errors.Is(err, browser.ErrSessionOwnership) {
		http.Error(w, "forbidden", http.StatusForbidden)
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

	// Invalidate the cached browser for this platform so the next SubmitNow
	// picks up the fresh cookies rather than reusing the old browser session.
	if req.Platform == "seek" {
		h.svc.Bot.InvalidateSeekBrowser(userID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"cookies_saved": len(cookies),
	})
}

// GET /api/auth/{platform}/status
func (h *AuthHandlers) PlatformStatus(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	platform := chi.URLParam(r, "platform")

	info, err := h.svc.SessionStore.Status(userID, platform)
	if errors.Is(err, browser.ErrSessionNotFound) {
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

	if err := h.svc.SessionStore.Delete(userID, platform); errors.Is(err, browser.ErrSessionNotFound) {
		notFound(w, "no session found for "+platform)
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	if platform == "seek" {
		h.svc.Bot.InvalidateSeekBrowser(userID)
	}

	okMsg(w, "session cleared for "+platform)
}
