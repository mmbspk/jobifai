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

// ─── Email-change templates ────────────────────────────────────────────────

var plainEmailChangeVerifyTmpl = texttmpl.Must(texttmpl.New("email_change_verify_plain").Parse(`Change your Jobifai email address
{{if .Name}}
Hi {{.Name}},
{{end}}
Someone requested that this email address become the account email for a Jobifai account. Click the link below to confirm this change:

  {{.VerifyURL}}

This link is valid for 24 hours. If you did not request this, you can safely ignore this message — your account has not been changed.

— The Jobifai Team
`))

var htmlEmailChangeVerifyTmpl = htmltmpl.Must(htmltmpl.New("email_change_verify_html").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Confirm your new email address</title>
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
  <div class="header"><h1>Jobifai</h1></div>
  <div class="body">
    {{if .Name}}<p>Hi {{.Name}},</p>{{end}}
    <p>Someone requested that this email address become the account email for a Jobifai account.</p>
    <p>Click the button below to confirm this change:</p>
    <p><a href="{{.VerifyURL}}" class="btn">Confirm new email address</a></p>
    <p class="url-fallback">Or copy this link into your browser:<br>{{.VerifyURL}}</p>
    <p>This link is valid for <strong>24 hours</strong>. If you didn't request this, you can safely ignore this message — your account has not been changed.</p>
  </div>
  <div class="footer">You received this email because a request was made to use this address for a Jobifai account.</div>
</div>
</body>
</html>
`))

var plainEmailChangeOldTmpl = texttmpl.Must(texttmpl.New("email_change_old_plain").Parse(`Important: your Jobifai email address was changed
{{if .Name}}
Hi {{.Name}},
{{end}}
The email address on your Jobifai account has been changed to {{.NewEmail}}.

If you made this change, no further action is needed.

If you did not make this change, please contact us immediately so we can help secure your account.

— The Jobifai Team
`))

var htmlEmailChangeOldTmpl = htmltmpl.Must(htmltmpl.New("email_change_old_html").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Your Jobifai email address was changed</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; background: #f4f4f5; margin: 0; padding: 0; }
    .container { max-width: 560px; margin: 40px auto; background: #ffffff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 4px rgba(0,0,0,0.08); }
    .header { background: #18181b; padding: 24px 32px; }
    .header h1 { color: #ffffff; font-size: 20px; margin: 0; font-weight: 600; }
    .body { padding: 32px; color: #3f3f46; font-size: 15px; line-height: 1.6; }
    .body p { margin: 0 0 16px; }
    .footer { padding: 20px 32px; border-top: 1px solid #e4e4e7; font-size: 12px; color: #a1a1aa; }
  </style>
</head>
<body>
<div class="container">
  <div class="header"><h1>Jobifai</h1></div>
  <div class="body">
    {{if .Name}}<p>Hi {{.Name}},</p>{{end}}
    <p>The email address on your Jobifai account has been changed to <strong>{{.NewEmail}}</strong>.</p>
    <p>If you made this change, no further action is needed.</p>
    <p>If you did not make this change, please contact us immediately so we can help secure your account.</p>
  </div>
  <div class="footer">You received this email because the account address previously associated with your Jobifai account was changed.</div>
</div>
</body>
</html>
`))

var plainEmailChangeNewTmpl = texttmpl.Must(texttmpl.New("email_change_new_plain").Parse(`Your Jobifai email address is confirmed
{{if .Name}}
Hi {{.Name}},
{{end}}
This email address is now the account email for your Jobifai account.

You have been signed out of all other sessions. Please sign in again using this address.

— The Jobifai Team
`))

var htmlEmailChangeNewTmpl = htmltmpl.Must(htmltmpl.New("email_change_new_html").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Your Jobifai email address is confirmed</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; background: #f4f4f5; margin: 0; padding: 0; }
    .container { max-width: 560px; margin: 40px auto; background: #ffffff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 4px rgba(0,0,0,0.08); }
    .header { background: #18181b; padding: 24px 32px; }
    .header h1 { color: #ffffff; font-size: 20px; margin: 0; font-weight: 600; }
    .body { padding: 32px; color: #3f3f46; font-size: 15px; line-height: 1.6; }
    .body p { margin: 0 0 16px; }
    .footer { padding: 20px 32px; border-top: 1px solid #e4e4e7; font-size: 12px; color: #a1a1aa; }
  </style>
</head>
<body>
<div class="container">
  <div class="header"><h1>Jobifai</h1></div>
  <div class="body">
    {{if .Name}}<p>Hi {{.Name}},</p>{{end}}
    <p>This email address is now the account email for your Jobifai account.</p>
    <p>You have been signed out of all other sessions. Please sign in again using this address.</p>
  </div>
  <div class="footer">You received this email because your Jobifai account email was updated to this address.</div>
</div>
</body>
</html>
`))

// RenderEmailChangeVerification renders the verification email sent to the new address.
func RenderEmailChangeVerification(name, verifyURL string) (plain, html string, err error) {
	data := struct {
		Name      string
		VerifyURL string
	}{Name: name, VerifyURL: verifyURL}
	var pb, hb bytes.Buffer
	if err = plainEmailChangeVerifyTmpl.Execute(&pb, data); err != nil {
		return "", "", fmt.Errorf("plain template: %w", err)
	}
	if err = htmlEmailChangeVerifyTmpl.Execute(&hb, data); err != nil {
		return "", "", fmt.Errorf("html template: %w", err)
	}
	return pb.String(), hb.String(), nil
}

// RenderEmailChangeOldNotification renders the security notification sent to the old address.
func RenderEmailChangeOldNotification(name, newEmail string) (plain, html string, err error) {
	data := struct {
		Name     string
		NewEmail string
	}{Name: name, NewEmail: newEmail}
	var pb, hb bytes.Buffer
	if err = plainEmailChangeOldTmpl.Execute(&pb, data); err != nil {
		return "", "", fmt.Errorf("plain template: %w", err)
	}
	if err = htmlEmailChangeOldTmpl.Execute(&hb, data); err != nil {
		return "", "", fmt.Errorf("html template: %w", err)
	}
	return pb.String(), hb.String(), nil
}

// RenderEmailChangeNewConfirmation renders the confirmation sent to the newly active address.
func RenderEmailChangeNewConfirmation(name string) (plain, html string, err error) {
	data := struct{ Name string }{Name: name}
	var pb, hb bytes.Buffer
	if err = plainEmailChangeNewTmpl.Execute(&pb, data); err != nil {
		return "", "", fmt.Errorf("plain template: %w", err)
	}
	if err = htmlEmailChangeNewTmpl.Execute(&hb, data); err != nil {
		return "", "", fmt.Errorf("html template: %w", err)
	}
	return pb.String(), hb.String(), nil
}
