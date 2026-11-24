package bot

// external_ats_test.go — unit tests for Story #134: external ATS handoffs.
//
// Tests focus on the detection, verification, and blocked-state logic that
// does not require a live browser. Browser-bound tests (form filling, navigation)
// use rod HijackRequests to serve controlled HTML at real greenhouse.io URLs
// without touching the network.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

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
		// Partial names do not verify an issuer relationship.
		{"techcorp", "TechCorp Solutions", false},
		{"globalfin", "Global Finance Group", false},
		{"acmeincfraud", "Acme Inc.", false},
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
		`<form id="application_form" method="POST" action="` + actionURL + `">` +
		`<label for="full_name">Full Name</label>` +
		`<input type="text" id="full_name" name="full_name" aria-label="Full Name"/>` +
		`<button type="submit">Next</button>` +
		`</form></body></html>`
}

// ghStep2WorkHTML is served at the second step URL to simulate a real step transition.
const ghStep2WorkHTML = `<!DOCTYPE html><html><body>` +
	`<h1>Work Experience</h1>` +
	`<form id="application_form">` +
	`<label for="job_title">Job Title</label>` +
	`<input type="text" id="job_title" name="job_title" aria-label="Job Title"/>` +
	`<button type="submit">Next</button>` +
	`</form></body></html>`

// ghAccountWallHTML is served to simulate an account-wall block after clicking Next.
const ghAccountWallHTML = `<!DOCTYPE html><html><body>` +
	`<h1>Account Required</h1>` +
	`<form id="application_form">` +
	`<label for="acct_email">Email</label>` +
	`<input type="email" id="acct_email" name="email" aria-label="Email"/>` +
	`<button type="submit">Sign In</button>` +
	`</form>` +
	`<p>Sign in to apply for this position.</p>` +
	`</body></html>`

// ghSnapInterp returns a minimal snapshot + interpretation for the "Full Name" text field.
func ghSnapInterp(t *testing.T, page *rod.Page) (StepSnapshot, StepInterpretation) {
	t.Helper()
	raw, err := page.Eval(jsScanFields, map[string]any{})
	require.NoError(t, err)
	var fields []formField
	require.NoError(t, json.Unmarshal([]byte(raw.Value.String()), &fields))
	snap := captureStepSnapshot(fields, nil, "greenhouse", 0, "Your Information")
	require.Len(t, snap.Fields, 1)
	interp := StepInterpretation{Fields: []FieldResolution{{Ref: snap.Fields[0].Ref, Action: ActionFill, Source: SourceProfile, Value: "Jane Smith", IntentType: FieldIntentProfile, BindingKey: "personal_information.full_name"}}}
	return snap, interp
}

// ghFiller builds a greenhouseRodFiller anchored to acmeinc / job 4567890.
func ghFiller(page *rod.Page) *greenhouseRodFiller {
	b := &Bot{cfg: Config{UserID: "gh-test-user"}, seenCache: newJobSeenCache()}
	return &greenhouseRodFiller{page: page, bot: b, expectedTenant: "acmeinc", expectedJobID: "4567890"}
}

// ── recording store ───────────────────────────────────────────────────────────

// recordingMappingStore wraps DBMappingStore and counts Write calls so tests
// can assert exact write timing and count without relying on upsert idempotency.
type recordingMappingStore struct {
	mu     sync.Mutex
	writes int
	*DBMappingStore
}

func (r *recordingMappingStore) Write(ctx context.Context, snap StepSnapshot, refs []FieldResolution) error {
	r.mu.Lock()
	r.writes++
	r.mu.Unlock()
	return r.DBMappingStore.Write(ctx, snap, refs)
}

func (r *recordingMappingStore) writeCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writes
}

func ghRecordingStore(t *testing.T, userID string) *recordingMappingStore {
	t.Helper()
	db := openProviderTestDB(t)
	return &recordingMappingStore{DBMappingStore: &DBMappingStore{DB: db, UserID: userID}}
}

// ── additional HTML fixtures ──────────────────────────────────────────────────

// ghLoadingOnlyHTML simulates a loading page with no application form.
// greenhouseStepMarker returns "" for this page (no visible controls → never evidence of advance).
const ghLoadingOnlyHTML = `<!DOCTYPE html><html><body><h1>Loading…</h1></body></html>`

