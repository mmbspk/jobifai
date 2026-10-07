// Package email provides a provider-agnostic interface for sending
// transactional emails. Implementations: SMTP (default), Noop (tests).
package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// smtpSendTimeout is the maximum time allowed for a complete SMTP send,
// including dial, TLS handshake, authentication, and message transfer.
const smtpSendTimeout = 30 * time.Second

// Config holds the runtime email configuration loaded from the admin settings.
type Config struct {
	Provider  string // "smtp" (currently the only production provider)
	SMTPHost  string
	SMTPPort  int    // e.g. 587 (STARTTLS), 465 (implicit TLS), 25 (plain)
	SMTPUser  string
	SMTPPass  string // decrypted at send time; never logged
	EmailFrom string // e.g. "Jobifai <noreply@jobifai.com.au>"
}

// Sender is the interface for sending transactional emails.
// Implementations must not log secret values.
type Sender interface {
	// SendVerification sends an initial email-verification message.
	SendVerification(ctx context.Context, toEmail, toName, verifyURL string) error
	// SendVerificationReminder is used when the user explicitly requests a resend.
	SendVerificationReminder(ctx context.Context, toEmail, toName, verifyURL string) error
	// SendPasswordReset sends a password-reset link to the user.
	SendPasswordReset(ctx context.Context, toEmail, toName, resetURL string) error
	// SendEmailChangeVerification sends a confirmation link to the new address.
	SendEmailChangeVerification(ctx context.Context, toEmail, toName, verifyURL string) error
	// SendEmailChangeOldNotification notifies the old address that the email was changed.
	SendEmailChangeOldNotification(ctx context.Context, toEmail, toName, newEmail string) error
	// SendEmailChangeNewConfirmation confirms to the new address that it is now active.
	SendEmailChangeNewConfirmation(ctx context.Context, toEmail, toName string) error
}

// ─── SMTP implementation ───────────────────────────────────────────────────

// SMTPSender sends emails via SMTP (STARTTLS on 587, implicit TLS on 465).
type SMTPSender struct {
	cfg Config
}

// NewSMTPSender creates an SMTPSender. Returns an error if the configuration
// is clearly incomplete.
func NewSMTPSender(cfg Config) (*SMTPSender, error) {
	if cfg.SMTPHost == "" {
		return nil, fmt.Errorf("email: smtp_host is required")
	}
	if cfg.EmailFrom == "" {
		return nil, fmt.Errorf("email: email_from is required")
	}
	return &SMTPSender{cfg: cfg}, nil
}

func (s *SMTPSender) SendVerification(ctx context.Context, toEmail, toName, verifyURL string) error {
	return s.send(ctx, toEmail, toName, verifyURL, false)
}

func (s *SMTPSender) SendVerificationReminder(ctx context.Context, toEmail, toName, verifyURL string) error {
	return s.send(ctx, toEmail, toName, verifyURL, true)
}

func (s *SMTPSender) SendPasswordReset(ctx context.Context, toEmail, toName, resetURL string) error {
	subj := "Reset your Jobifai password"

	plain, htmlBody, err := RenderPasswordReset(toName, resetURL)
	if err != nil {
		return fmt.Errorf("email: render reset template: %w", err)
	}

	msg := buildMIMEMessage(s.cfg.EmailFrom, toEmail, subj, plain, htmlBody)

	port := s.cfg.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.SMTPHost, port)

	var auth smtp.Auth
	if s.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPass, s.cfg.SMTPHost)
	}

	sendCtx, cancel := context.WithTimeout(ctx, smtpSendTimeout)
	defer cancel()

	from := extractAddress(s.cfg.EmailFrom)
	if port == 465 {
		return sendImplicitTLS(sendCtx, addr, s.cfg.SMTPHost, auth, from, toEmail, msg)
	}
	return sendSTARTTLS(sendCtx, addr, s.cfg.SMTPHost, auth, from, toEmail, msg)
}

func (s *SMTPSender) SendEmailChangeVerification(ctx context.Context, toEmail, toName, verifyURL string) error {
	plain, htmlBody, err := RenderEmailChangeVerification(toName, verifyURL)
	if err != nil {
		return fmt.Errorf("email: render email-change verification template: %w", err)
	}
	return s.sendMsg(ctx, toEmail, "Confirm your new email address — Jobifai", plain, htmlBody)
}

