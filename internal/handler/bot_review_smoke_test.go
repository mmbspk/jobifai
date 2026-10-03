package handler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

type submitCaptureBot struct {
	recordingBot
	lastSubmit bot.SubmitRequest
}

func (b *submitCaptureBot) SubmitSync(_ context.Context, _ string, req bot.SubmitRequest) error {
	b.lastSubmit = req
	return nil
}

// simulatePrepareFixture persists the pack shape PrepareReviewDocuments writes after a successful scan.
func simulatePrepareFixture(
	t *testing.T, db *sql.DB, jobID, userID, resumeVersionID, resumePath, coverPath string,
	sel documents.ApplicationDocumentSelection,
) {
	t.Helper()
	res := documents.ResolveResult{
		Resume: documents.ResolvedDocument{
			Kind: documents.KindResume, Outcome: domain.DocumentOutcomeLocalFile,
			VersionID: resumeVersionID, NeedDefaultUpload: true,
		},
		Cover: documents.ResolvedDocument{
			Kind: documents.KindCoverLetter, Outcome: domain.DocumentOutcomeSkipped, Skip: true,
		},
	}
	pack := documents.PackFromPrepared("", res.Resume, res.Cover, resumePath, coverPath)
	pack.MarkPrepared()
	pack.Selection = sel
	pack.FormCaps = documents.FormDocumentCapabilities{Detected: true, ResumeFileSlots: 1, CoverOptional: true}
	require.True(t, pack.ReadyForSubmit())
	packJSON := documents.WriteApplicationPackJSON(pack)
	_, err := db.Exec(`UPDATE jobs_pending_review
		SET resume_path = ?, cover_letter_path = ?,
		    resume_content_version_id = ?, cover_letter_content_version_id = ?,
		    document_refs_json = ?
		WHERE job_id = ? AND user_id = ?`,
		resumePath, coverPath, resumeVersionID, "", packJSON, jobID, userID)
	require.NoError(t, err)
}

