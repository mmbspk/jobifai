package email

import (
	"bytes"
	"fmt"
	htmltmpl "html/template"
	texttmpl "text/template"
)

// ─── Plain-text template ───────────────────────────────────────────────────

var plainVerificationTmpl = texttmpl.Must(texttmpl.New("verification_plain").Parse(`{{if .Reminder}}This is a reminder that your Jobifai{{else}}Welcome to Jobifai{{end}} — please verify your email address.
{{if .Name}}
Hi {{.Name}},
{{end}}
{{if .Reminder}}We noticed your email address hasn't been verified yet. Please click the link below to confirm your address:{{else}}Thanks for registering. To complete your account setup, please click the link below to verify your email address:{{end}}

  {{.VerifyURL}}

This link is valid for 24 hours. If you didn't create an account with Jobifai, you can safely ignore this message.

— The Jobifai Team
`))

// ─── HTML template ─────────────────────────────────────────────────────────

var htmlVerificationTmpl = htmltmpl.Must(htmltmpl.New("verification_html").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{if .Reminder}}Reminder: Verify{{else}}Verify{{end}} your email address</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; background: #f4f4f5; margin: 0; padding: 0; }
    .container { max-width: 560px; margin: 40px auto; background: #ffffff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 4px rgba(0,0,0,0.08); }
    .header { background: #18181b; padding: 24px 32px; }
    .header h1 { color: #ffffff; font-size: 20px; margin: 0; font-weight: 600; }
    .body { padding: 32px; color: #3f3f46; font-size: 15px; line-height: 1.6; }
    .body p { margin: 0 0 16px; }
    .btn { display: inline-block; background: #6366f1; color: #ffffff; text-decoration: none; padding: 12px 24px; border-radius: 6px; font-size: 15px; font-weight: 600; margin: 8px 0 20px; }
    .url-fallback { font-size: 12px; color: #71717a; word-break: break-all; }
    .footer { padding: 20px 32px; border-top: 1px solid #e4e4e7; font-size: 12px; color: #a1a1aa; }
  </style>
</head>
<body>
<div class="container">
  <div class="header">
    <h1>Jobifai</h1>
  </div>
  <div class="body">
    {{if .Name}}<p>Hi {{.Name}},</p>{{end}}
    {{if .Reminder}}
    <p>This is a reminder that your Jobifai email address hasn't been verified yet.</p>
    {{else}}
    <p>Thanks for registering with Jobifai. To complete your account setup, please verify your email address.</p>
    {{end}}
    <p>
      <a href="{{.VerifyURL}}" class="btn">Verify email address</a>
    </p>
    <p class="url-fallback">Or copy this link into your browser:<br>{{.VerifyURL}}</p>
    <p>This link is valid for <strong>24 hours</strong>. If you didn't create a Jobifai account, you can safely ignore this message.</p>
  </div>
  <div class="footer">
    You received this email because an account was created with this address on Jobifai.
  </div>
</div>
</body>
</html>
`))

// ─── Password reset templates ──────────────────────────────────────────────

var plainPasswordResetTmpl = texttmpl.Must(texttmpl.New("password_reset_plain").Parse(`Reset your Jobifai password
{{if .Name}}
Hi {{.Name}},
{{end}}
We received a request to reset the password for your Jobifai account. Click the link below to set a new password:

  {{.ResetURL}}

This link is valid for 1 hour. If you didn't request a password reset, you can safely ignore this message — your password has not been changed.

— The Jobifai Team
`))

var htmlPasswordResetTmpl = htmltmpl.Must(htmltmpl.New("password_reset_html").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Reset your password</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; background: #f4f4f5; margin: 0; padding: 0; }
    .container { max-width: 560px; margin: 40px auto; background: #ffffff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 4px rgba(0,0,0,0.08); }
    .header { background: #18181b; padding: 24px 32px; }
    .header h1 { color: #ffffff; font-size: 20px; margin: 0; font-weight: 600; }
    .body { padding: 32px; color: #3f3f46; font-size: 15px; line-height: 1.6; }
    .body p { margin: 0 0 16px; }
    .btn { display: inline-block; background: #6366f1; color: #ffffff; text-decoration: none; padding: 12px 24px; border-radius: 6px; font-size: 15px; font-weight: 600; margin: 8px 0 20px; }
    .url-fallback { font-size: 12px; color: #71717a; word-break: break-all; }
    .footer { padding: 20px 32px; border-top: 1px solid #e4e4e7; font-size: 12px; color: #a1a1aa; }
  </style>
</head>
<body>
<div class="container">
  <div class="header">
    <h1>Jobifai</h1>
  </div>
  <div class="body">
    {{if .Name}}<p>Hi {{.Name}},</p>{{end}}
    <p>We received a request to reset the password for your Jobifai account.</p>
    <p>
      <a href="{{.ResetURL}}" class="btn">Reset password</a>
    </p>
    <p class="url-fallback">Or copy this link into your browser:<br>{{.ResetURL}}</p>
    <p>This link is valid for <strong>1 hour</strong>. If you didn't request a password reset, you can safely ignore this message — your password has not been changed.</p>
  </div>
  <div class="footer">
    You received this email because a password reset was requested for your Jobifai account.
  </div>
</div>
</body>
</html>
`))

// RenderPasswordReset renders both plain-text and HTML versions of the
// password-reset email. name may be empty.
func RenderPasswordReset(name, resetURL string) (plain, html string, err error) {
	data := struct {
		Name     string
		ResetURL string
	}{Name: name, ResetURL: resetURL}

	var pb bytes.Buffer
	if err = plainPasswordResetTmpl.Execute(&pb, data); err != nil {
		return "", "", fmt.Errorf("plain template: %w", err)
	}

	var hb bytes.Buffer
	if err = htmlPasswordResetTmpl.Execute(&hb, data); err != nil {
		return "", "", fmt.Errorf("html template: %w", err)
	}

	return pb.String(), hb.String(), nil
}

// RenderVerification renders both plain-text and HTML versions of the
// email-verification message. name may be empty.
func RenderVerification(name, verifyURL string, isReminder bool) (plain, html string, err error) {
	data := struct {
		Name      string
		VerifyURL string
		Reminder  bool
	}{Name: name, VerifyURL: verifyURL, Reminder: isReminder}

	var pb bytes.Buffer
	if err = plainVerificationTmpl.Execute(&pb, data); err != nil {
		return "", "", fmt.Errorf("plain template: %w", err)
	}

	var hb bytes.Buffer
	if err = htmlVerificationTmpl.Execute(&hb, data); err != nil {
		return "", "", fmt.Errorf("html template: %w", err)
	}

	return pb.String(), hb.String(), nil
}
