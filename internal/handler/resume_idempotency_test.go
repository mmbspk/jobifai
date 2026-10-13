package handler_test

// HTTP-level idempotency regression tests for the legacy direct-render endpoints.
//
// These tests verify:
//   - The scoped key format (document-render:<userID>:<operation>:<clientKey>)
//   - Cross-endpoint isolation: same client key on different endpoints → independent burns
//   - Cross-user isolation: same client key for different users → independent burns
//   - Different client keys → independent burns
//   - Missing header → UUID-based key, backward-compatible (no header required)
//
// DB-level deduplication (idempotency key uniqueness constraint in resource_usage_events)
// is tested in internal/documents/render_billing_real_test.go.
// These tests focus solely on the scoping and routing correctness in the HTTP layer.

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

// recordingResourceStub records every RecordRender call made by the handler.
// burns maps idempotency key → call count; callsForKey(k) returns the count for key k.
// It does NOT deduplicate — that is the real resource.Service's job.
// Use distinctKeys() to get the set of unique keys recorded.
type recordingResourceStub struct {
	mu    sync.Mutex
	burns map[string]int // idempotencyKey → call count
}

func newRecordingResourceStub() *recordingResourceStub {
	return &recordingResourceStub{burns: make(map[string]int)}
}

func (s *recordingResourceStub) BeforeRender(_ context.Context, _ string) error { return nil }

func (s *recordingResourceStub) RecordRender(_ context.Context, _ string, idempotencyKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.burns[idempotencyKey]++
	return nil
}

func (s *recordingResourceStub) distinctKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.burns))
	for k := range s.burns {
		keys = append(keys, k)
	}
	return keys
}

func (s *recordingResourceStub) callsForKey(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.burns[key]
}

// Satisfy remaining ResourceService interface methods (not exercised by billing tests).
func (s *recordingResourceStub) LoadDefaults() (domain.ResourceMeteringDefaults, error) {
	return domain.ResourceMeteringDefaults{}, nil
}
func (s *recordingResourceStub) SaveDefaults(domain.ResourceMeteringDefaults) error { return nil }
func (s *recordingResourceStub) EstimateRenderCredits() (int64, error)              { return 3, nil }
func (s *recordingResourceStub) EstimateBrowserIncrementCredits() (int64, error)    { return 1, nil }

// newResumeRouterWithBillingStub builds a fully-wired resume router with a recording resource stub.
func newResumeRouterWithBillingStub(t *testing.T) (http.Handler, *handler.Services, *recordingResourceStub) {
	t.Helper()
	svc, _ := newTestServices(t)
	llm, eval, halal, answerer := stubLLMFactories()
	svc.Renderer = stubRenderer{}
	wireDocumentService(t, svc)
	svc.Documents.Renderer = svc.Renderer
	svc.LLMFactory = llm
	svc.EvaluatorFactory = eval
	svc.HalalCheckerFactory = halal
	svc.QuestionAnswererFactory = answerer
	svc.FileToText = noopFileToText
	svc.FetchJobPage = func(_ context.Context, url string) (string, error) {
		return "Job description for " + url, nil
	}
	stub := newRecordingResourceStub()
	svc.Resource = stub
	return handler.NewRouter(svc), svc, stub
}

