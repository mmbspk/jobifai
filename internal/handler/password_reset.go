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
	baseURL := ResolvePublicAppURL(h.svc, r)
	go func() {
		if err := h.sendPasswordResetEmail(req.Email, snd, baseURL); err != nil {
			log.Error().Err(err).Str("email", req.Email).Msg("forgot password: failed")
		}
	}()
}

func (h *PasswordResetHandlers) sendPasswordResetEmail(emailAddr string, snd EmailSender, baseURL string) error {
	if h.db == nil || h.users == nil {
		return nil
	}

	user, err := h.users.ByEmail(emailAddr)
	if errors.Is(err, auth.ErrUserNotFound) {
		log.Debug().Msg("forgot password: no matching account — silently ignoring")
		return nil
	}
	if err != nil {
		return fmt.Errorf("forgot password: lookup user: %w", err)
	}

	// Google-only accounts have no password hash — no reset token.
	if user.PasswordHash == "" {
		log.Info().Str("user_id", user.ID).Msg("forgot password: account uses Google sign-in only — password reset is not available")
		return nil
	}

	rawToken, err := auth.CreatePasswordResetTokenIfAllowed(h.db, user.ID)
	if errors.Is(err, auth.ErrResetRateLimited) {
		log.Debug().Str("user_id", user.ID).Msg("forgot password: rate limited — duplicate request ignored")
		return nil
	}
	if err != nil {
		return fmt.Errorf("forgot password: create token: %w", err)
	}

	resetURL := buildPasswordResetURL(baseURL, rawToken)
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

