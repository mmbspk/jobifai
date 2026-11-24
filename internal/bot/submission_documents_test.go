package bot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestUnknownSubmissionDocuments_PersistAfterCancellationWithoutGrantingRetry(t *testing.T) {
	db := openMappingDB(t)
	b := New(Config{DB: db, UserID: "candidate"})
	job := linkedInJob{ID: "12345678", URL: "https://au.seek.com/job/12345678", Company: "Company", Title: "Coordinator"}
	lazy := &lazyDocGen{b: b, resume: "resume_job_12345678_abc12345.pdf", cover: "cover_letter_job_12345678_1234abcd.pdf", resumeVersionID: "resume-version", coverVersionID: "cover-version", refsJSON: `{"resume":{"content_version_id":"resume-version"},"cover":{"content_version_id":"cover-version"}}`}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b.preserveUnknownSubmissionDocuments(ctx, domain.PlatformSeek, job, lazy, 8, "Reviewed", nil)
	var resume, cover, resumeVersion, coverVersion, refs string
	var easyApply bool
	require.NoError(t, db.QueryRow(`SELECT resume_path,cover_letter_path,resume_content_version_id,cover_letter_content_version_id,document_refs_json,easy_apply FROM jobs_pending_review WHERE user_id=? AND job_id=?`, b.cfg.UserID, job.ID).Scan(&resume, &cover, &resumeVersion, &coverVersion, &refs, &easyApply))
	require.Equal(t, lazy.resume, resume)
	require.Equal(t, lazy.cover, cover)
	require.Equal(t, lazy.resumeVersionID, resumeVersion)
	require.Equal(t, lazy.coverVersionID, coverVersion)
	require.Contains(t, refs, `"submission_unknown":true`)
	require.Contains(t, refs, `"content_version_id":"cover-version"`)
	require.False(t, easyApply)
	var applied int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM jobs_applied WHERE user_id=?`, b.cfg.UserID).Scan(&applied))
	require.Zero(t, applied)
}

func TestUnknownSubmissionDocuments_KeepOtherOwnerPlatformAndListing(t *testing.T) {
	for _, tc := range []struct{ name, owner, platform, link string }{
		{"other candidate", "other", "seek", "https://au.seek.com/job/12345678"},
		{"other platform", "candidate", "linkedin", "https://www.linkedin.com/jobs/view/12345678"},
		{"other listing", "candidate", "seek", "https://au.seek.com/job/22345678"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openMappingDB(t)
			_, err := db.Exec(`INSERT INTO jobs_pending_review(job_id,user_id,platform,company,role,link,resume_path,easy_apply) VALUES('12345678',?,?,'Company','Coordinator',?,'original.pdf',1)`, tc.owner, tc.platform, tc.link)
			require.NoError(t, err)
			b := New(Config{DB: db, UserID: "candidate"})
			job := linkedInJob{ID: "12345678", URL: "https://au.seek.com/job/12345678", Company: "Company", Title: "Coordinator"}
			b.preserveUnknownSubmissionDocuments(context.Background(), domain.PlatformSeek, job, &lazyDocGen{b: b, resume: "submitted.pdf"}, 8, "Reviewed", nil)
			var owner, path string
			var easyApply bool
			require.NoError(t, db.QueryRow(`SELECT user_id,resume_path,easy_apply FROM jobs_pending_review WHERE job_id='12345678'`).Scan(&owner, &path, &easyApply))
			require.Equal(t, tc.owner, owner)
			require.Equal(t, "original.pdf", path)
			require.True(t, easyApply)
		})
	}
}

func TestUnknownSubmissionDocuments_MissingArtifactsDoNotGenerateOrReplaceReview(t *testing.T) {
	db := openMappingDB(t)
	b := New(Config{DB: db, UserID: "candidate"})
	_, err := db.Exec(`INSERT INTO jobs_pending_review(job_id,user_id,platform,company,role,resume_path,easy_apply) VALUES('12345678','candidate','seek','Company','Coordinator','original.pdf',1)`)
	require.NoError(t, err)
	b.preserveUnknownSubmissionDocuments(context.Background(), domain.PlatformSeek, linkedInJob{ID: "12345678"}, &lazyDocGen{b: b}, 8, "Reviewed", nil)
	var path string
	var easyApply bool
	require.NoError(t, db.QueryRow(`SELECT resume_path,easy_apply FROM jobs_pending_review WHERE job_id='12345678'`).Scan(&path, &easyApply))
	require.Equal(t, "original.pdf", path)
	require.True(t, easyApply)
}
