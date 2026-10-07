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

// EmailChangeHandlers serves the email-change and account-deletion endpoints.
type EmailChangeHandlers struct{ svc *Services }

func NewEmailChangeHandlers(svc *Services) *EmailChangeHandlers {
	return &EmailChangeHandlers{svc: svc}
}

// POST /api/me/email
// Body: {"new_email": "new@example.com", "current_password": "..."}
// Restricted to email/password accounts — Google-only accounts cannot change
// their Jobifai email independently.
func (h *EmailChangeHandlers) RequestEmailChange(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		NewEmail        string `json:"new_email"`
		CurrentPassword string `json:"current_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON"})
		return
	}
	req.NewEmail = strings.TrimSpace(strings.ToLower(req.NewEmail))
	if req.NewEmail == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "new_email is required"})
		return
	}

	user, err := h.svc.Users.ByID(userID)
	if errors.Is(err, auth.ErrUserNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	// Google-only accounts cannot change their Jobifai email through local flow.
	if user.PasswordHash == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"message": "Email changes for Google sign-in accounts must be managed through the Google account used to sign in.",
		})
		return
	}

	if req.CurrentPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "current_password is required"})
		return
	}
	if err := auth.CheckPassword(user.PasswordHash, req.CurrentPassword); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "current password is incorrect"})
		return
	}

	if req.NewEmail == user.Email {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "new email address must be different from the current one"})
		return
	}

	// Check whether the target address is already taken — report 409 so the UI
	// can give a helpful message. This is not a timing oracle: ErrEmailTaken is
	// only returned when the new address collides with an *existing* account.
	if _, checkErr := h.svc.Users.ByEmail(req.NewEmail); checkErr == nil {
		conflict(w, "that email address is already registered to another account")
		return
	}

	rawToken, err := auth.CreateEmailChangeToken(h.svc.DB, userID, req.NewEmail)
	if err != nil {
		log.Error().Err(err).Str("user_id", userID).Msg("email change: create token")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not initiate email change"})
		return
	}

	snd := snapshotEmailSender(h.svc)
	go func() {
		verifyURL := buildEmailChangeVerifyURL(h.svc.AppBaseURL, rawToken)
		if err := snd.SendEmailChangeVerification(context.Background(), req.NewEmail, user.DisplayName, verifyURL); err != nil {
			log.Error().Err(err).Str("user_id", userID).Msg("email change: send verification email")
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"message": "a verification link has been sent to " + req.NewEmail,
	})
}

// GET /auth/verify-email-change?token=<token>
// Public endpoint (no JWT required) — accessed by clicking the link in the
// verification email sent to the new address.
func (h *EmailChangeHandlers) VerifyEmailChange(w http.ResponseWriter, r *http.Request) {
	rawToken := r.URL.Query().Get("token")
	if rawToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "token is required"})
		return
	}

	userID, oldEmail, newEmail, err := auth.ConsumeEmailChangeToken(h.svc.DB, rawToken)
	if errors.Is(err, auth.ErrEmailChangeTokenNotFound) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"message": "invalid or already-used verification token",
			"code":    "invalid",
		})
		return
	}
	if errors.Is(err, auth.ErrEmailChangeTokenExpired) {
		writeJSON(w, http.StatusGone, map[string]string{
			"message": "this verification link has expired — please request a new email change",
			"code":    "expired",
		})
		return
	}
	if errors.Is(err, auth.ErrEmailChangeTargetTaken) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"message": "that email address has already been registered by another account",
			"code":    "taken",
		})
		return
	}
	if err != nil {
		log.Error().Err(err).Msg("verify email change: consume token")
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"message": "could not complete email change",
			"code":    "error",
		})
		return
	}

	log.Info().Str("user_id", userID).Msg("email address changed successfully")

	// Fetch the display name for notification emails.
	user, userErr := h.svc.Users.ByID(userID)

	// Identity swap is committed — send notifications out-of-band.
	// SMTP failure must never roll back the already-committed change.
	snd := snapshotEmailSender(h.svc)
	go func() {
		displayName := ""
		if userErr == nil {
			displayName = user.DisplayName
		}
		if err := snd.SendEmailChangeOldNotification(context.Background(), oldEmail, displayName, newEmail); err != nil {
			log.Error().Err(err).Str("user_id", userID).Msg("email change: send old-address notification")
		}
		if err := snd.SendEmailChangeNewConfirmation(context.Background(), newEmail, displayName); err != nil {
			log.Error().Err(err).Str("user_id", userID).Msg("email change: send new-address confirmation")
		}
	}()

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "email address updated successfully — please sign in with your new address",
		"code":    "success",
	})
}

// DELETE /api/me
// Body: {"current_password": "...", "confirm": "DELETE"}
// For email/password accounts: current_password is required.
// For Google-only accounts: confirm must equal "DELETE".
func (h *EmailChangeHandlers) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		CurrentPassword string `json:"current_password"`
		Confirm         string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON"})
		return
	}

	user, err := h.svc.Users.ByID(userID)
	if errors.Is(err, auth.ErrUserNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	if user.PasswordHash != "" {
		// Email/password account — require current password.
		if req.CurrentPassword == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "current_password is required to delete your account"})
			return
		}
		if err := auth.CheckPassword(user.PasswordHash, req.CurrentPassword); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "current password is incorrect"})
			return
		}
	} else {
		// Google-only account — require explicit confirmation string.
		if req.Confirm != "DELETE" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": `confirm must be "DELETE" to delete your account`})
			return
		}
	}

	if err := h.svc.Users.DeleteUser(userID); err != nil {
		log.Error().Err(err).Str("user_id", userID).Msg("delete account: delete user")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not delete account"})
		return
	}

	log.Info().Str("user_id", userID).Msg("account deleted")
	writeJSON(w, http.StatusOK, map[string]string{"message": "account deleted successfully"})
}

// buildEmailChangeVerifyURL constructs the email-change verification link.
func buildEmailChangeVerifyURL(baseURL, rawToken string) string {
	if baseURL == "" {
		log.Warn().Msg("APP_BASE_URL not set — email change verification link uses http://localhost:8081 fallback")
		baseURL = "http://localhost:8081"
	}
	return fmt.Sprintf("%s/auth/verify-email-change?token=%s", strings.TrimRight(baseURL, "/"), rawToken)
}
