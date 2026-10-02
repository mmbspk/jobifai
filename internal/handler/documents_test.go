package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

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