// authPostMultipartWithIdempotencyKey is like authPostMultipart but also sets Idempotency-Key.
func authPostMultipartWithIdempotencyKey(t *testing.T, router http.Handler, path, token, idempotencyKey string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		require.NoError(t, w.WriteField(k, v))
	}
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestResume_Idempotency_SameEndpointSameKey_SameScopedKey proves that two requests
// from the same user to the same endpoint with the same Idempotency-Key header produce
// an identical scoped billing key.  A real resource.Service would deduplicate by this
// key at the DB layer, resulting in exactly one credit burn.
func TestResume_Idempotency_SameEndpointSameKey_SameScopedKey(t *testing.T) {
	t.Parallel()
	router, svc, stub := newResumeRouterWithBillingStub(t)
	token := registerAndLogin(t, router, "idempotency-same@example.com", "pw123456")
	saveResumeProfile(t, router, token, sampleResumeProfile())
	_ = svc

	const clientKey = "retry-safe-key-abc"

	w1 := authPostMultipartWithIdempotencyKey(t, router, "/api/resume/generate", token, clientKey, nil)
	require.Equal(t, http.StatusOK, w1.Code, "first request: %s", w1.Body)

	w2 := authPostMultipartWithIdempotencyKey(t, router, "/api/resume/generate", token, clientKey, nil)
	require.Equal(t, http.StatusOK, w2.Code, "second request: %s", w2.Body)

	keys := stub.distinctKeys()
	require.Len(t, keys, 1, "same user+endpoint+key must produce exactly one distinct scoped key")

	scopedKey := keys[0]
	assert.True(t, strings.HasPrefix(scopedKey, "document-render:"), "key must begin with document-render:")
	assert.Contains(t, scopedKey, ":resume-generate:", "key must encode the operation name")
	assert.True(t, strings.HasSuffix(scopedKey, ":"+clientKey), "key must end with the client-provided key")
	assert.Equal(t, 2, stub.callsForKey(scopedKey), "handler called RecordRender on each request; DB dedup collapses to one burn")
}

// TestResume_Idempotency_SameKeyDifferentEndpoints_IndependentBurns proves that the same
// client-supplied Idempotency-Key on resume-generate and cover-generate produces two
// distinct scoped keys, so those are independent billing operations.
func TestResume_Idempotency_SameKeyDifferentEndpoints_IndependentBurns(t *testing.T) {
	t.Parallel()
	router, _, stub := newResumeRouterWithBillingStub(t)
	token := registerAndLogin(t, router, "idempotency-endpoint-iso@example.com", "pw123456")
	saveResumeProfile(t, router, token, sampleResumeProfile())

	const clientKey = "same-client-key"

	wResume := authPostMultipartWithIdempotencyKey(t, router, "/api/resume/generate", token, clientKey, nil)
	require.Equal(t, http.StatusOK, wResume.Code, "resume generate: %s", wResume.Body)

	wCover := authPostMultipartWithIdempotencyKey(t, router, "/api/resume/generate-cover-letter", token, clientKey, map[string]string{
		"skip_url_fetch": "true",
	})
	require.Equal(t, http.StatusOK, wCover.Code, "cover generate: %s", wCover.Body)

	keys := stub.distinctKeys()
	require.Len(t, keys, 2, "resume-generate and cover-generate must produce independent scoped keys")

	var resumeKey, coverKey string
	for _, k := range keys {
		if strings.Contains(k, ":resume-generate:") {
			resumeKey = k
		} else if strings.Contains(k, ":cover-generate:") {
			coverKey = k
		}
	}
	require.NotEmpty(t, resumeKey, "must find a resume-generate key")
	require.NotEmpty(t, coverKey, "must find a cover-generate key")
	assert.NotEqual(t, resumeKey, coverKey, "keys must differ (operation-scoped)")
}

// TestResume_Idempotency_DifferentUsersSameKey_IndependentBurns proves that the same
// client-supplied Idempotency-Key from two different users produces distinct scoped
// keys, keeping each user's billing ledger independent.
func TestResume_Idempotency_DifferentUsersSameKey_IndependentBurns(t *testing.T) {
	t.Parallel()
	router, _, stub := newResumeRouterWithBillingStub(t)

	tokenA := registerAndLogin(t, router, "idempotency-userA@example.com", "pw123456")
	tokenB := registerAndLogin(t, router, "idempotency-userB@example.com", "pw123456")
	saveResumeProfile(t, router, tokenA, sampleResumeProfile())
	saveResumeProfile(t, router, tokenB, sampleResumeProfile())

	const clientKey = "shared-by-both-users"

	wA := authPostMultipartWithIdempotencyKey(t, router, "/api/resume/generate", tokenA, clientKey, nil)
	require.Equal(t, http.StatusOK, wA.Code, "user A: %s", wA.Body)

	wB := authPostMultipartWithIdempotencyKey(t, router, "/api/resume/generate", tokenB, clientKey, nil)
	require.Equal(t, http.StatusOK, wB.Code, "user B: %s", wB.Body)

	keys := stub.distinctKeys()
	require.Len(t, keys, 2, "different users must produce distinct scoped keys even with the same client key")

	for _, k := range keys {
		assert.True(t, strings.HasSuffix(k, ":"+clientKey), "key must embed the client key: %s", k)
	}
	assert.NotEqual(t, keys[0], keys[1], "keys must differ (userID-scoped)")
}