// ghCSSHiddenFormHTML has a form whose only text input is CSS-hidden (display:none).
// offsetParent is null for that input → it is excluded from the marker → marker = "".
const ghCSSHiddenFormHTML = `<!DOCTYPE html><html><body>` +
	`<h1>Work Experience</h1>` +
	`<form id="application_form">` +
	`<label for="job_title">Job Title</label>` +
	`<input type="text" id="job_title" name="job_title" style="display:none"/>` +
	`<button type="submit">Next</button>` +
	`</form></body></html>`

// ghSameControlsDiffHeadingHTML has the same visible control (full_name) as ghStep1
// but a different heading. The control-only marker is identical to step 1 → no advance.
const ghSameControlsDiffHeadingHTML = `<!DOCTYPE html><html><body>` +
	`<h1>Personal Details</h1>` +
	`<form id="application_form">` +
	`<label for="full_name">Full Name</label>` +
	`<input type="text" id="full_name" name="full_name" aria-label="Full Name"/>` +
	`<button type="submit">Next</button>` +
	`</form></body></html>`

// ghDOMJobNoTenantHTML has a visible text field (so marker is non-empty and
// destinationInApplication is reached) plus a hidden job_id that matches
// expectedJobID. The URL will carry no tenant slug, so the check must reject it.
const ghDOMJobNoTenantHTML = `<!DOCTYPE html><html><body>` +
	`<h1>Work Experience</h1>` +
	`<form id="application_form">` +
	`<input type="hidden" name="job_id" value="4567890"/>` +
	`<label for="job_title">Job Title</label>` +
	`<input type="text" id="job_title" name="job_title" aria-label="Job Title"/>` +
	`<button type="submit">Next</button>` +
	`</form></body></html>`

// ghVisibilityHiddenFormHTML has a form whose only text input is visibility:hidden.
// Such a control still has a non-null offsetParent (it occupies layout space), so
// only the getComputedStyle visibility check prevents it from entering the marker.
const ghVisibilityHiddenFormHTML = `<!DOCTYPE html><html><body>` +
	`<h1>Work Experience</h1>` +
	`<form id="application_form">` +
	`<label for="job_title">Job Title</label>` +
	`<input type="text" id="job_title" name="job_title" style="visibility:hidden"/>` +
	`<button type="submit">Next</button>` +
	`</form></body></html>`

