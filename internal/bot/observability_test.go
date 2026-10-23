package bot

// Observability tests for Story #109: structured lifecycle events, redaction,
// and the once-only quota_stop guard.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/quota"
)

// ── redactURL ─────────────────────────────────────────────────────────────────

func TestRedactURL(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"no query no fragment", "https://example.com/jobs/123", "https://example.com/jobs/123"},
		{"strips query string", "https://example.com/jobs?refId=abc&trackingId=xyz", "https://example.com/jobs"},
		{"strips fragment only", "https://example.com/jobs/123#section", "https://example.com/jobs/123"},
		{"strips query and fragment", "https://example.com/jobs?q=dev#top", "https://example.com/jobs"},
		{"strips embedded credentials", "https://user:password@example.com/path", "https://example.com/path"},
		{"strips credentials with query", "https://user:pass@host.com/p?k=v", "https://host.com/p"},
		{"malformed url", "not a url %%invalid", "[redacted-malformed-url]"},
		{"plain path", "/jobs/view/123456789", "/jobs/view/123456789"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, redactURL(tc.input))
		})
	}
}

// ── finishAttempt: attempt_result lifecycle event ─────────────────────────────

func TestFinishAttempt_LifecycleEvent(t *testing.T) {
	db := openAttemptTestDB(t)
	ac := newAttemptContext("u-obs", "linkedin", "job-42",
		"https://www.linkedin.com/jobs/view/42?trackingId=abc&refId=xyz",
		"Policy Analyst", "GovCo", "automated_linkedin", "run-789")
	beginAttempt(db, ac)

	buf := &bytes.Buffer{}
	orig := log.Logger
	log.Logger = zerolog.New(buf)
	t.Cleanup(func() { log.Logger = orig })

	finishAttempt(db, ac, OutcomeFailed, "browser", "rod_timeout", "rod: timeout after 30s")

	// There should be exactly one attempt_result event.
	var found bool
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal(line, &m), "log line must be valid JSON: %s", line)
		if m["event"] != "attempt_result" {
			continue
		}
		found = true
		assert.Equal(t, "1", m["schema_version"], "schema_version")
		assert.Equal(t, "u-obs", m["user_id"], "user_id")
		assert.Equal(t, "run-789", m["automation_run_id"], "automation_run_id")
		assert.Equal(t, ac.AttemptID, m["attempt_id"], "attempt_id")
		assert.Equal(t, "job-42", m["job_id"], "job_id")
		assert.Equal(t, "linkedin", m["platform"], "platform")
		assert.Equal(t, "failed", m["outcome"], "outcome")
		assert.Equal(t, "rod_timeout", m["failure_code"], "failure_code")

		// URL must be redacted: no query string.
		url, _ := m["url"].(string)
		assert.Equal(t, "https://www.linkedin.com/jobs/view/42", url, "url must be stripped of query string")
		assert.NotContains(t, url, "trackingId", "tracking param must not appear in logged URL")
		assert.NotContains(t, url, "refId", "refId param must not appear in logged URL")
	}
	assert.True(t, found, "attempt_result event not emitted")
}

// ── quota_stop: emitted exactly once ─────────────────────────────────────────

// mockExhaustedMeter always reports Err() = quota.ErrExceeded so that
// DestructiveActionAllowed returns false on the first call to stopReason.
type mockExhaustedMeter struct{ err error }

func (m *mockExhaustedMeter) Activate(_ context.Context) error { return nil }
func (m *mockExhaustedMeter) Deactivate()                      {}
func (m *mockExhaustedMeter) Err() error                       { return m.err }
func (m *mockExhaustedMeter) Stop()                            {}
func (m *mockExhaustedMeter) BillingCtx() context.Context      { return context.Background() }
func (m *mockExhaustedMeter) SetJobID(_ string)                {}
func (m *mockExhaustedMeter) WasActivated() bool               { return true }

func TestQuotaStop_EmittedOnce(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := zerolog.New(buf)

	b := &Bot{
		cfg: Config{
			UserID:          "u-quota",
			AutomationRunID: "run-quota",
			BrowserMeter:    &mockExhaustedMeter{err: quota.ErrExceeded},
		},
		log:    logger,
		stopCh: make(chan struct{}),
	}

	ctx := context.Background()
	// Call stopReason 3 times; the quota_stop event must appear exactly once.
	for range 3 {
		reason := b.stopReason(ctx)
		assert.Equal(t, "browser automation quota exceeded", reason)
	}

	var count int
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal(line, &m))
		if m["event"] == "quota_stop" {
			count++
			assert.Equal(t, "1", m["schema_version"])
			assert.Equal(t, "run-quota", m["automation_run_id"])
			assert.Equal(t, "browser_quota_exceeded", m["failure_code"])
		}
	}
	assert.Equal(t, 1, count, "quota_stop must be emitted exactly once regardless of how many times stopReason is called")
}

