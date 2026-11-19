package bot

// external_ats_test.go — unit tests for Story #134: external ATS handoffs.
//
// Tests focus on the detection, verification, and blocked-state logic that
// does not require a live browser. Browser-bound tests (form filling, navigation)
// require a real Greenhouse page and are tagged as integration-only.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── URL classification ─────────��──────────────────────────────────────────────

func TestClassifyExternalATS_Greenhouse(t *testing.T) {
	cases := []string{
		"https://boards.greenhouse.io/acmeinc/jobs/4567890",
		"https://boards.greenhouse.io/acmeinc/jobs/4567890/apply",
		"https://job-boards.greenhouse.io/techcorp/jobs/1234567",
		"https://job-boards.greenhouse.io/techcorp/jobs/1234567/apply",
	}
	for _, u := range cases {
		got := classifyExternalATS(u)
		assert.Equal(t, ATSKindGreenhouse, got, "URL: %s", u)
	}
}

func TestClassifyExternalATS_Unknown(t *testing.T) {
	cases := []string{
		"https://workday.com/jobs/123",
		"https://lever.co/acme/abc123",
		"https://linkedin.com/jobs/view/4567890",
		"https://apply.greenhouse.com/acme/jobs/123", // wrong domain
		"",
		"not-a-url",
	}
	for _, u := range cases {
		got := classifyExternalATS(u)
		assert.Equal(t, ATSKindUnknown, got, "URL: %s", u)
	}
}

// ── Greenhouse URL parsing ────────────────────────────────────────────────────

func TestGreenhouseTenant(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://boards.greenhouse.io/acmeinc/jobs/4567890", "acmeinc"},
		{"https://boards.greenhouse.io/acmeinc/jobs/4567890/apply", "acmeinc"},
		{"https://job-boards.greenhouse.io/techcorp/jobs/1234567", "techcorp"},
		{"https://boards.greenhouse.io/my-company/jobs/999", "my-company"},
	}
	for _, tc := range cases {
		got := greenhouseTenant(tc.url)
		assert.Equal(t, tc.want, got, "URL: %s", tc.url)
	}
}

func TestGreenhouseTenant_Invalid(t *testing.T) {
	cases := []string{
		"https://boards.greenhouse.io/",
		"https://boards.greenhouse.io/acmeinc",
		"not-a-url",
		"",
	}
	for _, u := range cases {
		got := greenhouseTenant(u)
		assert.Equal(t, "", got, "URL: %s", u)
	}
}

func TestGreenhouseJobID(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://boards.greenhouse.io/acmeinc/jobs/4567890", "4567890"},
		{"https://boards.greenhouse.io/acmeinc/jobs/4567890/apply", "4567890"},
		{"https://job-boards.greenhouse.io/techcorp/jobs/1234567", "1234567"},
	}
	for _, tc := range cases {
		got := greenhouseJobID(tc.url)
		assert.Equal(t, tc.want, got, "URL: %s", tc.url)
	}
}

// ── tenant → company matching ─────────────────────────────────────────────────

