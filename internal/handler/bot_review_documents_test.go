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

func TestReviewDocuments_SiteSkipPutGetRoundTrip(t *testing.T) {
	svc, db := newTestServices(t)
	wireDocumentService(t, svc)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "siteskip@example.com", "password123")
	var userID string
	require.NoError(t, db.QueryRow(`SELECT id FROM users WHERE email = ?`, "siteskip@example.com").Scan(&userID))
	jobID := "job-site-skip"
	_, err := db.Exec(`INSERT INTO jobs_pending_review
		(job_id,user_id,company,role,platform,link,easy_apply,suitability_score,created_at)
		VALUES (?,?,?,?,?,?,1,9,datetime('now'))`,
		jobID, userID, "Co", "Role", "seek", "https://example.com/j")
	require.NoError(t, err)

	wPut := authPut(t, router, "/api/bot/review/"+jobID+"/documents", token, map[string]any{
		"resume_use_site": true,
		"cover_skip":      true,
	})
	require.Equal(t, http.StatusOK, wPut.Code, wPut.Body.String())

	wGet := authGet(t, router, "/api/bot/review/"+jobID+"/documents", token)
	require.Equal(t, http.StatusOK, wGet.Code, wGet.Body.String())
	var resp struct {
		Options struct {
			Resume []any `json:"resume"`
			Cover  []any `json:"cover"`
		} `json:"options"`
		Preflight struct {
			Selection struct {
				ResumeUseSite bool `json:"resume_use_site"`
				CoverSkip     bool `json:"cover_skip"`
			} `json:"selection"`
		} `json:"preflight"`
	}
	require.NoError(t, json.NewDecoder(wGet.Body).Decode(&resp))
	require.NotNil(t, resp.Options.Resume)
	require.NotNil(t, resp.Options.Cover)
	require.True(t, resp.Preflight.Selection.ResumeUseSite)
	require.True(t, resp.Preflight.Selection.CoverSkip)

	var refs string
	require.NoError(t, db.QueryRow(`SELECT document_refs_json FROM jobs_pending_review WHERE job_id = ?`, jobID).Scan(&refs))
	parsed := documents.ParseApplicationPackJSON(refs)
	require.True(t, parsed.Selection.ResumeUseSite)
	require.True(t, parsed.Selection.CoverSkip)
}

func TestReviewDocuments_CrossUserVersionRejected(t *testing.T) {
	svc, db := newTestServices(t)
	wireDocumentService(t, svc)
	router := handler.NewRouter(svc)
	tokenA := registerAndLogin(t, router, "owner@example.com", "password123")
	tokenB := registerAndLogin(t, router, "other@example.com", "password123")
	var userA string
	require.NoError(t, db.QueryRow(`SELECT id FROM users WHERE email = ?`, "owner@example.com").Scan(&userA))
	authPost(t, router, "/api/settings/resume", tokenA, map[string]any{
		"personal_information": map[string]string{"name": "Owner"},
	})
	wResume := authPost(t, router, "/api/documents/resume/from-profile", tokenA, map[string]string{"title": "Mine"})
	require.Equal(t, http.StatusCreated, wResume.Code)
	var body struct {
		ContentVersionID string `json:"content_version_id"`
	}
	require.NoError(t, json.NewDecoder(wResume.Body).Decode(&body))

	jobID := "job-cross-user"
	_, err := db.Exec(`INSERT INTO jobs_pending_review
		(job_id,user_id,company,role,platform,link,easy_apply,suitability_score,created_at)
		VALUES (?,?,?,?,?,?,1,9,datetime('now'))`,
		jobID, userA, "Co", "Role", "linkedin", "https://example.com/j")
	require.NoError(t, err)

	var userB string
	require.NoError(t, db.QueryRow(`SELECT id FROM users WHERE email = ?`, "other@example.com").Scan(&userB))
	_, err = db.Exec(`INSERT INTO jobs_pending_review
		(job_id,user_id,company,role,platform,link,easy_apply,suitability_score,created_at)
		VALUES (?,?,?,?,?,?,1,9,datetime('now'))`,
		"job-b", userB, "Co", "Role", "linkedin", "https://example.com/j2")
	require.NoError(t, err)

	w := authPut(t, router, "/api/bot/review/job-b/documents", tokenB, map[string]any{
		"resume_version_id": body.ContentVersionID,
	})
	assert.Equal(t, http.StatusNotFound, w.Code)
}