func TestQuotaStop_MeterFailureCode(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := zerolog.New(buf)

	meterErr := errors.New("stripe: payment failed") // non-quota error
	b := &Bot{
		cfg: Config{
			BrowserMeter: &mockExhaustedMeter{err: meterErr},
		},
		log:    logger,
		stopCh: make(chan struct{}),
	}

	b.stopReason(context.Background())

	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal(line, &m))
		if m["event"] == "quota_stop" {
			assert.Equal(t, "browser_meter_failure", m["failure_code"],
				"non-quota meter errors must use browser_meter_failure code")
			return
		}
	}
	t.Fatal("quota_stop event not emitted")
}

// ── user_action_required: bounded reason code ─────────────────────────────────

func TestLinkedInReviewOutcome_BoundedCodes(t *testing.T) {
	// linkedInReviewOutcome must always return one of a known set of bounded codes,
	// never raw error text. The user_action_required event copies this code as reason_code.
	cases := []struct {
		err      error
		wantCode string
	}{
		{errLinkedInSubmissionUnknown, "linkedin_submit_unconfirmed"},
		{errLinkedInExternal, "external_apply"},
		{errLinkedInUnknown, "linkedin_listing_unverified"},
		{errLinkedInIdentity, "linkedin_listing_unverified"},
	}
	for _, tc := range cases {
		_, _, code := linkedInReviewOutcome(tc.err)
		assert.Equal(t, tc.wantCode, code, "reason code for %v must be bounded", tc.err)
		// The code must contain no whitespace — it is a machine-readable token.
		assert.NotContains(t, code, " ", "reason code must contain no spaces")
		// The raw error string must NOT appear as the code.
		assert.NotEqual(t, tc.err.Error(), code, "reason code must not be raw error text")
	}
}

// ── sanitizeErr: strip URLs from rod navigation error messages ────────────────

func TestSanitizeErr(t *testing.T) {
	cases := []struct {
		name  string
		input error
		want  string
	}{
		{"nil error", nil, ""},
		{"no url in message", errors.New("context deadline exceeded"), "context deadline exceeded"},
		{"rod navigate with https url",
			errors.New("page Navigate: context deadline exceeded https://www.linkedin.com/jobs/view/123?trackingId=abc&refId=xyz"),
			"page Navigate: context deadline exceeded [url-redacted]"},
		{"rod navigate with http url",
			errors.New("page Navigate: net::ERR_NAME_NOT_RESOLVED http://example.com/apply?ref=seek"),
			"page Navigate: net::ERR_NAME_NOT_RESOLVED [url-redacted]"},
		{"multiple urls in message",
			errors.New("navigate from https://seek.com/job/1?q=a to https://seek.com/apply?ref=b failed"),
			"navigate from [url-redacted] to [url-redacted] failed"},
		{"credentials in url",
			errors.New("connect: dial tcp https://user:pass@host.com/path error"),
			"connect: dial tcp [url-redacted] error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, sanitizeErr(tc.input))
			if tc.input != nil {
				// The raw error text with its URL must not appear in the output.
				assert.NotContains(t, sanitizeErr(tc.input), "trackingId",
					"sanitizeErr must strip query params embedded in error text")
				assert.NotContains(t, sanitizeErr(tc.input), "user:pass",
					"sanitizeErr must strip credentials embedded in error text")
			}
		})
	}
}

// ── captured-output: no raw secret URL in any log line ───────────────────────

// TestNoRawSecretURLInLogOutput is the end-to-end redaction invariant test.
// It feeds a URL containing credentials and tracking parameters through the two
// sanitization paths (redactURL via finishAttempt, sanitizeErr via a nav error)
// and asserts the secret token never appears anywhere in the captured log output —
// not in the "url" field, not in the "error" field, not anywhere.
func TestNoRawSecretURLInLogOutput(t *testing.T) {
	const secretToken = "secret-credential-token-99"
	rawURL := "https://user:" + secretToken + "@www.linkedin.com/jobs/view/99?trackingId=" + secretToken

	db := openAttemptTestDB(t)
	ac := newAttemptContext("u-secret", "linkedin", "job-99", rawURL, "Nurse", "Hospital", "automated_linkedin", "run-secret")
	beginAttempt(db, ac)

	buf := &bytes.Buffer{}
	orig := log.Logger
	log.Logger = zerolog.New(buf)
	t.Cleanup(func() { log.Logger = orig })

	// Path 1: finishAttempt logs attempt_result with URL via redactURL.
	finishAttempt(db, ac, OutcomeFailed, "open_page", "browser_error", "rod: timeout")

	// Path 2: a rod navigation error embeds the raw URL; sanitizeErr must strip it.
	navErr := errors.New("page Navigate: net::ERR_TIMED_OUT " + rawURL)
	log.Logger.Error().Str("error", sanitizeErr(navErr)).Str("job_id", "job-99").Msg("test: open job page")

	// The secret token must not appear anywhere in any log line.
	output := buf.String()
	assert.NotContains(t, output, secretToken,
		"secret credential/tracking token must never appear in log output")
}