// TestResume_Idempotency_DifferentClientKeys_IndependentBurns proves that two distinct
// Idempotency-Key header values from the same user always produce two independent burns.
func TestResume_Idempotency_DifferentClientKeys_IndependentBurns(t *testing.T) {
	t.Parallel()
	router, _, stub := newResumeRouterWithBillingStub(t)
	token := registerAndLogin(t, router, "idempotency-two-keys@example.com", "pw123456")
	saveResumeProfile(t, router, token, sampleResumeProfile())

	w1 := authPostMultipartWithIdempotencyKey(t, router, "/api/resume/generate", token, "key-alpha", nil)
	require.Equal(t, http.StatusOK, w1.Code, "first key: %s", w1.Body)

	w2 := authPostMultipartWithIdempotencyKey(t, router, "/api/resume/generate", token, "key-beta", nil)
	require.Equal(t, http.StatusOK, w2.Code, "second key: %s", w2.Body)

	keys := stub.distinctKeys()
	assert.Len(t, keys, 2, "two different client keys must produce two distinct billing events")
}

// TestResume_Idempotency_NoHeader_BackwardCompatible proves that omitting the
// Idempotency-Key header is fully backward-compatible: the handler generates a UUID
// key automatically and the request succeeds normally.
func TestResume_Idempotency_NoHeader_BackwardCompatible(t *testing.T) {
	t.Parallel()
	router, _, stub := newResumeRouterWithBillingStub(t)
	token := registerAndLogin(t, router, "idempotency-no-header@example.com", "pw123456")
	saveResumeProfile(t, router, token, sampleResumeProfile())

	// Send without any Idempotency-Key header (empty string skips setting the header).
	w := authPostMultipartWithIdempotencyKey(t, router, "/api/resume/generate", token, "", nil)
	require.Equal(t, http.StatusOK, w.Code, "no header: %s", w.Body)
	assert.Equal(t, "application/pdf", w.Header().Get("Content-Type"))

	keys := stub.distinctKeys()
	require.Len(t, keys, 1, "must record exactly one billing event")
	scopedKey := keys[0]
	assert.True(t, strings.HasPrefix(scopedKey, "document-render:"), "auto-generated key must follow the scoped format")
	assert.Contains(t, scopedKey, ":resume-generate:", "auto-generated key must encode the operation")
}

// TestResume_Idempotency_OverlongKey_Rejected proves that an Idempotency-Key header value
// longer than 128 characters is rejected with HTTP 400 rather than silently truncated.
// Silent truncation would allow two keys that share the same first 128 chars but differ
// afterward to collide on the same billing operation — a false-dedup security bug.
func TestResume_Idempotency_OverlongKey_Rejected(t *testing.T) {
	t.Parallel()
	router, _, stub := newResumeRouterWithBillingStub(t)
	token := registerAndLogin(t, router, "idempotency-overlong@example.com", "pw123456")
	saveResumeProfile(t, router, token, sampleResumeProfile())

	// Two keys that share the same first 128 chars but differ afterward.
	base := strings.Repeat("x", 128)
	keyA := base + "-alpha"
	keyB := base + "-beta"

	wA := authPostMultipartWithIdempotencyKey(t, router, "/api/resume/generate", token, keyA, nil)
	assert.Equal(t, http.StatusBadRequest, wA.Code, "overlong key A must be rejected: %s", wA.Body)

	wB := authPostMultipartWithIdempotencyKey(t, router, "/api/resume/generate", token, keyB, nil)
	assert.Equal(t, http.StatusBadRequest, wB.Code, "overlong key B must be rejected: %s", wB.Body)

	// Neither request must have triggered a billing call.
	assert.Empty(t, stub.distinctKeys(), "rejected requests must not produce any billing events")
}
