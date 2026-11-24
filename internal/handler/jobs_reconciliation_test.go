package handler_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/handler"
)

const reconciliationJobID = "reviewed-job"
const reconciliationLink = "https://www.linkedin.com/jobs/view/reviewed-job"
const reconciliationUser = "candidate"
const reconciliationHalal = `{"verdict":"HALAL","confidence":"HIGH","summary":"Reviewed"}`
const reconciliationPack = `{"resume":{"content_version_id":"resume-version"},"hold_reason":"Submission unconfirmed"}`

func seedReconciliationReview(t *testing.T, db *sql.DB, cannotApply bool, link string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO jobs_pending_review
 (job_id,user_id,company,role,location,platform,link,suitability_score,halal_verdict,
 resume_path,cover_letter_path,resume_content_version_id,cover_letter_content_version_id,document_refs_json)
 VALUES(?,?,'Company','Coordinator','Adelaide','linkedin',?,8,?,'resume.pdf','cover.pdf','resume-version','cover-version',?)`,
		reconciliationJobID, reconciliationUser, link, reconciliationHalal, reconciliationPack)
	require.NoError(t, err)
	if cannotApply {
		_, err = db.Exec(`INSERT INTO jobs_skipped(id,user_id,company,role,location,platform,link,suitability_score,halal_verdict,skip_reason)
  VALUES(?,?,'Company','Coordinator','Adelaide','linkedin',?,8,?,'easy apply: submission could not be confirmed')`,
			reconciliationJobID, reconciliationUser, link, reconciliationHalal)
		require.NoError(t, err)
	}
}

func markReconciliationApplied(t *testing.T, svc *handler.Services, cannotApply bool, user string) *httptest.ResponseRecorder {
	t.Helper()
	router := chi.NewRouter()
	jobs := handler.NewJobHandlers(svc)
	router.Post("/pending-review/{job_id}/mark-applied", jobs.MarkApplied)
	router.Post("/cannot-apply/{job_id}/mark-applied", jobs.MarkAppliedFromCannotApply)
	path := "/pending-review/" + reconciliationJobID + "/mark-applied"
	if cannotApply {
		path = "/cannot-apply/" + reconciliationJobID + "/mark-applied"
	}
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req = req.WithContext(auth.TestContext(req.Context(), user))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

type reconciliationAttempt struct {
	name, user, platform, jobID, link, outcome string
	confirmed                                  bool
}

func seedReconciliationAttempt(t *testing.T, db *sql.DB, attempt reconciliationAttempt) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO apply_attempts(id,user_id,platform,job_id,url,outcome,stage,reason_code,reason_text,finished_at,elapsed_ms)
 VALUES(?,?,?,?,?,?,'submit_dispatch','confirmation_missing','Original diagnostic','2026-11-23T08:00:00Z',1200)`,
		attempt.name, attempt.user, attempt.platform, attempt.jobID, attempt.link, attempt.outcome)
	require.NoError(t, err)
}