// TestGreenhouse_MissingIdentity_ZeroWrites asserts that NavigateNext rejects a destination
// whose URL carries no tenant or job identity, and that writeConfirmedStep writes zero
// mappings in that case. The destination has visible form controls to prove that
// destinationInApplication is reached before the rejection.
func TestGreenhouse_MissingIdentity_ZeroWrites(t *testing.T) {
	const (
		initialURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		// omittedURL has no tenant or job slug in the path; both are absent from the URL.
		omittedURL = "https://boards.greenhouse.io/"
	)
	clickDetected := make(chan struct{}, 1)
	page := ghHijackPage(t, func(url string) string {
		switch url {
		case omittedURL:
			select {
			case clickDetected <- struct{}{}:
			default:
			}
			return ghStep2WorkHTML // visible form — marker is non-empty → destinationInApplication reached
		default:
			return ghStep1(omittedURL) // step 1 form; action → URL with no path IDs
		}
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp(t, page)
	filler := ghFiller(page)
	rs := ghRecordingStore(t, "gh-missing-identity")

	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, _ := se.ExecuteStep(context.Background(), snap, interp, filler)
	writeConfirmedStep(context.Background(), rs, snap, interp, result, false)

	// Verify the click happened and the destination appeared before asserting zero writes.
	select {
	case <-clickDetected:
	default:
		t.Fatal("destination was never requested — click did not reach the hijack")
	}
	require.NotEqual(t, StepOutcomeAdvanced, result.Outcome,
		"destination without URL identity must not advance")
	require.Equal(t, 0, rs.writeCount(),
		"destination outside anchored application must write zero mappings")
	t.Logf("EVIDENCE missing-identity | outcome=%s writes=0", result.Outcome)
}

// TestGreenhouse_WrongIdentity_ZeroWrites asserts that NavigateNext rejects a destination
// whose URL carries a different tenant and job than the anchored application.
func TestGreenhouse_WrongIdentity_ZeroWrites(t *testing.T) {
	const (
		initialURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		wrongURL   = "https://boards.greenhouse.io/otherinc/jobs/9999999"
	)
	clickDetected := make(chan struct{}, 1)
	page := ghHijackPage(t, func(url string) string {
		switch url {
		case wrongURL:
			select {
			case clickDetected <- struct{}{}:
			default:
			}
			return ghStep2WorkHTML
		default:
			return ghStep1(wrongURL)
		}
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp(t, page)
	filler := ghFiller(page)
	rs := ghRecordingStore(t, "gh-wrong-identity")

	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, _ := se.ExecuteStep(context.Background(), snap, interp, filler)
	writeConfirmedStep(context.Background(), rs, snap, interp, result, false)

	select {
	case <-clickDetected:
	default:
		t.Fatal("wrong-identity destination was never requested — click did not reach the hijack")
	}
	require.NotEqual(t, StepOutcomeAdvanced, result.Outcome,
		"navigation to wrong tenant/job must not advance")
	require.Equal(t, 0, rs.writeCount(),
		"destination with wrong identity must write zero mappings")
	t.Logf("EVIDENCE wrong-identity | outcome=%s writes=0", result.Outcome)
}

// TestGreenhouse_DOMJobNoTenant_ZeroWrites asserts that NavigateNext rejects a destination
// whose URL carries no tenant slug, even when the form DOM contains a job_id hidden input
// that matches expectedJobID. A matching DOM job ID cannot prove the tenant.
func TestGreenhouse_DOMJobNoTenant_ZeroWrites(t *testing.T) {
	const (
		initialURL  = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		noTenantURL = "https://boards.greenhouse.io/"
	)
	clickDetected := make(chan struct{}, 1)
	page := ghHijackPage(t, func(url string) string {
		switch url {
		case noTenantURL:
			select {
			case clickDetected <- struct{}{}:
			default:
			}
			// DOM has matching job_id + visible controls → marker changes and
			// destinationInApplication is reached, then rejects (no URL tenant).
			return ghDOMJobNoTenantHTML
		default:
			return ghStep1(noTenantURL)
		}
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp(t, page)
	filler := ghFiller(page)
	rs := ghRecordingStore(t, "gh-dom-job-no-tenant")

	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, _ := se.ExecuteStep(context.Background(), snap, interp, filler)
	writeConfirmedStep(context.Background(), rs, snap, interp, result, false)

	select {
	case <-clickDetected:
	default:
		t.Fatal("destination was never requested — click did not reach the hijack")
	}
	// Confirm the DOM job_id was reachable (proves the fixture is wired correctly).
	require.NotEqual(t, StepOutcomeAdvanced, result.Outcome,
		"DOM job_id alone without URL tenant must not advance")
	require.Equal(t, 0, rs.writeCount(),
		"matching DOM job ID without tenant evidence must write zero mappings")
	t.Logf("EVIDENCE dom-job-no-tenant | outcome=%s writes=0", result.Outcome)
}

// TestGreenhouse_QueryStringOnly_SameStep_ZeroWrites asserts that NavigateNext times out
// when the form stays on the same step after a query-string URL change.
//
// Takes ~5 s (NavigateNext deadline).
func TestGreenhouse_QueryStringOnly_SameStep_ZeroWrites(t *testing.T) {
	const (
		initialURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		qsURL      = "https://boards.greenhouse.io/acmeinc/jobs/4567890?page=2"
	)
	clickDetected := make(chan struct{}, 1)
	page := ghHijackPage(t, func(url string) string {
		if url == qsURL {
			select {
			case clickDetected <- struct{}{}:
			default:
			}
		}
		// Same controls at every URL — marker never changes.
		return ghStep1(qsURL)
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp(t, page)
	filler := ghFiller(page)
	rs := ghRecordingStore(t, "gh-same-step")

	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, _ := se.ExecuteStep(context.Background(), snap, interp, filler)
	writeConfirmedStep(context.Background(), rs, snap, interp, result, false)

	select {
	case <-clickDetected:
	default:
		t.Fatal("query-string destination was never requested — click did not reach the hijack")
	}
	require.NotEqual(t, StepOutcomeAdvanced, result.Outcome,
		"query-string URL change with identical form must not advance")
	require.Equal(t, 0, rs.writeCount(),
		"same-step non-advance must write zero mappings")
	t.Logf("EVIDENCE same-step | outcome=%s writes=0", result.Outcome)
}

// TestGreenhouse_CSSHiddenForm_ZeroWrites asserts that NavigateNext times out when the
// destination form has controls but they are all CSS-hidden (display:none). offsetParent
// is null for such controls — they are excluded from the marker — so the marker stays "".
//
// Takes ~5 s (NavigateNext deadline).
func TestGreenhouse_CSSHiddenForm_ZeroWrites(t *testing.T) {
	const (
		initialURL   = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		cssHiddenURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890/page2"
	)
	clickDetected := make(chan struct{}, 1)
	page := ghHijackPage(t, func(url string) string {
		switch url {
		case cssHiddenURL:
			select {
			case clickDetected <- struct{}{}:
			default:
			}
			return ghCSSHiddenFormHTML // controls exist but are CSS-hidden → marker = ""
		default:
			return ghStep1(cssHiddenURL)
		}
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp(t, page)
	filler := ghFiller(page)
	rs := ghRecordingStore(t, "gh-css-hidden")

	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, _ := se.ExecuteStep(context.Background(), snap, interp, filler)
	writeConfirmedStep(context.Background(), rs, snap, interp, result, false)

	select {
	case <-clickDetected:
	default:
		t.Fatal("CSS-hidden destination was never requested — click did not reach the hijack")
	}
	require.NotEqual(t, StepOutcomeAdvanced, result.Outcome,
		"CSS-hidden controls must not advance")
	require.Equal(t, 0, rs.writeCount(),
		"CSS-hidden form must write zero mappings")
	t.Logf("EVIDENCE css-hidden | outcome=%s writes=0", result.Outcome)
}

// TestGreenhouse_VisibilityHiddenForm_ZeroWrites asserts that NavigateNext times out
// when the destination form's controls are visibility:hidden. Such controls have a
// non-null offsetParent (they occupy layout space), so only the getComputedStyle
// visibility check prevents them from entering the step marker.
//
// Takes ~5 s (NavigateNext deadline).
func TestGreenhouse_VisibilityHiddenForm_ZeroWrites(t *testing.T) {
	const (
		initialURL   = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		visHiddenURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890/page3"
	)
	clickDetected := make(chan struct{}, 1)
	page := ghHijackPage(t, func(url string) string {
		switch url {
		case visHiddenURL:
			select {
			case clickDetected <- struct{}{}:
			default:
			}
			return ghVisibilityHiddenFormHTML // controls exist but are visibility:hidden → marker = ""
		default:
			return ghStep1(visHiddenURL)
		}
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp(t, page)
	filler := ghFiller(page)
	rs := ghRecordingStore(t, "gh-vis-hidden")

	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, _ := se.ExecuteStep(context.Background(), snap, interp, filler)
	writeConfirmedStep(context.Background(), rs, snap, interp, result, false)

	select {
	case <-clickDetected:
	default:
		t.Fatal("visibility:hidden destination was never requested — click did not reach the hijack")
	}
	require.NotEqual(t, StepOutcomeAdvanced, result.Outcome,
		"visibility:hidden controls must not advance")
	require.Equal(t, 0, rs.writeCount(),
		"visibility:hidden form must write zero mappings")
	t.Logf("EVIDENCE vis-hidden | outcome=%s writes=0", result.Outcome)
}

// TestGreenhouse_HeadingOnlyChange_ZeroWrites asserts that NavigateNext times out when the
// destination changes the page heading but keeps the same visible controls as the starting
// step. The control-only marker is identical on both sides of the click, so NavigateNext
// never sees a distinct step within its 5 s window.
//
// Takes ~5 s (NavigateNext deadline).
func TestGreenhouse_HeadingOnlyChange_ZeroWrites(t *testing.T) {
	const (
		initialURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		headingURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890/page2"
	)
	clickDetected := make(chan struct{}, 1)
	page := ghHijackPage(t, func(url string) string {
		switch url {
		case headingURL:
			select {
			case clickDetected <- struct{}{}:
			default:
			}
			// Different heading, same semantic control on both sides.
			return ghSameControlsDiffHeadingHTML
		default:
			return ghStep1(headingURL)
		}
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp(t, page)
	filler := ghFiller(page)
	rs := ghRecordingStore(t, "gh-heading-only")
	initialMarker := greenhouseStepMarker(page)

	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, _ := se.ExecuteStep(context.Background(), snap, interp, filler)
	writeConfirmedStep(context.Background(), rs, snap, interp, result, false)

	select {
	case <-clickDetected:
	default:
		t.Fatal("heading-only destination was never requested — click did not reach the hijack")
	}
	// Confirm heading changed but controls are the same (proves the fixture is wired correctly).
	finalMarker := greenhouseStepMarker(filler.page)
	require.Equal(t, initialMarker, finalMarker,
		"marker must still equal step-1 controls after heading-only change")
	require.NotEqual(t, StepOutcomeAdvanced, result.Outcome,
		"heading change with same controls must not advance")
	require.Equal(t, 0, rs.writeCount(),
		"heading-only change must write zero mappings")
	t.Logf("EVIDENCE heading-only | finalMarker=%q outcome=%s writes=0", finalMarker, result.Outcome)
}

// TestGreenhouse_AccountWall_ZeroWrites asserts that NavigateNext rejects an account-wall
// page even when the destination URL correctly carries the anchored tenant+job.
func TestGreenhouse_AccountWall_ZeroWrites(t *testing.T) {
	const (
		initialURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		wallURL    = "https://boards.greenhouse.io/acmeinc/jobs/4567890/account"
	)
	clickDetected := make(chan struct{}, 1)
	page := ghHijackPage(t, func(url string) string {
		switch url {
		case wallURL:
			select {
			case clickDetected <- struct{}{}:
			default:
			}
			return ghAccountWallHTML
		default:
			return ghStep1(wallURL)
		}
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp(t, page)
	filler := ghFiller(page)
	rs := ghRecordingStore(t, "gh-account-wall")

	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, _ := se.ExecuteStep(context.Background(), snap, interp, filler)
	writeConfirmedStep(context.Background(), rs, snap, interp, result, false)

	select {
	case <-clickDetected:
	default:
		t.Fatal("account wall was never requested — click did not reach the hijack")
	}
	// Prove blocked state was reached: destinationBlocked should still report it.
	require.ErrorIs(t, filler.destinationBlocked(), errATSAccountWall,
		"account wall page must still report blocked state after NavigateNext")
	require.NotEqual(t, StepOutcomeAdvanced, result.Outcome,
		"account-wall destination must not advance even with matching tenant/job in URL")
	require.Equal(t, 0, rs.writeCount(),
		"account-wall block must write zero mappings")
	t.Logf("EVIDENCE account-wall | outcome=%s writes=0", result.Outcome)
}

// TestGreenhouse_LoadingScreen_ZeroWrites asserts that NavigateNext times out when the
// destination is a loading page with no application form. The click is confirmed by waiting
// for the loading URL to be requested before asserting zero writes.
//
// Takes ~5 s (NavigateNext deadline).
func TestGreenhouse_LoadingScreen_ZeroWrites(t *testing.T) {
	const (
		initialURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		loadingURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890/page2"
	)
	clickDetected := make(chan struct{}, 1)
	page := ghHijackPage(t, func(url string) string {
		switch url {
		case loadingURL:
			select {
			case clickDetected <- struct{}{}:
			default:
			}
			return ghLoadingOnlyHTML // no form → marker stays ""
		default:
			return ghStep1(loadingURL)
		}
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp(t, page)
	filler := ghFiller(page)
	rs := ghRecordingStore(t, "gh-loading-only")

	type execOut struct {
		result StepExecResult
		err    error
	}
	done := make(chan execOut, 1)
	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	go func() {
		r, e := se.ExecuteStep(context.Background(), snap, interp, filler)
		done <- execOut{r, e}
	}()

	// Wait for the loading URL to be requested — proves the click happened.
	select {
	case <-clickDetected:
	case <-time.After(10 * time.Second):
		t.Fatal("loading URL was never requested — click did not reach the hijack within 10s")
	}

	out := <-done // blocks ~5s while NavigateNext deadline expires
	writeConfirmedStep(context.Background(), rs, snap, interp, out.result, false)

	require.NotEqual(t, StepOutcomeAdvanced, out.result.Outcome,
		"loading screen must not advance")
	require.Equal(t, 0, rs.writeCount(),
		"loading screen must write zero mappings")
	t.Logf("EVIDENCE loading-only | outcome=%s writes=0", out.result.Outcome)
}

// TestGreenhouse_LoadingThenRealStep_OneWrite proves write timing: the test holds the browser
// on the loading screen, asserts zero writes while loading is active, then releases the
// browser to serve the real next step, and asserts exactly one write containing only the
// FilledRefs from the preceding step.
func TestGreenhouse_LoadingThenRealStep_OneWrite(t *testing.T) {
	const (
		initialURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		step2URL   = "https://boards.greenhouse.io/acmeinc/jobs/4567890/page2"
	)

	clickDetected := make(chan struct{}, 1)
	releaseLoading := make(chan struct{})

	page := ghHijackPage(t, func(url string) string {
		switch url {
		case step2URL:
			// Signal click on first request; block until the test releases.
			select {
			case clickDetected <- struct{}{}:
			default:
			}
			<-releaseLoading
			return ghStep2WorkHTML
		default:
			return ghStep1(step2URL)
		}
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp(t, page)
	filler := ghFiller(page)
	rs := ghRecordingStore(t, "gh-load-then-step")

	type execOut struct {
		result StepExecResult
		err    error
	}
	done := make(chan execOut, 1)
	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	go func() {
		r, e := se.ExecuteStep(context.Background(), snap, interp, filler)
		done <- execOut{r, e}
	}()

	// Wait for the click to be confirmed (browser requested step2URL).
	select {
	case <-clickDetected:
	case <-time.After(10 * time.Second):
		t.Fatal("step2URL was never requested — click did not reach the hijack within 10s")
	}

	// Browser is blocked on the loading screen: assert zero writes at this point.
	require.Equal(t, 0, rs.writeCount(),
		"no write expected while browser is held on loading screen")

	// Release the loading screen → browser receives step2 HTML → NavigateNext advances.
	close(releaseLoading)

	out := <-done
	writeConfirmedStep(context.Background(), rs, snap, interp, out.result, false)

	require.NoError(t, out.err, "execution must not error on successful advance")
	require.Equal(t, StepOutcomeAdvanced, out.result.Outcome,
		"genuine next step after loading must advance")
	require.Equal(t, 1, rs.writeCount(),
		"exactly one write for the confirmed step")
	require.Equal(t, []string{"f0"}, out.result.FilledRefs,
		"only the filled ref from the preceding step must be recorded")
	t.Logf("EVIDENCE load-then-step | outcome=%s writes=1 filledRefs=%v",
		out.result.Outcome, out.result.FilledRefs)
}

// TestGreenhouse_RealAdvance_WritesFilledRefs asserts that NavigateNext accepts a genuine
// next step in the same application and that writeConfirmedStep persists exactly one write
// containing only the step's FilledRefs.
func TestGreenhouse_RealAdvance_WritesFilledRefs(t *testing.T) {
	const (
		initialURL = "https://boards.greenhouse.io/acmeinc/jobs/4567890"
		step2URL   = "https://boards.greenhouse.io/acmeinc/jobs/4567890/page2"
	)
	page := ghHijackPage(t, func(url string) string {
		switch url {
		case step2URL:
			return ghStep2WorkHTML
		default:
			return ghStep1(step2URL)
		}
	})
	require.NoError(t, page.Navigate(initialURL))

	snap, interp := ghSnapInterp(t, page)
	filler := ghFiller(page)
	rs := ghRecordingStore(t, "gh-real-advance")

	se := NewStepExecutor(DefaultStepExecBounds(), nil)
	result, execErr := se.ExecuteStep(context.Background(), snap, interp, filler)
	writeConfirmedStep(context.Background(), rs, snap, interp, result, false)

	require.NoError(t, execErr, "execution must not error on successful advance")
	require.Equal(t, StepOutcomeAdvanced, result.Outcome,
		"genuine next step must advance")
	require.Equal(t, 1, rs.writeCount(),
		"real advance must write exactly once via production gate")
	require.Equal(t, []string{"f0"}, result.FilledRefs,
		"only the filled ref from the preceding step must be recorded")
	t.Logf("EVIDENCE real-advance | outcome=%s writes=1 filledRefs=%v",
		result.Outcome, result.FilledRefs)
}
