package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func TestDocuments_DeleteUpdatesListDefaultsAndPreservesApplicationDownload(t *testing.T) {
	router, svc, token := newResumeRouter(t)
	saveResumeProfile(t, router, token, sampleResumeProfile())
	generated := authPostMultipart(t, router, "/api/resume/generate", token, nil)
	require.Equal(t, http.StatusOK, generated.Code)
	versionID := generated.Header().Get("X-Jobifai-Document-Version")
	detailResp := authGet(t, router, "/api/documents/versions/"+versionID, token)
	var detail documents.VersionDetail
	require.NoError(t, json.Unmarshal(detailResp.Body.Bytes(), &detail))
	userID := userIDFromToken(t, router, token)
	_, err := svc.DB.Exec(`INSERT INTO jobs_applied (id,user_id,platform,company,role,link,resume_content_version_id) VALUES ('delete-history',?,'linkedin','Acme','Lead','https://example.com/job',?)`, userID, versionID)
	require.NoError(t, err)
	defaultResp := authPut(t, router, "/api/documents/defaults", token, map[string]string{"kind": "resume", "content_version_id": versionID})
	require.Equal(t, http.StatusOK, defaultResp.Code)
	list := func() documents.ListResponse {
		w := authGet(t, router, "/api/documents", token)
		require.Equal(t, http.StatusOK, w.Code)
		var out documents.ListResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	require.Len(t, list().Documents, 1)
	require.Equal(t, versionID, list().Defaults.ResumeVersionID)
	for i := 0; i < 2; i++ {
		deleted := authDelete(t, router, "/api/documents/"+detail.DocumentID, token)
		require.Equal(t, http.StatusOK, deleted.Code, deleted.Body.String())
	}
	remaining := list()
	require.Empty(t, remaining.Documents)
	require.Empty(t, remaining.Defaults.ResumeVersionID)
	require.False(t, remaining.Defaults.ResumeOutdated)
	history := authGet(t, router, "/api/jobs/applied/delete-history/pdf/resume", token)
	require.Equal(t, http.StatusOK, history.Code, history.Body.String())
	require.Equal(t, generated.Body.Bytes(), history.Body.Bytes())
	// Removed versions are still readable for history, but cannot be chosen again.
	defaultResp = authPut(t, router, "/api/documents/defaults", token, map[string]string{"kind": "resume", "content_version_id": versionID})
	require.NotEqual(t, http.StatusOK, defaultResp.Code)
	appendResp := authPost(t, router, "/api/documents/"+detail.DocumentID+"/resume-versions", token, map[string]any{"profile": sampleResumeProfile()})
	require.NotEqual(t, http.StatusCreated, appendResp.Code)
	var active int
	require.NoError(t, svc.DB.QueryRow(`SELECT COUNT(*) FROM user_documents WHERE user_id=? AND deleted_at IS NULL`, userID).Scan(&active))
	require.Zero(t, active)
}

func TestDocuments_DeleteRejectsOtherUsersAndMissingDocuments(t *testing.T) {
	router, _, owner := newResumeRouter(t)
	saveResumeProfile(t, router, owner, sampleResumeProfile())
	generated := authPostMultipart(t, router, "/api/resume/generate", owner, nil)
	versionID := generated.Header().Get("X-Jobifai-Document-Version")
	detailResp := authGet(t, router, "/api/documents/versions/"+versionID, owner)
	var detail documents.VersionDetail
	require.NoError(t, json.Unmarshal(detailResp.Body.Bytes(), &detail))
	other := registerAndLogin(t, router, "other-delete@example.com", "password123")
	for _, id := range []string{detail.DocumentID, "missing-document"} {
		w := authDelete(t, router, "/api/documents/"+id, other)
		require.Equal(t, http.StatusNotFound, w.Code)
	}
	unauthenticated := authDelete(t, router, "/api/documents/"+detail.DocumentID, "")
	require.Equal(t, http.StatusUnauthorized, unauthenticated.Code)
	w := authGet(t, router, "/api/documents", owner)
	require.Contains(t, w.Body.String(), detail.DocumentID)
}