func (s *SMTPSender) SendEmailChangeOldNotification(ctx context.Context, toEmail, toName, newEmail string) error {
	plain, htmlBody, err := RenderEmailChangeOldNotification(toName, newEmail)
	if err != nil {
		return fmt.Errorf("email: render email-change old-notification template: %w", err)
	}
	return s.sendMsg(ctx, toEmail, "Your Jobifai email address was changed", plain, htmlBody)
}

func (s *SMTPSender) SendEmailChangeNewConfirmation(ctx context.Context, toEmail, toName string) error {
	plain, htmlBody, err := RenderEmailChangeNewConfirmation(toName)
	if err != nil {
		return fmt.Errorf("email: render email-change new-confirmation template: %w", err)
	}
	return s.sendMsg(ctx, toEmail, "Your Jobifai email address is confirmed", plain, htmlBody)
}

func (s *SMTPSender) sendMsg(ctx context.Context, toEmail, subject, plain, htmlBody string) error {
	msg := buildMIMEMessage(s.cfg.EmailFrom, toEmail, subject, plain, htmlBody)
	port := s.cfg.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.SMTPHost, port)
	var auth smtp.Auth
	if s.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPass, s.cfg.SMTPHost)
	}
	sendCtx, cancel := context.WithTimeout(ctx, smtpSendTimeout)
	defer cancel()
	from := extractAddress(s.cfg.EmailFrom)
	if port == 465 {
		return sendImplicitTLS(sendCtx, addr, s.cfg.SMTPHost, auth, from, toEmail, msg)
	}
	return sendSTARTTLS(sendCtx, addr, s.cfg.SMTPHost, auth, from, toEmail, msg)
}

func (s *SMTPSender) send(ctx context.Context, toEmail, toName, verifyURL string, isReminder bool) error {
	subj := "Confirm your email address — Jobifai"
	if isReminder {
		subj = "Reminder: confirm your email address — Jobifai"
	}

	plain, htmlBody, err := RenderVerification(toName, verifyURL, isReminder)
	if err != nil {
		return fmt.Errorf("email: render template: %w", err)
	}

	msg := buildMIMEMessage(s.cfg.EmailFrom, toEmail, subj, plain, htmlBody)

	port := s.cfg.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.SMTPHost, port)

	var auth smtp.Auth
	if s.cfg.SMTPUser != "" {
		// SMTPPass is intentionally not logged anywhere.
		auth = smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPass, s.cfg.SMTPHost)
	}

	// Wrap with a send timeout so SMTP cannot hang indefinitely.
	sendCtx, cancel := context.WithTimeout(ctx, smtpSendTimeout)
	defer cancel()

	from := extractAddress(s.cfg.EmailFrom)
	if port == 465 {
		return sendImplicitTLS(sendCtx, addr, s.cfg.SMTPHost, auth, from, toEmail, msg)
	}
	return sendSTARTTLS(sendCtx, addr, s.cfg.SMTPHost, auth, from, toEmail, msg)
}

func sendImplicitTLS(ctx context.Context, addr, host string, auth smtp.Auth, from, to string, msg []byte) error {
	var d net.Dialer
	rawConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("email: dial %s: %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = rawConn.SetDeadline(deadline)
	}

	conn := tls.Client(rawConn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = rawConn.Close()
		return fmt.Errorf("email: tls handshake %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("email: smtp client: %w", err)
	}
	defer func() { _ = c.Quit() }()

	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("email: smtp auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("email: smtp MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("email: smtp RCPT TO: %w", err)
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("email: smtp DATA: %w", err)
	}
	if _, err := wc.Write(msg); err != nil {
		return fmt.Errorf("email: smtp write body: %w", err)
	}
	return wc.Close()
}

func sendSTARTTLS(ctx context.Context, addr, host string, auth smtp.Auth, from, to string, msg []byte) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("email: dial %s: %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	defer func() { _ = conn.Close() }()

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("email: smtp client: %w", err)
	}
	defer func() { _ = c.Quit() }()

	if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
		return fmt.Errorf("email: starttls: %w", err)
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("email: smtp auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("email: smtp MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("email: smtp RCPT TO: %w", err)
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("email: smtp DATA: %w", err)
	}
	if _, err := wc.Write(msg); err != nil {
		return fmt.Errorf("email: smtp write body: %w", err)
	}
	return wc.Close()
}

// ─── Noop sender (tests / unconfigured) ────────────────────────────────────

// NoopSender discards all emails and logs at debug level.
type NoopSender struct{}

