package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/resume"
)


type stubPDFRenderer struct{}

func (stubPDFRenderer) RenderResume(_ context.Context, _ *domain.ResumeProfile, _, _ string, _ *resume.RenderOptions) ([]byte, error) {
	return []byte("%PDF-stub"), nil
}
func (stubPDFRenderer) RenderCoverLetter(_ context.Context, _ string, _, _ string) ([]byte, error) {
	return []byte("%PDF-stub"), nil
}

func wireDocumentService(t *testing.T, svc *handler.Services) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "doc-blobs")
	blobs, err := documents.NewLocalBlobStore(root)
	require.NoError(t, err)
	r := stubPDFRenderer{}
	svc.Documents = &documents.Service{
		Store:     documents.NewStore(svc.DB),
		Blobs:     blobs,
		Renderer:  r,
		MarketDir: "resume_markets",
		StylesDir: filepath.Join("..", "..", documents.StylesDirRelative),
		LoadProfile: func(userID string) (*domain.ResumeProfile, error) {
			var p domain.ResumeProfile
			if err := svc.Config.Get(userID, "resume_profile", &p); errors.Is(err, domain.ErrNotFound) {
				return nil, nil
			} else if err != nil {
				return nil, err
			}
			return &p, nil
		},
		SaveDefaultsMeta: func(userID string, m documents.DefaultsMeta) error {
			return svc.Config.Set(userID, "document_defaults_meta", m)
		},
		DefaultsMeta: func(userID string) (documents.DefaultsMeta, error) {
			var m documents.DefaultsMeta
			if err := svc.Config.Get(userID, "document_defaults_meta", &m); err != nil {
				return documents.DefaultsMeta{}, nil
			}
			return m, nil
		},
	}
}

func multipartOriginal(t *testing.T, filename string, payload []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(payload)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return &body, w.FormDataContentType()
}

