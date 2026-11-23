package bot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
)

func TestApplicationFilename_JobIdentityRetainsDuplicateUploadGuard(t *testing.T) {
	for _, old := range []string{"resume_abc12345.pdf", "cover_letter_9876fedc.pdf", "My Resume_1234abcd.docx"} {
		current := documents.ApplicationUploadFilename("12345", old)
		require.True(t, isVersionScopedFilename(current), current)
		require.True(t, linkedInFilenameLike(current), current)
	}
}

func TestSavePDF_JobCopiesDoNotOverwriteOtherJobsOrLegacyFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	bot := &Bot{}
	legacy := bot.savePDF([]byte("legacy bytes"), "Company", "Coordinator", "resume", "")
	first := bot.savePDF([]byte("first job"), "Company", "Coordinator", "resume", "12345")
	second := bot.savePDF([]byte("second job"), "Company", "Coordinator", "resume", "67890")
	cover := bot.savePDF([]byte("cover bytes"), "Company", "Coordinator", "cover_letter", "12345")
	require.Equal(t, "resume_job_12345.pdf", filepath.Base(first))
	require.Equal(t, "resume_job_67890.pdf", filepath.Base(second))
	require.Equal(t, "cover_letter_job_12345.pdf", filepath.Base(cover))
	for _, doc := range []struct{ path, content string }{
		{legacy, "legacy bytes"}, {first, "first job"}, {second, "second job"}, {cover, "cover bytes"},
	} {
		data, err := os.ReadFile(doc.path)
		require.NoError(t, err)
		require.Equal(t, doc.content, string(data))
	}
}