func (NoopSender) SendVerification(_ context.Context, toEmail, _, _ string) error {
	log.Debug().Str("to", toEmail).Msg("email: noop — email not configured")
	return nil
}
func (NoopSender) SendVerificationReminder(_ context.Context, toEmail, _, _ string) error {
	log.Debug().Str("to", toEmail).Msg("email: noop reminder — email not configured")
	return nil
}
func (NoopSender) SendPasswordReset(_ context.Context, toEmail, _, _ string) error {
	log.Debug().Str("to", toEmail).Msg("email: noop password reset — email not configured")
	return nil
}
func (NoopSender) SendEmailChangeVerification(_ context.Context, toEmail, _, _ string) error {
	log.Debug().Str("to", toEmail).Msg("email: noop email-change verification — email not configured")
	return nil
}
func (NoopSender) SendEmailChangeOldNotification(_ context.Context, toEmail, _, _ string) error {
	log.Debug().Str("to", toEmail).Msg("email: noop email-change old notification — email not configured")
	return nil
}
func (NoopSender) SendEmailChangeNewConfirmation(_ context.Context, toEmail, _ string) error {
	log.Debug().Str("to", toEmail).Msg("email: noop email-change new confirmation — email not configured")
	return nil
}

// ─── CaptureSender (tests: records sent emails for assertions) ─────────────

// CapturedEmail records one transactional send for test assertions.
type CapturedEmail struct {
	To                      string
	VerifyURL               string
	ResetURL                string
	Reminder                bool
	PasswordReset           bool
	EmailChangeVerify       bool   // verification link to new address
	EmailChangeOldNotify    bool   // security notice to old address
	EmailChangeNewConfirm   bool   // confirmation to new address
	NewEmail                string // populated for EmailChangeOldNotify
	SentAt                  time.Time
}

// CaptureSender records emails without delivering them. Use in tests.
type CaptureSender struct {
	mu     sync.Mutex
	emails []CapturedEmail
}

func (c *CaptureSender) SendVerification(_ context.Context, to, _, verifyURL string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emails = append(c.emails, CapturedEmail{To: to, VerifyURL: verifyURL, SentAt: time.Now()})
	return nil
}

func (c *CaptureSender) SendVerificationReminder(_ context.Context, to, _, verifyURL string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emails = append(c.emails, CapturedEmail{To: to, VerifyURL: verifyURL, Reminder: true, SentAt: time.Now()})
	return nil
}

func (c *CaptureSender) SendPasswordReset(_ context.Context, to, _, resetURL string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emails = append(c.emails, CapturedEmail{To: to, ResetURL: resetURL, PasswordReset: true, SentAt: time.Now()})
	return nil
}

func (c *CaptureSender) SendEmailChangeVerification(_ context.Context, to, _, verifyURL string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emails = append(c.emails, CapturedEmail{To: to, VerifyURL: verifyURL, EmailChangeVerify: true, SentAt: time.Now()})
	return nil
}

func (c *CaptureSender) SendEmailChangeOldNotification(_ context.Context, to, _, newEmail string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emails = append(c.emails, CapturedEmail{To: to, NewEmail: newEmail, EmailChangeOldNotify: true, SentAt: time.Now()})
	return nil
}

func (c *CaptureSender) SendEmailChangeNewConfirmation(_ context.Context, to, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emails = append(c.emails, CapturedEmail{To: to, EmailChangeNewConfirm: true, SentAt: time.Now()})
	return nil
}

// Last returns a copy of the most recently captured email, or nil if none were sent.
func (c *CaptureSender) Last() *CapturedEmail {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.emails) == 0 {
		return nil
	}
	cp := c.emails[len(c.emails)-1]
	return &cp
}

// All returns a copy of all captured emails.
func (c *CaptureSender) All() []CapturedEmail {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]CapturedEmail, len(c.emails))
	copy(out, c.emails)
	return out
}

// Count returns the number of emails captured so far.
func (c *CaptureSender) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.emails)
}

// ─── Helpers ───────────────────────────────────────────────────────────────

// extractAddress strips a display name from an RFC 5321 address string.
// "Jobifai <noreply@example.com>" → "noreply@example.com"
func extractAddress(from string) string {
	if i := strings.Index(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j >= 0 {
			return strings.TrimSpace(from[i+1 : i+j])
		}
	}
	return strings.TrimSpace(from)
}

func buildMIMEMessage(from, to, subject, plainText, htmlText string) []byte {
	const boundary = "==jobifai_mime_boundary=="
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary)
	fmt.Fprintf(&b, "\r\n")

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(plainText)
	fmt.Fprintf(&b, "\r\n")

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/html; charset=UTF-8\r\n\r\n")
	b.WriteString(htmlText)
	fmt.Fprintf(&b, "\r\n")

	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes()
}
