package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

func TestReviewPutDocuments_InvalidatesPrepareAndValidatesKind(t *testing.T) {
	svc, db := newTestServices(t)
	wireDocumentService(t, svc)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "reviewdocs@example.com", "password123")
	var userID string
	require.NoError(t, db.QueryRow(`SELECT id FROM users WHERE email = ?`, "reviewdocs@example.com").Scan(&userID))

	authPost(t, router, "/api/settings/resume", token, map[string]any{
		"personal_information": map[string]string{"name": "Alex"},
	})
	wResume := authPost(t, router, "/api/documents/resume/from-profile", token, map[string]string{"title": "Default"})
	require.Equal(t, http.StatusCreated, wResume.Code, wResume.Body.String())
	var resumeBody struct {
		ContentVersionID string `json:"content_version_id"`
	}
	require.NoError(t, json.NewDecoder(wResume.Body).Decode(&resumeBody))
	wCover := authPost(t, router, "/api/documents/cover-letter", token, map[string]any{
		"title": "General", "body": "Dear hiring team,\n",
	})
	require.Equal(t, http.StatusCreated, wCover.Code, wCover.Body.String())
	var coverBody struct {
		ContentVersionID string `json:"content_version_id"`
	}
	require.NoError(t, json.NewDecoder(wCover.Body).Decode(&coverBody))

	jobID := "job-review-docs-1"
	pack := documents.WriteApplicationPackJSON(documents.ApplicationDocumentPack{
		Prepared: true,
		Resume:   domain.ApplicationDocumentRef{Kind: documents.KindResume, Outcome: domain.DocumentOutcomeLocalFile},
		Cover:    domain.ApplicationDocumentRef{Kind: documents.KindCoverLetter, Outcome: domain.DocumentOutcomeSkipped},
	})
	_, err := db.Exec(`INSERT INTO jobs_pending_review
		(job_id,user_id,company,role,platform,link,easy_apply,resume_path,cover_letter_path,
		 resume_content_version_id,cover_letter_content_version_id,document_refs_json,suitability_score,created_at)
		VALUES (?,?,?,?,?,?,1,'/tmp/r.pdf','/tmp/c.pdf',?,?,?,80,datetime('now'))`,
		jobID, userID, "Co", "Role", "linkedin", "https://example.com/j", resumeBody.ContentVersionID, coverBody.ContentVersionID, pack)
	require.NoError(t, err)

	wBad := authPut(t, router, "/api/bot/review/"+jobID+"/documents", token, map[string]any{
		"resume_version_id": coverBody.ContentVersionID,
	})
	assert.Equal(t, http.StatusBadRequest, wBad.Code)

	wOK := authPut(t, router, "/api/bot/review/"+jobID+"/documents", token, map[string]any{
		"resume_version_id": resumeBody.ContentVersionID,
		"cover_skip":        true,
	})
	require.Equal(t, http.StatusOK, wOK.Code, wOK.Body.String())

	var resumePath, coverPath, refs string
	err = db.QueryRow(`SELECT COALESCE(resume_path,''), COALESCE(cover_letter_path,''), COALESCE(document_refs_json,'')
		FROM jobs_pending_review WHERE job_id = ?`, jobID).Scan(&resumePath, &coverPath, &refs)
	require.NoError(t, err)
	assert.Empty(t, resumePath)
	assert.Empty(t, coverPath)
	parsed := documents.ParseApplicationPackJSON(refs)
	assert.False(t, parsed.Prepared)
	assert.Contains(t, parsed.HoldReason, "prepare again")
}
