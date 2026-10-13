package handler_test

// Regression tests proving that an overlong Idempotency-Key header is rejected
// BEFORE any billable work — LLM calls, PDF rendering, and resource metering must
// all be zero when the key validation fires early in the handler.
//
// The invariant: malformed Idempotency-Key → HTTP 400 → zero LLM calls →
// zero render calls → zero BeforeRender calls → zero RecordRender calls.

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/resume"
)

// ─── Counting stubs ──────────────────────────────────────────────────────────

// countingRenderer wraps stubRenderer and counts every RenderResume / RenderCoverLetter call.
type countingRenderer struct {
	resumeCalls atomic.Int32
	coverCalls  atomic.Int32
}

func (c *countingRenderer) RenderResume(_ context.Context, _ *domain.ResumeProfile, _, _ string, _ *resume.RenderOptions) ([]byte, error) {
	c.resumeCalls.Add(1)
	return []byte("%PDF-counting-stub"), nil
}

func (c *countingRenderer) RenderCoverLetter(_ context.Context, _ string, _, _ string) ([]byte, error) {
	c.coverCalls.Add(1)
	return []byte("%PDF-counting-cover-stub"), nil
}

// countingTailor wraps stubTailor and counts every TailorProfile / WriteCoverLetter call.
type countingTailor struct {
	tailorCalls atomic.Int32
	coverCalls  atomic.Int32
}

func (c *countingTailor) TailorProfile(_ context.Context, profile *domain.ResumeProfile, _ string) (*domain.ResumeProfile, error) {
	c.tailorCalls.Add(1)
	return profile, nil
}

func (c *countingTailor) WriteCoverLetter(_ context.Context, _ *domain.ResumeProfile, _, _ string) (string, error) {
	c.coverCalls.Add(1)
	return "Dear hiring team,\n", nil
}

// countingExtractor wraps stubExtractor and counts every ExtractFromText call.
type countingExtractor struct {
	calls atomic.Int32
}

func (c *countingExtractor) ExtractFromText(_ context.Context, _ string) (*domain.ResumeProfile, error) {
	c.calls.Add(1)
	return &domain.ResumeProfile{Summary: "extracted"}, nil
}

// countingResourceStub extends recordingResourceStub to expose explicit before/record counters.
type countingResourceStub struct {
	recordingResourceStub
	beforeN atomic.Int32
	recordN atomic.Int32
}

func newCountingResourceStub() *countingResourceStub {
	s := &countingResourceStub{}
	s.burns = make(map[string]int)
	return s
}

func (s *countingResourceStub) BeforeRender(_ context.Context, _ string) error {
	s.beforeN.Add(1)
	return nil
}

func (s *countingResourceStub) RecordRender(_ context.Context, _ string, idempotencyKey string) error {
	s.recordN.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.burns[idempotencyKey]++
	return nil
}

// ─── Router builder ──────────────────────────────────────────────────────────

// earlyValidationRouterParts groups all stubs used by the early-validation tests.
type earlyValidationRouterParts struct {
	router    http.Handler
	renderer  *countingRenderer
	extractor *countingExtractor
	tailor    *countingTailor
	resource  *countingResourceStub
}

