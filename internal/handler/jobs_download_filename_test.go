package handler_test

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppliedDownload_JobNamesPreserveLegacyFilesAndOwnerScope(t *testing.T) {
	router, svc, token := newResumeRouter(t)
	userID := userIDFromToken(t, router, token)
	directory := t.TempDir()
	resume := filepath.Join(directory, "resume.pdf")
	cover := filepath.Join(directory, "cover_letter.pdf")
	resumeBytes, coverBytes := []byte("%PDF-legacy-resume"), []byte("%PDF-legacy-cover")
	require.NoError(t, os.WriteFile(resume, resumeBytes, 0600))
	require.NoError(t, os.WriteFile(cover, coverBytes, 0600))
	for _, jobID := range []string{"job-101", "job-102"} {
		_, err := svc.DB.Exec(`INSERT INTO jobs_applied(id,user_id,platform,company,role,link,resume_path,cover_letter_path)
 VALUES(?,?,'seek','Company','Coordinator','https://example.com/job',?,?)`, jobID, userID, resume, cover)
		require.NoError(t, err)
		for _, doc := range []struct {
			kind string
			data []byte
		}{{"resume", resumeBytes}, {"cover_letter", coverBytes}} {
			response := authGet(t, router, "/api/jobs/applied/"+jobID+"/pdf/"+doc.kind+"?inline=1", token)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Equal(t, doc.data, response.Body.Bytes())
			disposition, params, err := mime.ParseMediaType(response.Header().Get("Content-Disposition"))
			require.NoError(t, err)
			require.Equal(t, "inline", disposition)
			require.Equal(t, doc.kind+"_job_"+jobID+".pdf", params["filename"])
		}
	}
	other := registerAndLogin(t, router, "other-download@example.com", "password123")
	response := authGet(t, router, "/api/jobs/applied/job-101/pdf/resume", other)
	require.Equal(t, http.StatusNotFound, response.Code)
	for _, document := range []struct {
		path string
		data []byte
	}{{resume, resumeBytes}, {cover, coverBytes}} {
		data, err := os.ReadFile(document.path)
		require.NoError(t, err)
		require.Equal(t, document.data, data)
	}
	var storedResume, storedCover string
	require.NoError(t, svc.DB.QueryRow(`SELECT resume_path,cover_letter_path FROM jobs_applied WHERE id='job-101' AND user_id=?`, userID).Scan(&storedResume, &storedCover))
	require.Equal(t, resume, storedResume)
	require.Equal(t, cover, storedCover)
}