func TestTenantMatchesCompany(t *testing.T) {
	cases := []struct {
		tenant  string
		company string
		want    bool
	}{
		// Exact match after normalization.
		{"acmeinc", "Acme Inc.", true},
		{"acme", "Acme Corporation", true},
		// Slug is a prefix of company name.
		{"techcorp", "TechCorp Solutions", true},
		// Company name contains tenant slug.
		{"globalfin", "Global Finance Group", true},
		// Mismatch — different companies.
		{"acmeinc", "Beta Industries", false},
		{"gamma", "Alpha Corp", false},
		// Empty inputs are always false.
		{"", "Acme Inc.", false},
		{"acmeinc", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got := tenantMatchesCompany(tc.tenant, tc.company)
		assert.Equal(t, tc.want, got, "tenant=%q company=%q", tc.tenant, tc.company)
	}
}

// ── blocked state detection ───────────────────────────────────────────────────

func TestCheckATSBlockedState_ClosedJob(t *testing.T) {
	texts := []string{
		"This job is no longer available.",
		"Sorry, this job has been filled.",
		"We are no longer accepting applications for this position.",
	}
	for _, text := range texts {
		err := checkATSBlockedState(text)
		require.Error(t, err, "text: %q", text)
		assert.ErrorIs(t, err, errATSClosedJob, "text: %q", text)
	}
}

func TestCheckATSBlockedState_AccountWall(t *testing.T) {
	texts := []string{
		"Sign in to apply for this position.",
		"Please create an account to continue your application.",
		"Log in to apply for this role.",
	}
	for _, text := range texts {
		err := checkATSBlockedState(text)
		require.Error(t, err, "text: %q", text)
		assert.ErrorIs(t, err, errATSAccountWall, "text: %q", text)
	}
}

func TestCheckATSBlockedState_Captcha(t *testing.T) {
	texts := []string{
		"Please verify you are human — CAPTCHA required.",
		"Are you a robot? Please complete verification.",
		"Cloudflare protection detected.",
	}
	for _, text := range texts {
		err := checkATSBlockedState(text)
		require.Error(t, err, "text: %q", text)
		assert.ErrorIs(t, err, errATSCaptcha, "text: %q", text)
	}
}

func TestCheckATSBlockedState_NormalPage_NoError(t *testing.T) {
	texts := []string{
		"Apply for Software Engineer at Acme Inc.",
		"Full name: * Email: * Phone: *",
		"Submit your application",
	}
	for _, text := range texts {
		err := checkATSBlockedState(text)
		assert.NoError(t, err, "text: %q should not trigger blocked state", text)
	}
}

// ── confirmation detection ────────────────────────────────────────────────────

func TestIsATSConfirmed_ConfirmedPages(t *testing.T) {
	texts := []string{
		"Thank you for applying to Acme Inc.!",
		"Your application has been submitted successfully.",
		"Application submitted — we'll be in touch.",
		"We've received your application.",
		"Application complete. We will review your submission.",
	}
	for _, text := range texts {
		assert.True(t, isATSConfirmed(text), "text: %q should be confirmed", text)
	}
}

func TestIsATSConfirmed_UnconfirmedPages(t *testing.T) {
	texts := []string{
		"Apply for Software Engineer at Acme Inc.",
		"Submit Application",
		"",
	}
	for _, text := range texts {
		assert.False(t, isATSConfirmed(text), "text: %q should NOT be confirmed", text)
	}
}

// ── dispatch ──────────────────────────────────────────────────────────────────

func TestExternalATSApply_UnsupportedATS_ReturnsError(t *testing.T) {
	b := newTestBot(t)
	err := b.externalATSApply(context.TODO(), nil, nil, nil, "https://workday.com/jobs/123")
	require.Error(t, err)
	assert.ErrorIs(t, err, errATSUnsupported)
}

// ── Greenhouse apply pre-flight ───────────────────────────────────────────────

func TestGreenhouseApply_TenantMismatch_ReturnsError(t *testing.T) {
	b := newTestBot(t)
	lazy := &lazyDocGen{job: linkedInJob{Company: "Beta Industries", ID: "job-1"}}
	// Tenant "acmeinc" does not match "Beta Industries".
	err := b.greenhouseApply(context.TODO(), nil, lazy, &AttemptContext{}, "https://boards.greenhouse.io/acmeinc/jobs/4567890")
	require.Error(t, err)
	assert.ErrorIs(t, err, errATSTenantMismatch)
}

func TestGreenhouseApply_BadURL_ReturnsError(t *testing.T) {
	b := newTestBot(t)
	lazy := &lazyDocGen{job: linkedInJob{Company: "Acme Inc.", ID: "job-1"}}
	err := b.greenhouseApply(context.TODO(), nil, lazy, &AttemptContext{}, "https://boards.greenhouse.io/acmeinc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot parse Greenhouse URL")
}

// ── redirect chain verification ───────────────────────────────────────────────

func TestGreenhouseTenant_RedirectPreservesJobID(t *testing.T) {
	// Verify that after a /apply suffix redirect the tenant and job ID are still parseable.
	raw := "https://boards.greenhouse.io/acmeinc/jobs/4567890/apply"
	assert.Equal(t, "acmeinc", greenhouseTenant(raw))
	assert.Equal(t, "4567890", greenhouseJobID(raw))
}

// ── employer mismatch in redirect chain ──────────────────────────────────────

func TestClassifyAndTenant_DifferentDomainAfterRedirect(t *testing.T) {
	// Simulates a redirect that leaves greenhouse.io entirely.
	landed := "https://workday.com/acme/jobs/123"
	assert.Equal(t, ATSKindUnknown, classifyExternalATS(landed))
}
