package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/auth"
)

// PasswordResetHandlers serves forgot-password and reset-password endpoints.
type PasswordResetHandlers struct {
	svc   *Services
	users *auth.UserStore
	db    *sql.DB
}

func NewPasswordResetHandlers(svc *Services) *PasswordResetHandlers {
	return &PasswordResetHandlers{svc: svc, users: svc.Users, db: svc.DB}
}

// POST /auth/forgot-password
// Body: {"email": "user@example.com"}
// Always returns 202 regardless of whether the account exists (anti-enumeration).
func (h *PasswordResetHandlers) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON"})
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "email is required"})
		return
	}

	// Always 202 — no enumeration.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"message":"if an account with that address exists, a password reset link has been sent"}`))

	// Snapshot sender before goroutine so no DB access happens after request ends.
	snd := snapshotEmailSender(h.svc)
	go func() {
		if err := h.sendPasswordResetEmail(req.Email, snd); err != nil {
			log.Error().Err(err).Str("email", req.Email).Msg("forgot password: failed")
		}
	}()
}

func (h *PasswordResetHandlers) sendPasswordResetEmail(emailAddr string, snd EmailSender) error {
	if h.db == nil || h.users == nil {
		return nil
	}

	user, err := h.users.ByEmail(emailAddr)
	if errors.Is(err, auth.ErrUserNotFound) {
		return nil // silently drop — no enumeration
	}
	if err != nil {
		return fmt.Errorf("forgot password: lookup user: %w", err)
	}

	// Google-only accounts have no password hash — no reset token.
	if user.PasswordHash == "" {
		return nil
	}

	// Rate-limit: one reset request per 5 minutes.
	ok, err := auth.CanRequestPasswordReset(h.db, user.ID)
	if err != nil {
		return fmt.Errorf("forgot password: rate check: %w", err)
	}
	if !ok {
		return nil // rate limited — silently drop
	}

	rawToken, err := auth.CreatePasswordResetToken(h.db, user.ID)
	if err != nil {
		return fmt.Errorf("forgot password: create token: %w", err)
	}

	resetURL := buildPasswordResetURL(h.svc.AppBaseURL, rawToken)
	if err := snd.SendPasswordReset(context.Background(), user.Email, user.DisplayName, resetURL); err != nil {
		return fmt.Errorf("forgot password: send email: %w", err)
	}
	log.Info().Str("user_id", user.ID).Msg("password reset email sent")
	return nil
}

// POST /auth/reset-password
// Body: {"token": "<raw>", "new_password": "..."}
func (h *PasswordResetHandlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON"})
		return
	}
	if req.Token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "token is required"})
		return
	}
	if len(req.NewPassword) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "password must be at least 8 characters"})
		return
	}

	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not hash password"})
		return
	}

	err = auth.ConsumePasswordResetToken(h.db, req.Token, newHash)
	if errors.Is(err, auth.ErrResetTokenNotFound) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid or expired reset token"})
		return
	}
	if errors.Is(err, auth.ErrResetTokenAlreadyUsed) {
		writeJSON(w, http.StatusGone, map[string]string{"message": "this reset link has already been used"})
		return
	}
	if err != nil {
		log.Error().Err(err).Msg("reset password: consume token")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not reset password"})
		return
	}

	log.Info().Msg("password reset successfully")
	okMsg(w, "password reset successfully — please sign in with your new password")
}

// buildPasswordResetURL constructs the password-reset link.
// Falls back to localhost when APP_BASE_URL is unset (logs a warning).
func buildPasswordResetURL(baseURL, rawToken string) string {
	if baseURL == "" {
		log.Warn().Msg("APP_BASE_URL not set — password reset link uses http://localhost:8081 fallback; set APP_BASE_URL in production")
		baseURL = "http://localhost:8081"
	}
	return fmt.Sprintf("%s/reset-password?token=%s", strings.TrimRight(baseURL, "/"), rawToken)
}
