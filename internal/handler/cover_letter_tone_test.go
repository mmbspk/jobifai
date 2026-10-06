package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

// capturingTailor records the last tone argument passed to WriteCoverLetter.
type capturingTailor struct {
	mu   sync.Mutex
	tone string
}

func (c *capturingTailor) TailorProfile(_ context.Context, p *domain.ResumeProfile, _ string) (*domain.ResumeProfile, error) {
	return p, nil
}

func (c *capturingTailor) WriteCoverLetter(_ context.Context, _ *domain.ResumeProfile, _, tone string) (string, error) {
	c.mu.Lock()
	c.tone = tone
	c.mu.Unlock()
	return "Dear hiring team,\n", nil
}

func (c *capturingTailor) lastTone() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tone
}

// errConfigStore wraps a real ConfigStore and returns an error for chosen keys.
type errConfigStore struct {
	handler.ConfigStore
	mu      sync.Mutex
	failKey string // when non-empty, Get returns an error for this key
}

func (e *errConfigStore) failOn(key string) {
	e.mu.Lock()
	e.failKey = key
	e.mu.Unlock()
}

func (e *errConfigStore) Get(userID, key string, dst any) error {
	e.mu.Lock()
	fk := e.failKey
	e.mu.Unlock()
	if fk != "" && key == fk {
		return fmt.Errorf("simulated DB read failure")
	}
	return e.ConfigStore.Get(userID, key, dst)
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestGeneralSettings_CoverLetterTone_RoundTrip(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "tone-rt@example.com", "password123")

	w := authPost(t, router, "/api/settings/general", token, map[string]any{
		"cover_letter_tone": "formal",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = authGet(t, router, "/api/settings/general", token)
	require.Equal(t, http.StatusOK, w.Code)
	var gs domain.GeneralSettings
	require.NoError(t, json.NewDecoder(w.Body).Decode(&gs))
	assert.Equal(t, "formal", gs.CoverLetterTone)
}

func TestResume_GenerateCoverLetter_TonePropagation(t *testing.T) {
	svc, _ := newTestServices(t)
	ct := &capturingTailor{}
	svc.Renderer = stubRenderer{}
	svc.LLMFactory = func(string) (handler.ResumeExtractor, handler.ResumeTailor) {
		return stubExtractor{}, ct
	}
	svc.FetchJobPage = func(_ context.Context, url string) (string, error) {
		return "job posting for " + url, nil
	}
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "tone-prop@example.com", "password123")
	saveResumeProfile(t, router, token, sampleResumeProfile())

	// Set tone in general settings.
	w := authPost(t, router, "/api/settings/general", token, map[string]any{
		"cover_letter_tone": "conversational",
	})
	require.Equal(t, http.StatusOK, w.Code)

	// Generate cover letter; handler should forward the tone to WriteCoverLetter.
	w = authPostMultipart(t, router, "/api/resume/generate-cover-letter", token, map[string]string{
		"job_description": "We are hiring an operations coordinator.",
		"skip_url_fetch":  "true",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "conversational", ct.lastTone())
}

func TestDocuments_AIGenerateCoverLetter_TonePropagation(t *testing.T) {
	svc, _ := newTestServices(t)
	wireDocumentService(t, svc)
	ct := &capturingTailor{}
	svc.LLMFactory = func(string) (handler.ResumeExtractor, handler.ResumeTailor) {
		return stubExtractor{}, ct
	}
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "doc-tone@example.com", "password123")
	saveResumeProfile(t, router, token, sampleResumeProfile())

	w := authPost(t, router, "/api/settings/general", token, map[string]any{
		"cover_letter_tone": "confident",
	})
	require.Equal(t, http.StatusOK, w.Code)

	w = authPost(t, router, "/api/documents/cover-letter/ai-generate", token, map[string]any{
		"title": "General cover letter",
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Equal(t, "confident", ct.lastTone())
}

func TestResume_GenerateCoverLetter_SettingsReadError_Returns500(t *testing.T) {
	svc, _ := newTestServices(t)
	ct := &capturingTailor{}
	svc.Renderer = stubRenderer{}
	errCfg := &errConfigStore{ConfigStore: svc.Config}
	svc.Config = errCfg
	svc.LLMFactory = func(string) (handler.ResumeExtractor, handler.ResumeTailor) {
		return stubExtractor{}, ct
	}
	svc.FetchJobPage = func(_ context.Context, url string) (string, error) {
		return "job text", nil
	}
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "tone-err@example.com", "password123")
	saveResumeProfile(t, router, token, sampleResumeProfile())

	// Inject a DB failure for general_settings reads.
	errCfg.failOn("general_settings")

	w := authPostMultipart(t, router, "/api/resume/generate-cover-letter", token, map[string]string{
		"job_description": "Operations coordinator role.",
		"skip_url_fetch":  "true",
	})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Empty(t, ct.lastTone(), "WriteCoverLetter must not be called when settings read fails")
}

func TestDocuments_AIGenerateCoverLetter_SettingsReadError_Returns500(t *testing.T) {
	svc, _ := newTestServices(t)
	ct := &capturingTailor{}
	errCfg := &errConfigStore{ConfigStore: svc.Config}
	svc.Config = errCfg
	svc.LLMFactory = func(string) (handler.ResumeExtractor, handler.ResumeTailor) {
		return stubExtractor{}, ct
	}
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "doc-err@example.com", "password123")
	saveResumeProfile(t, router, token, sampleResumeProfile())

	errCfg.failOn("general_settings")

	w := authPost(t, router, "/api/documents/cover-letter/ai-generate", token, map[string]any{
		"title": "General cover letter",
	})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Empty(t, ct.lastTone(), "WriteCoverLetter must not be called when settings read fails")
}