func TestDocuments_ListEmptyArray(t *testing.T) {
	t.Parallel()
	svc, _ := newTestServices(t)
	wireDocumentService(t, svc)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "empty-docs@example.com", "password123")
	w := authGet(t, router, "/api/documents", token)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"documents":[]`)
}

func TestDocuments_CreateFromProfileAndSetDefault(t *testing.T) {
	t.Parallel()
	svc, _ := newTestServices(t)
	wireDocumentService(t, svc)
	router := handler.NewRouter(svc)

	token := registerAndLogin(t, router, "docs@example.com", "password123")
	authPost(t, router, "/api/settings/resume", token, map[string]any{
		"personal_information": map[string]string{
			"name":  "Sam Candidate",
			"email": "sam@example.com",
		},
	})

	w := authPost(t, router, "/api/documents/resume/from-profile", token, map[string]string{
		"title": "Profile resume",
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created struct {
		ContentVersionID string `json:"content_version_id"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))
	require.NotEmpty(t, created.ContentVersionID)

	w = authPut(t, router, "/api/documents/defaults", token, map[string]string{
		"kind":               "resume",
		"content_version_id": created.ContentVersionID,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = authGet(t, router, "/api/documents/", token)
	require.Equal(t, http.StatusOK, w.Code)
	var list documents.ListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.Equal(t, created.ContentVersionID, list.Defaults.ResumeVersionID)
}

func TestDocuments_ProfileSaveMarksDefaultsOutdated(t *testing.T) {
	t.Parallel()
	svc, _ := newTestServices(t)
	wireDocumentService(t, svc)
	router := handler.NewRouter(svc)

	token := registerAndLogin(t, router, "outdated@example.com", "password123")
	authPost(t, router, "/api/settings/resume", token, map[string]any{
		"personal_information": map[string]string{"name": "First", "email": "a@example.com"},
	})
	w := authPost(t, router, "/api/documents/resume/from-profile", token, map[string]string{})
	require.Equal(t, http.StatusCreated, w.Code)
	var created struct {
		ContentVersionID string `json:"content_version_id"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))
	authPut(t, router, "/api/documents/defaults", token, map[string]string{
		"kind": "resume", "content_version_id": created.ContentVersionID,
	})

	authPost(t, router, "/api/settings/resume", token, map[string]any{
		"personal_information": map[string]string{"name": "Updated", "email": "a@example.com"},
	})

	w = authGet(t, router, "/api/documents/", token)
	var list documents.ListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.True(t, list.Defaults.ResumeOutdated)
	require.Contains(t, list.Defaults.OutdatedReason, "profile")
}

func TestDocuments_SetDefaultOriginalUploadResume(t *testing.T) {
	t.Parallel()
	svc, _ := newTestServices(t)
	wireDocumentService(t, svc)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "orig-default@example.com", "password123")

	body, contentType := multipartOriginal(t, "resume.pdf", []byte("original-pdf-bytes"))
	req := httptest.NewRequest(http.MethodPost, "/api/documents/originals", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created struct {
		ContentVersionID string `json:"content_version_id"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))

	w = authPut(t, router, "/api/documents/defaults", token, map[string]string{
		"kind": "resume", "content_version_id": created.ContentVersionID,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = authGet(t, router, "/api/documents", token)
	require.Equal(t, http.StatusOK, w.Code)
}

// failingPDFRenderer returns an error from RenderResume to simulate Chrome being unavailable.
type failingPDFRenderer struct{}

func (failingPDFRenderer) RenderResume(_ context.Context, _ *domain.ResumeProfile, _, _ string, _ *resume.RenderOptions) ([]byte, error) {
	return nil, errors.New("renderer unavailable: Chrome not found")
}
func (failingPDFRenderer) RenderCoverLetter(_ context.Context, _ string, _, _ string) ([]byte, error) {
	return nil, errors.New("renderer unavailable: Chrome not found")
}

// wireDocumentServiceWithRenderer builds a test document service with the given renderer.
func wireDocumentServiceWithRenderer(t *testing.T, svc *handler.Services, r documents.PDFRenderer) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "doc-blobs")
	blobs, err := documents.NewLocalBlobStore(root)
	require.NoError(t, err)
	svc.Documents = &documents.Service{
		Store:     documents.NewStore(svc.DB),
		Blobs:     blobs,
		Renderer:  r,
		MarketDir: "resume_markets",
		StylesDir: filepath.Join("..", "..", documents.StylesDirRelative),
		LoadProfile: func(userID string) (*domain.ResumeProfile, error) {
			var p domain.ResumeProfile
			if err := svc.Config.Get(userID, "resume_profile", &p); errors.Is(err, domain.ErrNotFound) {
				return nil, nil
			} else if err != nil {
				return nil, err
			}
			return &p, nil
		},
		SaveDefaultsMeta: func(userID string, m documents.DefaultsMeta) error {
			return svc.Config.Set(userID, "document_defaults_meta", m)
		},
		DefaultsMeta: func(userID string) (documents.DefaultsMeta, error) {
			var m documents.DefaultsMeta
			if err := svc.Config.Get(userID, "document_defaults_meta", &m); err != nil {
				return documents.DefaultsMeta{}, nil
			}
			return m, nil
		},
	}
}

// TestCreateResumeFromProfile_RenderFails_Returns201 is the regression test for the E2E
// failure diagnosed in feat/resource-credit-document-rendering: when the PDF renderer is
// unavailable (Chrome not found in CI), /api/documents/resume/from-profile must still
// return HTTP 201 with a content_version_id and render_status="failed".
// The version is preserved (content survives; no PDF artifact) because no billing was
// committed. The caller may set it as the default even without a PDF.
func TestCreateResumeFromProfile_RenderFails_Returns201(t *testing.T) {
	t.Parallel()
	svc, _ := newTestServices(t)
	wireDocumentServiceWithRenderer(t, svc, failingPDFRenderer{})
	router := handler.NewRouter(svc)

	token := registerAndLogin(t, router, "render-fail@example.com", "password123")
	authPost(t, router, "/api/settings/resume", token, map[string]any{
		"personal_information": map[string]string{"name": "Alex Test", "email": "alex@example.com"},
	})

	w := authPost(t, router, "/api/documents/resume/from-profile", token, map[string]string{
		"title": "CI render-fail test",
	})
	assert.Equal(t, http.StatusCreated, w.Code,
		"render failure must return 201 (partial success), not 422 — body: %s", w.Body)

	var resp struct {
		ContentVersionID string `json:"content_version_id"`
		RenderStatus     string `json:"render_status"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotEmpty(t, resp.ContentVersionID, "must include a content_version_id")
	assert.Equal(t, "failed", resp.RenderStatus, "render_status must be 'failed'")
}