func newEarlyValidationRouter(t *testing.T) earlyValidationRouterParts {
	t.Helper()
	svc, _ := newTestServices(t)

	renderer := &countingRenderer{}
	extractor := &countingExtractor{}
	tailor := &countingTailor{}
	resource := newCountingResourceStub()

	svc.Renderer = renderer
	svc.LLMFactory = func(string) (handler.ResumeExtractor, handler.ResumeTailor) {
		return extractor, tailor
	}
	svc.EvaluatorFactory = func(string) handler.JobEvaluator { return stubEvaluator{} }
	svc.HalalCheckerFactory = func(string) handler.JobHalalChecker { return stubHalalChecker{} }
	svc.QuestionAnswererFactory = func(string) handler.JobQuestionAnswerer { return stubQuestionAnswerer{} }
	svc.FileToText = noopFileToText
	svc.FetchJobPage = func(_ context.Context, url string) (string, error) {
		return "Job description for " + url, nil
	}
	svc.Resource = resource

	return earlyValidationRouterParts{
		router:    handler.NewRouter(svc),
		renderer:  renderer,
		extractor: extractor,
		tailor:    tailor,
		resource:  resource,
	}
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestEarlyKeyValidation_Generate proves that /api/resume/generate rejects an overlong
// Idempotency-Key with HTTP 400 and makes zero LLM, renderer, and billing calls.
func TestEarlyKeyValidation_Generate(t *testing.T) {
	t.Parallel()
	p := newEarlyValidationRouter(t)
	token := registerAndLogin(t, p.router, "early-gen@example.com", "pw123456")
	saveResumeProfile(t, p.router, token, sampleResumeProfile())

	overlongKey := strings.Repeat("x", 129) // 1 character over the 128-char limit

	w := authPostMultipartWithIdempotencyKey(t, p.router, "/api/resume/generate", token, overlongKey, nil)

	require.Equal(t, http.StatusBadRequest, w.Code, "overlong key must be rejected: %s", w.Body)
	assert.Equal(t, int32(0), p.extractor.calls.Load(), "ExtractFromText must not be called")
	assert.Equal(t, int32(0), p.tailor.tailorCalls.Load(), "TailorProfile must not be called")
	assert.Equal(t, int32(0), p.renderer.resumeCalls.Load(), "RenderResume must not be called")
	assert.Equal(t, int32(0), p.resource.beforeN.Load(), "BeforeRender must not be called")
	assert.Equal(t, int32(0), p.resource.recordN.Load(), "RecordRender must not be called")
}

// TestEarlyKeyValidation_GenerateTailored proves that /api/resume/generate-tailored rejects
// an overlong Idempotency-Key before calling TailorProfile or any render/billing logic.
func TestEarlyKeyValidation_GenerateTailored(t *testing.T) {
	t.Parallel()
	p := newEarlyValidationRouter(t)
	token := registerAndLogin(t, p.router, "early-tailored@example.com", "pw123456")
	saveResumeProfile(t, p.router, token, sampleResumeProfile())

	overlongKey := strings.Repeat("y", 129)

	w := authPostMultipartWithIdempotencyKey(t, p.router, "/api/resume/generate-tailored", token, overlongKey,
		map[string]string{"job_description": "Looking for a great candidate.", "skip_url_fetch": "true"})

	require.Equal(t, http.StatusBadRequest, w.Code, "overlong key must be rejected: %s", w.Body)
	assert.Equal(t, int32(0), p.tailor.tailorCalls.Load(), "TailorProfile must not be called")
	assert.Equal(t, int32(0), p.renderer.resumeCalls.Load(), "RenderResume must not be called")
	assert.Equal(t, int32(0), p.resource.beforeN.Load(), "BeforeRender must not be called")
	assert.Equal(t, int32(0), p.resource.recordN.Load(), "RecordRender must not be called")
}

// TestEarlyKeyValidation_GenerateCoverLetter proves that /api/resume/generate-cover-letter
// rejects an overlong Idempotency-Key before calling WriteCoverLetter or any render/billing.
func TestEarlyKeyValidation_GenerateCoverLetter(t *testing.T) {
	t.Parallel()
	p := newEarlyValidationRouter(t)
	token := registerAndLogin(t, p.router, "early-cover@example.com", "pw123456")
	saveResumeProfile(t, p.router, token, sampleResumeProfile())

	overlongKey := strings.Repeat("z", 129)

	w := authPostMultipartWithIdempotencyKey(t, p.router, "/api/resume/generate-cover-letter", token, overlongKey,
		map[string]string{"skip_url_fetch": "true"})

	require.Equal(t, http.StatusBadRequest, w.Code, "overlong key must be rejected: %s", w.Body)
	assert.Equal(t, int32(0), p.tailor.coverCalls.Load(), "WriteCoverLetter must not be called")
	assert.Equal(t, int32(0), p.renderer.coverCalls.Load(), "RenderCoverLetter must not be called")
	assert.Equal(t, int32(0), p.resource.beforeN.Load(), "BeforeRender must not be called")
	assert.Equal(t, int32(0), p.resource.recordN.Load(), "RecordRender must not be called")
}

// TestEarlyKeyValidation_ExactLimitAccepted is a boundary test: a key of exactly
// maxIdempotencyKeyLen (128) characters must be accepted (not rejected).
func TestEarlyKeyValidation_ExactLimitAccepted(t *testing.T) {
	t.Parallel()
	p := newEarlyValidationRouter(t)
	token := registerAndLogin(t, p.router, "early-exact@example.com", "pw123456")
	saveResumeProfile(t, p.router, token, sampleResumeProfile())

	exactKey := strings.Repeat("a", 128) // exactly at the limit — must succeed

	w := authPostMultipartWithIdempotencyKey(t, p.router, "/api/resume/generate", token, exactKey, nil)
	assert.Equal(t, http.StatusOK, w.Code, "key of exactly 128 chars must be accepted: %s", w.Body)
	assert.Equal(t, "application/pdf", w.Header().Get("Content-Type"))

	// Billing must fire exactly once for this valid request.
	assert.Equal(t, int32(1), p.resource.beforeN.Load(), "BeforeRender must fire once for valid key")
	assert.Equal(t, int32(1), p.resource.recordN.Load(), "RecordRender must fire once for valid key")
}