func TestMarkApplied_ReconcilesOnlyOwnedUncertainSubmissionAndPreservesMetadata(t *testing.T) {
	for _, cannotApply := range []bool{false, true} {
		name := "pending review"
		if cannotApply {
			name = "cannot apply"
		}
		t.Run(name, func(t *testing.T) {
			svc, db := newTestServices(t)
			seedReconciliationReview(t, db, cannotApply, reconciliationLink)
			attempts := []reconciliationAttempt{
				{"target", reconciliationUser, "linkedin", reconciliationJobID, reconciliationLink, "submission_unknown", true},
				{"exact link alias", reconciliationUser, "linkedin", "manual-alias", reconciliationLink, "submission_unknown", true},
				{"other user", "other-candidate", "linkedin", reconciliationJobID, reconciliationLink, "submission_unknown", false},
				{"other platform", reconciliationUser, "seek", reconciliationJobID, reconciliationLink, "submission_unknown", false},
				{"other job", reconciliationUser, "linkedin", "different-job", "https://www.linkedin.com/jobs/view/different-job", "submission_unknown", false},
				{"active", reconciliationUser, "linkedin", reconciliationJobID, reconciliationLink, "in_progress", false},
				{"already confirmed", reconciliationUser, "linkedin", reconciliationJobID, reconciliationLink, "submitted_confirmed", false},
				{"failed", reconciliationUser, "linkedin", reconciliationJobID, reconciliationLink, "failed", false},
			}
			for _, attempt := range attempts {
				seedReconciliationAttempt(t, db, attempt)
			}
			response := markReconciliationApplied(t, svc, cannotApply, reconciliationUser)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			for _, attempt := range attempts {
				var outcome, stage, reason, reasonText, finished string
				var elapsed int
				require.NoError(t, db.QueryRow(`SELECT outcome,stage,reason_code,reason_text,finished_at,elapsed_ms FROM apply_attempts WHERE id=?`, attempt.name).Scan(&outcome, &stage, &reason, &reasonText, &finished, &elapsed))
				if attempt.confirmed {
					require.Equal(t, "submitted_confirmed", outcome)
					require.Equal(t, "confirmation", stage)
					require.Equal(t, "operator_reconciled_confirmation", reason)
					require.Contains(t, reasonText, "operator")
				} else {
					require.Equal(t, attempt.outcome, outcome, attempt.name)
					require.Equal(t, "submit_dispatch", stage, attempt.name)
					require.Equal(t, "confirmation_missing", reason, attempt.name)
					require.Equal(t, "Original diagnostic", reasonText, attempt.name)
				}
				require.Equal(t, "2026-11-23T08:00:00Z", finished)
				require.Equal(t, 1200, elapsed)
			}
			var resume, cover, resumeVersion, coverVersion, pack, halal, appliedAt string
			var score int
			require.NoError(t, db.QueryRow(`SELECT resume_path,cover_letter_path,resume_content_version_id,cover_letter_content_version_id,document_refs_json,halal_verdict,suitability_score,applied_at FROM jobs_applied WHERE id=? AND user_id=?`, reconciliationJobID, reconciliationUser).Scan(&resume, &cover, &resumeVersion, &coverVersion, &pack, &halal, &score, &appliedAt))
			require.Equal(t, "resume.pdf", resume)
			require.Equal(t, "cover.pdf", cover)
			require.Equal(t, "resume-version", resumeVersion)
			require.Equal(t, "cover-version", coverVersion)
			require.JSONEq(t, reconciliationPack, pack)
			require.JSONEq(t, reconciliationHalal, halal)
			require.Equal(t, 8, score)
			var pending, skipped int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_pending_review WHERE job_id=? AND user_id=?`, reconciliationJobID, reconciliationUser).Scan(&pending))
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_skipped WHERE id=? AND user_id=?`, reconciliationJobID, reconciliationUser).Scan(&skipped))
			require.Zero(t, pending)
			require.Zero(t, skipped)
			response = markReconciliationApplied(t, svc, cannotApply, reconciliationUser)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var repeatedAt string
			var count int
			require.NoError(t, db.QueryRow(`SELECT applied_at FROM jobs_applied WHERE id=? AND user_id=?`, reconciliationJobID, reconciliationUser).Scan(&repeatedAt))
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_applied WHERE id=? AND user_id=?`, reconciliationJobID, reconciliationUser).Scan(&count))
			require.Equal(t, appliedAt, repeatedAt)
			require.Equal(t, 1, count)
		})
	}
}

func TestMarkApplied_ReconciliationRollbackIncludesHistoryAttemptsAndReview(t *testing.T) {
	for _, cannotApply := range []bool{false, true} {
		for _, failure := range []string{"attempt update", "review removal"} {
			t.Run(failure+map[bool]string{false: " pending", true: " cannot"}[cannotApply], func(t *testing.T) {
				svc, db := newTestServices(t)
				seedReconciliationReview(t, db, cannotApply, reconciliationLink)
				seedReconciliationAttempt(t, db, reconciliationAttempt{"target", reconciliationUser, "linkedin", reconciliationJobID, reconciliationLink, "submission_unknown", false})
				trigger := `CREATE TRIGGER reject_confirmation BEFORE UPDATE OF outcome ON apply_attempts BEGIN SELECT RAISE(ABORT,'forced reconciliation failure'); END`
				if failure == "review removal" {
					trigger = `CREATE TRIGGER reject_review_removal BEFORE DELETE ON jobs_pending_review BEGIN SELECT RAISE(ABORT,'forced removal failure'); END`
				}
				_, err := db.Exec(trigger)
				require.NoError(t, err)
				response := markReconciliationApplied(t, svc, cannotApply, reconciliationUser)
				require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
				var applied, pending, skipped int
				require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_applied WHERE id=?`, reconciliationJobID).Scan(&applied))
				require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_pending_review WHERE job_id=?`, reconciliationJobID).Scan(&pending))
				require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_skipped WHERE id=?`, reconciliationJobID).Scan(&skipped))
				require.Zero(t, applied)
				require.Equal(t, 1, pending)
				require.Equal(t, map[bool]int{false: 0, true: 1}[cannotApply], skipped)
				var outcome, stage, reason string
				require.NoError(t, db.QueryRow(`SELECT outcome,stage,reason_code FROM apply_attempts WHERE id='target'`).Scan(&outcome, &stage, &reason))
				require.Equal(t, "submission_unknown", outcome)
				require.Equal(t, "submit_dispatch", stage)
				require.Equal(t, "confirmation_missing", reason)
			})
		}
	}
}

