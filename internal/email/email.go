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

func (s *SMTPSender) send(_ context.Context, toEmail, toName, verifyURL string, isReminder bool) error {
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

	if port == 465 {
		return sendImplicitTLS(addr, s.cfg.SMTPHost, auth, extractAddress(s.cfg.EmailFrom), toEmail, msg)
	}
	return smtp.SendMail(addr, auth, extractAddress(s.cfg.EmailFrom), []string{toEmail}, msg)
}

func sendImplicitTLS(addr, host string, auth smtp.Auth, from, to string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err != nil {
		return fmt.Errorf("email: tls dial %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	_, _, err = net.SplitHostPort(addr) // validate addr format
	if err != nil {
		return err
	}

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

// ─── CaptureSender (tests: records sent emails for assertions) ─────────────

// CapturedEmail records one transactional send for test assertions.
type CapturedEmail struct {
	To        string
	VerifyURL string
	Reminder  bool
	SentAt    time.Time
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
