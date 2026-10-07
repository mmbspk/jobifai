package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/email"
)

const keyEmailSettings = "email_settings"
const secretKeySmtpPass = "smtp_pass"

// NewDynamicEmailSender returns an EmailSender that reads the current email
// configuration from the admin settings store on every send, so admin changes
// take effect immediately without restarting the server.
func NewDynamicEmailSender(cfgStore ConfigStore, secrets SecretsStore) EmailSender {
	return &dynamicEmailSender{cfgStore: cfgStore, secrets: secrets}
}

// dynamicEmailSender resolves email config from the admin settings at send
// time. When email is not configured it behaves like NoopSender (returns nil).
type dynamicEmailSender struct {
	cfgStore ConfigStore
	secrets  SecretsStore
}

func (d *dynamicEmailSender) SendVerification(ctx context.Context, toEmail, toName, verifyURL string) error {
	return d.send(ctx, toEmail, toName, verifyURL, false)
}

func (d *dynamicEmailSender) SendVerificationReminder(ctx context.Context, toEmail, toName, verifyURL string) error {
	return d.send(ctx, toEmail, toName, verifyURL, true)
}

func (d *dynamicEmailSender) SendPasswordReset(ctx context.Context, toEmail, toName, resetURL string) error {
	sender, err := buildEmailSenderFromConfig(d.cfgStore, d.secrets)
	if err != nil {
		log.Debug().Str("to", toEmail).Msg("email: not configured — skipping password reset send")
		return nil
	}
	return sender.SendPasswordReset(ctx, toEmail, toName, resetURL)
}

func (d *dynamicEmailSender) SendEmailChangeVerification(ctx context.Context, toEmail, toName, verifyURL string) error {
	sender, err := buildEmailSenderFromConfig(d.cfgStore, d.secrets)
	if err != nil {
		log.Debug().Str("to", toEmail).Msg("email: not configured — skipping email-change verification send")
		return nil
	}
	return sender.SendEmailChangeVerification(ctx, toEmail, toName, verifyURL)
}

func (d *dynamicEmailSender) SendEmailChangeOldNotification(ctx context.Context, toEmail, toName, newEmail string) error {
	sender, err := buildEmailSenderFromConfig(d.cfgStore, d.secrets)
	if err != nil {
		log.Debug().Str("to", toEmail).Msg("email: not configured — skipping email-change old-notify send")
		return nil
	}
	return sender.SendEmailChangeOldNotification(ctx, toEmail, toName, newEmail)
}

func (d *dynamicEmailSender) SendEmailChangeNewConfirmation(ctx context.Context, toEmail, toName string) error {
	sender, err := buildEmailSenderFromConfig(d.cfgStore, d.secrets)
	if err != nil {
		log.Debug().Str("to", toEmail).Msg("email: not configured — skipping email-change new-confirm send")
		return nil
	}
	return sender.SendEmailChangeNewConfirmation(ctx, toEmail, toName)
}

func (d *dynamicEmailSender) send(ctx context.Context, toEmail, toName, verifyURL string, reminder bool) error {
	sender, err := buildEmailSenderFromConfig(d.cfgStore, d.secrets)
	if err != nil {
		// Email not yet configured — silently skip, consistent with NoopSender.
		log.Debug().Str("to", toEmail).Msg("email: not configured — skipping send")
		return nil
	}
	if reminder {
		return sender.SendVerificationReminder(ctx, toEmail, toName, verifyURL)
	}
	return sender.SendVerification(ctx, toEmail, toName, verifyURL)
}

// snapshotEmailSender returns a pre-resolved EmailSender with the current
// email configuration read synchronously. Safe to pass into goroutines —
// the returned sender requires no further DB access.
// Returns email.NoopSender{} when email is not configured or the sender is nil.
func snapshotEmailSender(svc *Services) EmailSender {
	if svc.EmailSender == nil {
		return email.NoopSender{}
	}
	if ds, ok := svc.EmailSender.(*dynamicEmailSender); ok {
		s, err := buildEmailSenderFromConfig(ds.cfgStore, ds.secrets)
		if err != nil {
			return email.NoopSender{}
		}
		return s
	}
	return svc.EmailSender
}

// GET /api/admin/email/settings
func (h *AdminHandlers) EmailSettingsGet(w http.ResponseWriter, r *http.Request) {
	var cfg domain.EmailConfig
	_ = h.svc.Config.Get(domain.SystemUserID, keyEmailSettings, &cfg)
	// Never return the password — report only whether it is set.
	type response struct {
		domain.EmailConfig
		HasSMTPPass bool `json:"has_smtp_pass"`
	}
	writeJSON(w, http.StatusOK, response{
		EmailConfig: cfg,
		HasSMTPPass: h.svc.Secrets.Has(domain.SystemUserID, secretKeySmtpPass),
	})
}

// PUT /api/admin/email/settings
func (h *AdminHandlers) EmailSettingsSet(w http.ResponseWriter, r *http.Request) {
	var incoming struct {
		domain.EmailConfig
		SMTPPass string `json:"smtp_pass,omitempty"` // set only if non-empty
	}
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if err := h.svc.Config.Set(domain.SystemUserID, keyEmailSettings, incoming.EmailConfig); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	if incoming.SMTPPass != "" {
		if err := h.svc.Secrets.Set(domain.SystemUserID, secretKeySmtpPass, incoming.SMTPPass); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "saved settings but could not save SMTP password: " + err.Error()})
			return
		}
	}
	okMsg(w, "email settings saved")
}

// DELETE /api/admin/email/settings/smtp-pass
func (h *AdminHandlers) EmailDeleteSMTPPass(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Secrets.Delete(domain.SystemUserID, secretKeySmtpPass); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "SMTP password deleted")
}

// POST /api/admin/email/test
// Sends a test email to verify the current configuration.
// Body: {"to": "recipient@example.com"}
func (h *AdminHandlers) EmailTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if req.To == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "to is required"})
		return
	}

	sender, err := buildEmailSenderFromConfig(h.svc.Config, h.svc.Secrets)
	if err != nil {
		log.Warn().Err(err).Msg("email test: bad configuration")
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "email not configured: " + err.Error()})
		return
	}

	if err := sender.SendVerification(r.Context(), req.To, "", "https://example.com/auth/verify-email?token=test"); err != nil {
		log.Error().Err(err).Str("to", req.To).Msg("email test: send failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "send failed: " + err.Error()})
		return
	}
	log.Info().Str("to", req.To).Msg("email test: sent successfully")
	writeJSON(w, http.StatusOK, map[string]string{"message": "test email sent to " + req.To})
}

// buildEmailSenderFromConfig constructs an SMTPSender from the stored admin config.
// Called by the test endpoint and by main.go on startup/each request.
func buildEmailSenderFromConfig(cfgStore ConfigStore, secrets SecretsStore) (*email.SMTPSender, error) {
	var cfg domain.EmailConfig
	_ = cfgStore.Get(domain.SystemUserID, keyEmailSettings, &cfg)
	smtpPass, _ := secrets.Get(domain.SystemUserID, secretKeySmtpPass)
	return email.NewSMTPSender(email.Config{
		Provider:  cfg.Provider,
		SMTPHost:  cfg.SMTPHost,
		SMTPPort:  cfg.SMTPPort,
		SMTPUser:  cfg.SMTPUser,
		SMTPPass:  smtpPass, // never logged
		EmailFrom: cfg.EmailFrom,
	})
}
