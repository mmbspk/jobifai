package bot

// external_ats_test.go — unit tests for Story #134: external ATS handoffs.
//
// Tests focus on the detection, verification, and blocked-state logic that
// does not require a live browser. Browser-bound tests (form filling, navigation)
// use rod HijackRequests to serve controlled HTML at real greenhouse.io URLs
// without touching the network.

import (
	"context"
	"testing"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
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

// ── browser-level NavigateNext + writeConfirmedStep tests for Greenhouse ───────
//
// These tests exercise the full production navigation + persistence path:
//   1. Browser loads a hijacked Greenhouse form at a real greenhouse.io URL.
//   2. StepExecutor fills one field and calls NavigateNext (via ExecuteStep).
//   3. The click triggers a navigation to a hijacked destination URL.
//   4. NavigateNext checks the destination's identity and step marker.
//   5. writeConfirmedStep is called, and the mapping store is inspected.
//
// No real network traffic: all requests are intercepted by HijackRequests and
// served from constants below.

// ghHijackPage creates a headless page whose network is fully intercepted. Every
// request URL is passed to dispatch, which returns the HTML to serve.
func ghHijackPage(t *testing.T, dispatch func(url string) string) *rod.Page {
	t.Helper()
	launchURL, err := launcher.New().Headless(true).Leakless(false).NoSandbox(true).Launch()
	if err != nil {
		t.Skipf("chrome unavailable for Greenhouse browser test: %v", err)
	}
	browser := rod.New().ControlURL(launchURL)
	if err := browser.Connect(); err != nil {
		t.Skipf("chrome connect failed: %v", err)
	}
	t.Cleanup(func() { _ = browser.Close() })
	page := browser.MustPage("")
	router := page.HijackRequests()
	t.Cleanup(func() { router.MustStop() })
	router.MustAdd("*", func(ctx *rod.Hijack) {
		ctx.Response.SetHeader("Content-Type", "text/html; charset=utf-8")
		ctx.Response.SetBody(dispatch(ctx.Request.URL().String()))
	})
	go router.Run()
	return page
}

// ghStep1 returns a Greenhouse "Your Information" form HTML with the given submit action URL.
func ghStep1(actionURL string) string {
	return `<!DOCTYPE html><html><body>` +
		`<h1>Your Information</h1>` +
		`<form method="POST" action="` + actionURL + `">` +
		`<label for="full_name">Full Name</label>` +
		`<input type="text" id="full_name" name="full_name" aria-label="Full Name"/>` +
		`<button type="submit">Next</button>` +
		`</form></body></html>`
}

// ghStep2WorkHTML is served at the second step URL to simulate a real step transition.
const ghStep2WorkHTML = `<!DOCTYPE html><html><body>` +
	`<h1>Work Experience</h1>` +
	`<form>` +
	`<label for="job_title">Job Title</label>` +
	`<input type="text" id="job_title" name="job_title" aria-label="Job Title"/>` +
	`<button type="submit">Next</button>` +
	`</form></body></html>`

// ghAccountWallHTML is served to simulate an account-wall block after clicking Next.
const ghAccountWallHTML = `<!DOCTYPE html><html><body>` +
	`<h1>Account Required</h1>` +
	`<form>` +
	`<label for="acct_email">Email</label>` +
	`<input type="email" id="acct_email" name="email" aria-label="Email"/>` +
	`<button type="submit">Sign In</button>` +
	`</form>` +
	`<p>Sign in to apply for this position.</p>` +
	`</body></html>`

// ghSnapInterp returns a minimal snapshot + interpretation for the "Full Name" text field.
func ghSnapInterp() (StepSnapshot, StepInterpretation) {
	snap := captureStepSnapshot([]formField{
		{Type: "text", Question: "Full Name", ID: "full_name", InputType: "text"},
	}, nil, "greenhouse", 0, "Your Information")
	interp := StepInterpretation{
		Fields: []FieldResolution{{Ref: "f0", Action: ActionFill, Source: SourceLLM, Value: "Jane Smith"}},
	}
	return snap, interp
}

// ghFiller builds a greenhouseRodFiller anchored to acmeinc / job 4567890.
func ghFiller(page *rod.Page) *greenhouseRodFiller {
	b := &Bot{cfg: Config{UserID: "gh-test-user"}, seenCache: newJobSeenCache()}
	return &greenhouseRodFiller{page: page, bot: b, expectedTenant: "acmeinc", expectedJobID: "4567890"}
}

// TestGreenhouse_MissingIdentity_ZeroWrites asserts that NavigateNext rejects a destination
// whose URL carries a different tenant/job than the anchored application, and that
// writeConfirmedStep writes zero mappings in that case.
//
// EVIDENCE target: advanced=false, outcome=error, wrote=false
func TestGreenhouse_MissingIdentity_ZeroWrites(t *testing.T) {
	const (
		initialURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		otherURL   = "https://boards.greenhouse.io/otherinc/jobs/9999999" // different tenant+job
	)
	page := ghHijackPage(t, func(url string) string {
		switch url {
		case otherURL:
			return ghStep2WorkHTML // different form structure — real step transition shape
		default:
			return ghStep1(otherURL) // step 1 form; action → different application
		}
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp()
	filler := ghFiller(page)
	db := openProviderTestDB(t)
	store := &DBMappingStore{DB: db, UserID: "gh-missing-identity"}

	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, _ := se.ExecuteStep(context.Background(), snap, interp, filler)
	writeConfirmedStep(context.Background(), store, snap, interp, result, false)

	require.NotEqual(t, StepOutcomeAdvanced, result.Outcome,
		"navigation to wrong tenant/job must not advance")
	require.False(t, writeGateHasField(store, snap),
		"destination outside anchored application must write zero mappings")
	t.Logf("EVIDENCE missing-identity | advanced=%v outcome=%s wrote=false",
		result.Outcome == StepOutcomeAdvanced, result.Outcome)
}

// TestGreenhouse_QueryStringOnly_SameStep_ZeroWrites asserts that NavigateNext times out
// when the form stays on the same step after a query-string URL change, and that
// writeConfirmedStep writes zero mappings in that case.
//
// This test takes ~5 s because NavigateNext polls until its 5-second deadline expires.
//
// EVIDENCE target: advanced=false, outcome=error, wrote=false
func TestGreenhouse_QueryStringOnly_SameStep_ZeroWrites(t *testing.T) {
	const (
		initialURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		qsURL      = "https://boards.greenhouse.io/acmeinc/jobs/4567890?page=2"
	)
	// Both URLs serve the identical step-1 HTML — same heading, same field IDs.
	// The URL changes (query string added) but greenhouseStepMarker never changes.
	page := ghHijackPage(t, func(url string) string {
		return ghStep1(qsURL) // same structure at every URL
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp()
	filler := ghFiller(page)
	db := openProviderTestDB(t)
	store := &DBMappingStore{DB: db, UserID: "gh-same-step"}

	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, _ := se.ExecuteStep(context.Background(), snap, interp, filler)
	writeConfirmedStep(context.Background(), store, snap, interp, result, false)

	require.NotEqual(t, StepOutcomeAdvanced, result.Outcome,
		"query-string URL change with identical form must not advance")
	require.False(t, writeGateHasField(store, snap),
		"same-step non-advance must write zero mappings")
	t.Logf("EVIDENCE same-step | advanced=%v outcome=%s wrote=false",
		result.Outcome == StepOutcomeAdvanced, result.Outcome)
}

// TestGreenhouse_AccountWall_ZeroWrites asserts that NavigateNext rejects an account-wall
// page even when the destination URL correctly carries the anchored tenant+job, and that
// writeConfirmedStep writes zero mappings in that case.
//
// EVIDENCE target: advanced=false, outcome=error, wrote=false
func TestGreenhouse_AccountWall_ZeroWrites(t *testing.T) {
	const (
		initialURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		wallURL    = "https://boards.greenhouse.io/acmeinc/jobs/4567890/account"
	)
	page := ghHijackPage(t, func(url string) string {
		switch url {
		case wallURL:
			return ghAccountWallHTML // different form structure + blocked text
		default:
			return ghStep1(wallURL) // step 1; action → wall page (IDs match in URL)
		}
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp()
	filler := ghFiller(page)
	db := openProviderTestDB(t)
	store := &DBMappingStore{DB: db, UserID: "gh-account-wall"}

	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, _ := se.ExecuteStep(context.Background(), snap, interp, filler)
	writeConfirmedStep(context.Background(), store, snap, interp, result, false)

	require.NotEqual(t, StepOutcomeAdvanced, result.Outcome,
		"account-wall destination must not advance even with matching tenant/job in URL")
	require.False(t, writeGateHasField(store, snap),
		"account-wall block must write zero mappings")
	t.Logf("EVIDENCE account-wall | advanced=%v outcome=%s wrote=false",
		result.Outcome == StepOutcomeAdvanced, result.Outcome)
}

// TestGreenhouse_RealAdvance_WritesFilledRefs asserts that NavigateNext accepts a genuine
// next step in the same application (new form structure, same tenant+job in URL), and that
// writeConfirmedStep persists only the step's filled refs, exactly once.
//
// EVIDENCE target: advanced=true, outcome=advanced, wrote=true, filledRefs=[f0]
func TestGreenhouse_RealAdvance_WritesFilledRefs(t *testing.T) {
	const (
		initialURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		step2URL   = "https://boards.greenhouse.io/acmeinc/jobs/4567890/page2"
	)
	page := ghHijackPage(t, func(url string) string {
		switch url {
		case step2URL:
			return ghStep2WorkHTML // different heading + field IDs — genuine step transition
		default:
			return ghStep1(step2URL) // step 1; action → step 2 (same tenant+job in URL path)
		}
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp()
	filler := ghFiller(page)
	db := openProviderTestDB(t)
	store := &DBMappingStore{DB: db, UserID: "gh-real-advance"}

	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, execErr := se.ExecuteStep(context.Background(), snap, interp, filler)
	writeConfirmedStep(context.Background(), store, snap, interp, result, false)

	require.NoError(t, execErr, "execution must not error on successful advance")
	require.Equal(t, StepOutcomeAdvanced, result.Outcome,
		"genuine next step must advance")
	require.True(t, writeGateHasField(store, snap),
		"real advance must write the preceding step's filled ref via production gate")
	require.Equal(t, []string{"f0"}, result.FilledRefs,
		"only the filled ref from the preceding step must be recorded")
	t.Logf("EVIDENCE real-advance | outcome=%s filledRefs=%v wrote=true", result.Outcome, result.FilledRefs)
}
