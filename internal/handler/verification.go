package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/auth"
)

// VerificationHandlers serves the email-verification public endpoints.
type VerificationHandlers struct{ svc *Services }

func NewVerificationHandlers(svc *Services) *VerificationHandlers {
	return &VerificationHandlers{svc: svc}
}

// GET /auth/verify-email?token=<token>
func (h *VerificationHandlers) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	rawToken := r.URL.Query().Get("token")
	if rawToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "token is required"})
		return
	}

	userID, err := auth.ConsumeVerificationToken(h.svc.DB, rawToken)
	if errors.Is(err, auth.ErrTokenNotFound) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid verification token"})
		return
	}
	if errors.Is(err, auth.ErrTokenAlreadyUsed) {
		writeJSON(w, http.StatusGone, map[string]string{"message": "this verification link has already been used"})
		return
	}
	if errors.Is(err, auth.ErrTokenExpired) {
		writeJSON(w, http.StatusGone, map[string]string{"message": "this verification link has expired — please request a new one"})
		return
	}
	if err != nil {
		log.Error().Err(err).Msg("verify email: consume token")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not verify email"})
		return
	}

	log.Info().Str("user_id", userID).Msg("email verified")
	writeJSON(w, http.StatusOK, map[string]string{"message": "email verified successfully"})
}

// POST /auth/resend-verification
// Accepts: {"email": "user@example.com"}
// Always returns 202 to avoid leaking whether an address is registered.
func (h *VerificationHandlers) ResendVerification(w http.ResponseWriter, r *http.Request) {
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

	// Always respond 202 regardless of whether the account exists, to prevent
	// email-address enumeration.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"message":"if an unverified account with that address exists, a verification email has been sent"}`))

	// Snapshot sender synchronously so the goroutine only does SMTP I/O
	// and never touches the DB after the request context is done.
	snd := snapshotEmailSender(h.svc)
	baseURL := ResolvePublicAppURL(h.svc, r)
	go func() {
		if err := h.sendResendEmail(req.Email, snd, baseURL); err != nil {
			log.Error().Err(err).Str("email", req.Email).Msg("resend verification: failed")
		}
	}()
}

func (h *VerificationHandlers) sendResendEmail(emailAddr string, snd EmailSender, baseURL string) error {
	if h.svc.DB == nil {
		return nil
	}

	user, err := h.svc.Users.ByEmail(emailAddr)
	if errors.Is(err, auth.ErrUserNotFound) {
		return nil // silently drop — no enumeration
	}
	if err != nil {
		return fmt.Errorf("resend: lookup user: %w", err)
	}
	if user.EmailVerified {
		return nil // already verified — no action needed
	}

	if err := auth.CanResendVerification(h.svc.DB, user.ID); errors.Is(err, auth.ErrResendTooSoon) {
		return nil // rate limited — silently drop (caller already got 202)
	} else if err != nil {
		return fmt.Errorf("resend: rate limit check: %w", err)
	}

	rawToken, err := auth.CreateVerificationToken(h.svc.DB, user.ID)
	if err != nil {
		return fmt.Errorf("resend: create token: %w", err)
	}

	verifyURL := buildVerifyURL(baseURL, rawToken)
	if err := snd.SendVerificationReminder(context.Background(), user.Email, user.DisplayName, verifyURL); err != nil {
		return fmt.Errorf("resend: send email: %w", err)
	}
	log.Info().Str("user_id", user.ID).Msg("resend verification email sent")
	return nil
}