func TestReviewSmoke_SelectionChangePrepareApproveFixture(t *testing.T) {
	svc, db := newTestServices(t)
	wireDocumentService(t, svc)
	cap := &submitCaptureBot{}
	svc.Bot = cap
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "smoke-review@example.com", "password123")
	var userID string
	require.NoError(t, db.QueryRow(`SELECT id FROM users WHERE email = ?`, "smoke-review@example.com").Scan(&userID))

	authPost(t, router, "/api/settings/resume", token, map[string]any{
		"personal_information": map[string]string{"name": "Alex"},
	})
	wResume := authPost(t, router, "/api/documents/resume/from-profile", token, map[string]string{"title": "Default"})
	require.Equal(t, http.StatusCreated, wResume.Code)
	var resumeBody struct {
		ContentVersionID string `json:"content_version_id"`
	}
	require.NoError(t, json.NewDecoder(wResume.Body).Decode(&resumeBody))

	jobID := "job-smoke-review"
	_, err := db.Exec(`INSERT INTO jobs_pending_review
		(job_id,user_id,company,role,platform,link,easy_apply,suitability_score,created_at)
		VALUES (?,?,?,?,?,?,1,9,datetime('now'))`,
		jobID, userID, "Fixture Co", "Coordinator", "seek", "https://example.com/jobs/smoke")
	require.NoError(t, err)

	sel := documents.ApplicationDocumentSelection{
		ResumeVersionID: resumeBody.ContentVersionID,
		CoverSkip:       true,
	}
	wPut := authPut(t, router, "/api/bot/review/"+jobID+"/documents", token, map[string]any{
		"resume_version_id": sel.ResumeVersionID,
		"cover_skip":        true,
	})
	require.Equal(t, http.StatusOK, wPut.Code)

	wGet1 := authGet(t, router, "/api/bot/review/"+jobID+"/documents", token)
	require.Equal(t, http.StatusOK, wGet1.Code)
	var pre1 struct {
		Preflight struct {
			Prepared bool   `json:"prepared"`
			Selection struct {
				ResumeVersionID string `json:"resume_version_id"`
				CoverSkip       bool   `json:"cover_skip"`
			} `json:"selection"`
			PackHoldReason string `json:"pack_hold_reason"`
		} `json:"preflight"`
	}
	require.NoError(t, json.NewDecoder(wGet1.Body).Decode(&pre1))
	require.False(t, pre1.Preflight.Prepared)
	require.Equal(t, sel.ResumeVersionID, pre1.Preflight.Selection.ResumeVersionID)
	require.True(t, pre1.Preflight.Selection.CoverSkip)
	require.Contains(t, pre1.Preflight.PackHoldReason, "prepare again")

	resumePath := "job_applications/" + userID + "/" + jobID + "/resume.pdf"
	simulatePrepareFixture(t, db, jobID, userID, resumeBody.ContentVersionID, resumePath, "", sel)

	wGet2 := authGet(t, router, "/api/bot/review/"+jobID+"/documents", token)
	require.Equal(t, http.StatusOK, wGet2.Code)
	var pre2 struct {
		Preflight struct {
			Prepared bool `json:"prepared"`
			Resume   struct {
				Action    string `json:"action"`
				VersionID string `json:"version_id"`
			} `json:"resume"`
			Cover struct {
				Action string `json:"action"`
			} `json:"cover"`
		} `json:"preflight"`
	}
	require.NoError(t, json.NewDecoder(wGet2.Body).Decode(&pre2))
	require.True(t, pre2.Preflight.Prepared)
	require.Equal(t, resumeBody.ContentVersionID, pre2.Preflight.Resume.VersionID)

	wPending := authGet(t, router, "/api/bot/review/pending", token)
	require.Equal(t, http.StatusOK, wPending.Code)
	var pending []domain.PendingReview
	require.NoError(t, json.NewDecoder(wPending.Body).Decode(&pending))
	require.Len(t, pending, 1)
	require.Equal(t, resumePath, pending[0].ResumePath)
	require.Empty(t, pending[0].CoverLetterPath)

	wApprove := authPost(t, router, "/api/bot/review/"+jobID+"/approve", token, nil)
	require.Equal(t, http.StatusOK, wApprove.Code, wApprove.Body.String())
	require.True(t, cap.lastSubmit.FrozenDocuments)
	require.Equal(t, resumeBody.ContentVersionID, cap.lastSubmit.ResumeContentVersionID)
	require.Equal(t, resumePath, cap.lastSubmit.ResumePath)

	// Changing selection after prepare must block approve until prepare runs again.
	_, err = db.Exec(`INSERT INTO jobs_pending_review
		(job_id,user_id,company,role,platform,link,easy_apply,resume_path,cover_letter_path,
		 resume_content_version_id,document_refs_json,suitability_score,created_at)
		VALUES (?,?,?,?,?,?,1,?,?,?,?,9,datetime('now'))`,
		jobID+"-2", userID, "Fixture Co", "Coordinator", "seek", "https://example.com/j2",
		resumePath, "", resumeBody.ContentVersionID, documents.WriteApplicationPackJSON(documents.PackFromPrepared("", documents.ResolvedDocument{
			Kind: documents.KindResume, Outcome: domain.DocumentOutcomeLocalFile, VersionID: resumeBody.ContentVersionID,
		}, documents.ResolvedDocument{Kind: documents.KindCoverLetter, Outcome: domain.DocumentOutcomeSkipped, Skip: true},
			resumePath, "")),
	)
	require.NoError(t, err)
	simulatePrepareFixture(t, db, jobID+"-2", userID, resumeBody.ContentVersionID, resumePath, "", sel)

	wPut2 := authPut(t, router, "/api/bot/review/"+jobID+"-2/documents", token, map[string]any{
		"resume_use_site": true,
		"cover_skip":      true,
	})
	require.Equal(t, http.StatusOK, wPut2.Code)
	wApproveBlocked := authPost(t, router, "/api/bot/review/"+jobID+"-2/approve", token, nil)
	assert.Equal(t, http.StatusConflict, wApproveBlocked.Code)
}