func TestMarkApplied_EmptyLinkDoesNotReconcileUnrelatedEmptyLinks(t *testing.T) {
	svc, db := newTestServices(t)
	seedReconciliationReview(t, db, false, "")
	seedReconciliationAttempt(t, db, reconciliationAttempt{"other", reconciliationUser, "linkedin", "different-job", "", "submission_unknown", false})
	response := markReconciliationApplied(t, svc, false, reconciliationUser)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var outcome string
	require.NoError(t, db.QueryRow(`SELECT outcome FROM apply_attempts WHERE id='other'`).Scan(&outcome))
	require.Equal(t, "submission_unknown", outcome)
}

func TestMarkApplied_CannotApplyDocumentsRequireCompatibleListingURL(t *testing.T) {
	for _, tc := range []struct {
		name, skippedLink, preparedLink string
		wantDocuments                   bool
	}{
		{"matching listing", reconciliationLink, reconciliationLink, true},
		{"conflicting listing", reconciliationLink, "https://www.linkedin.com/jobs/view/different-job", false},
		{"legacy prepared row without link", reconciliationLink, "", true},
		{"legacy skipped row without link", "", reconciliationLink, true},
		{"legacy rows without links", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, db := newTestServices(t)
			seedReconciliationReview(t, db, true, tc.preparedLink)
			_, err := db.Exec(`UPDATE jobs_skipped SET link=? WHERE id=? AND user_id=?`,
				tc.skippedLink, reconciliationJobID, reconciliationUser)
			require.NoError(t, err)

			response := markReconciliationApplied(t, svc, true, reconciliationUser)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var link, resume, cover, resumeVersion, coverVersion, pack string
			require.NoError(t, db.QueryRow(`SELECT link,resume_path,cover_letter_path,
 resume_content_version_id,cover_letter_content_version_id,document_refs_json
 FROM jobs_applied WHERE id=? AND user_id=?`, reconciliationJobID, reconciliationUser).
				Scan(&link, &resume, &cover, &resumeVersion, &coverVersion, &pack))
			require.Equal(t, tc.skippedLink, link)
			if tc.wantDocuments {
				require.Equal(t, "resume.pdf", resume)
				require.Equal(t, "cover.pdf", cover)
				require.Equal(t, "resume-version", resumeVersion)
				require.Equal(t, "cover-version", coverVersion)
				require.JSONEq(t, reconciliationPack, pack)
			} else {
				require.Empty(t, resume, "a conflicting listing cannot supply the applied resume")
				require.Empty(t, cover)
				require.Empty(t, resumeVersion)
				require.Empty(t, coverVersion)
				require.Empty(t, pack)
			}
		})
	}
}

func TestMarkApplied_CollisionCannotBorrowOtherUserOrPlatformHistory(t *testing.T) {
	for _, conflict := range []struct{ name, user, platform string }{{"other user", "other-candidate", "linkedin"}, {"other platform", reconciliationUser, "seek"}} {
		t.Run(conflict.name, func(t *testing.T) {
			svc, db := newTestServices(t)
			seedReconciliationReview(t, db, false, reconciliationLink)
			seedReconciliationAttempt(t, db, reconciliationAttempt{"target", reconciliationUser, "linkedin", reconciliationJobID, reconciliationLink, "submission_unknown", false})
			_, err := db.Exec(`INSERT INTO jobs_applied(id,user_id,platform,company,role,link) VALUES(?,?,?,'Existing','Coordinator',?)`, reconciliationJobID, conflict.user, conflict.platform, reconciliationLink)
			require.NoError(t, err)
			response := markReconciliationApplied(t, svc, false, reconciliationUser)
			require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
			var outcome string
			require.NoError(t, db.QueryRow(`SELECT outcome FROM apply_attempts WHERE id='target'`).Scan(&outcome))
			require.Equal(t, "submission_unknown", outcome)
			var pending int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_pending_review WHERE job_id=? AND user_id=?`, reconciliationJobID, reconciliationUser).Scan(&pending))
			require.Equal(t, 1, pending)
		})
	}
}

func TestMarkApplied_OtherUserCannotReconcileReview(t *testing.T) {
	for _, cannotApply := range []bool{false, true} {
		svc, db := newTestServices(t)
		seedReconciliationReview(t, db, cannotApply, reconciliationLink)
		seedReconciliationAttempt(t, db, reconciliationAttempt{"target", reconciliationUser, "linkedin", reconciliationJobID, reconciliationLink, "submission_unknown", false})
		response := markReconciliationApplied(t, svc, cannotApply, "other-candidate")
		require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
		var outcome string
		require.NoError(t, db.QueryRow(`SELECT outcome FROM apply_attempts WHERE id='target'`).Scan(&outcome))
		require.Equal(t, "submission_unknown", outcome)
	}
}
