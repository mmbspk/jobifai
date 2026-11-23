package documents_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

func TestExportApplicationUploadFile_IsolatedPerUserAndVersion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a, err := documents.ExportApplicationUploadFile(root, "user-a", "job-1", "ver-1", "resume", "resume.pdf", []byte("%PDF-a"))
	require.NoError(t, err)
	b, err := documents.ExportApplicationUploadFile(root, "user-b", "job-1", "ver-1", "resume", "resume.pdf", []byte("%PDF-b"))
	require.NoError(t, err)
	require.NotEqual(t, a, b)
	raw, err := os.ReadFile(a)
	require.NoError(t, err)
	require.Equal(t, []byte("%PDF-a"), raw)
	rawB, err := os.ReadFile(b)
	require.NoError(t, err)
	require.Equal(t, []byte("%PDF-b"), rawB)
	require.Contains(t, filepath.Base(filepath.Dir(a)), "ver-1")
	require.Equal(t, "resume_job_job-1.pdf", filepath.Base(a))
}

func TestApplicationUploadFilename_PreservesVersionGuardAndMedia(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ jobID, old, want string }{
		{"12345", "resume_abc12345.pdf", "resume_job_12345_abc12345.pdf"},
		{"67890", "cover_letter_fedcba98.pdf", "cover_letter_job_67890_fedcba98.pdf"},
		{"external-42", "My Resume_0123abcd.docx", "My Resume_job_external-42_0123abcd.docx"},
		{"", "resume_abc12345.pdf", "resume_abc12345.pdf"},
		{"12345", "resume_job_12345_abc12345.pdf", "resume_job_12345_abc12345.pdf"},
		{"12345678", "resume_job_12345678.pdf", "resume_job_12345678.pdf"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			require.Equal(t, tc.want, documents.ApplicationUploadFilename(tc.jobID, tc.old))
		})
	}
}

func TestApplicationUploadFilename_SafeDistinctAndBoundedJobIDs(t *testing.T) {
	t.Parallel()
	a := documents.ApplicationUploadFilename("tenant/42", "resume_abc12345.pdf")
	b := documents.ApplicationUploadFilename("tenant\\42", "resume_abc12345.pdf")
	require.NotEqual(t, a, b, "different opaque IDs must not collapse after sanitization")
	for _, name := range []string{a, b, documents.ApplicationUploadFilename("../42:\n", "resume_abc12345.pdf")} {
		require.NotContains(t, name, "/")
		require.NotContains(t, name, "\\")
		require.NotContains(t, name, ":")
		require.NotContains(t, name, "\n")
		require.True(t, strings.HasSuffix(name, "_abc12345.pdf"))
	}
	long := documents.ApplicationUploadFilename(strings.Repeat("job", 100), strings.Repeat("é", 120)+"_abc12345.pdf")
	require.LessOrEqual(t, len(long), 240)
	require.True(t, utf8.ValidString(long))
	require.True(t, strings.HasSuffix(long, "_abc12345.pdf"))
}

func TestExportApplicationUploadFile_JobCopiesReuseVersionWithoutRenamingHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	renderer := &countingRenderer{}
	svc, _, userID := newTestService(t, renderer)
	rc := documents.RenderContext{Language: "en"}
	resumeVersion, err := svc.CreateResumeFromProfile(ctx, userID, "Same organization — Same role", rc)
	require.NoError(t, err)
	coverVersion, err := svc.SaveCoverLetter(ctx, userID, "Same organization — Same role cover", "Supplied professional experience.", rc)
	require.NoError(t, err)
	require.Equal(t, int32(2), renderer.calls.Load())
	root := t.TempDir()
	legacyPath := filepath.Join(root, "job_applications", userID, "11111", resumeVersion, "resume_"+resumeVersion[:8]+".pdf")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyPath), 0o750))
	require.NoError(t, os.WriteFile(legacyPath, []byte("previously submitted bytes"), 0o640))
	legacyPack := documents.ApplicationDocumentPack{Resume: domain.ApplicationDocumentRef{
		Kind: documents.KindResume, Outcome: domain.DocumentOutcomeLocalFile, ContentVersionID: resumeVersion, LocalPath: legacyPath,
	}}
	storedPack := documents.WriteApplicationPackJSON(legacyPack)
	deps := documents.MaterializeDeps{ExportUpload: func(uid, jid, vid, kind string, payload documents.VersionUploadPayload) (string, error) {
		return documents.ExportApplicationUploadFile(root, uid, jid, vid, kind, payload.Filename, payload.Data)
	}}
	res := documents.ResolveResult{
		Resume: documents.ResolvedDocument{Kind: documents.KindResume, Outcome: domain.DocumentOutcomeLocalFile, VersionID: resumeVersion},
		Cover:  documents.ResolvedDocument{Kind: documents.KindCoverLetter, Outcome: domain.DocumentOutcomeLocalFile, VersionID: coverVersion},
	}
	first, err := svc.MaterializeApplicationDocs(ctx, userID, "11111", "Same organization", "Same role", "", res, domain.DocumentPolicies{}, rc, nil, deps)
	require.NoError(t, err)
	second, err := svc.MaterializeApplicationDocs(ctx, userID, "22222", "Same organization", "Same role", "", res, domain.DocumentPolicies{}, rc, nil, deps)
	require.NoError(t, err)
	require.False(t, first.Hold)
	require.False(t, second.Hold)
	require.NotEqual(t, filepath.Base(first.ResumePath), filepath.Base(second.ResumePath))
	require.NotEqual(t, filepath.Base(first.CoverPath), filepath.Base(second.CoverPath))
	require.Equal(t, "resume_job_11111_"+resumeVersion[:8]+".pdf", filepath.Base(first.ResumePath))
	require.Equal(t, "cover_letter_job_22222_"+coverVersion[:8]+".pdf", filepath.Base(second.CoverPath))
	require.Equal(t, resumeVersion, first.Pack.Resume.ContentVersionID)
	require.Equal(t, coverVersion, second.Pack.Cover.ContentVersionID)
	require.Equal(t, int32(2), renderer.calls.Load(), "job filename changes must reuse stored renders")
	for _, path := range []string{first.ResumePath, second.ResumePath} {
		raw, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.Equal(t, []byte("%PDF-resume"), raw)
	}
	old, err := os.ReadFile(legacyPath)
	require.NoError(t, err)
	require.Equal(t, []byte("previously submitted bytes"), old)
	require.Equal(t, legacyPath, documents.ParseApplicationPackJSON(storedPack).Resume.LocalPath)

	nextVersion, err := svc.CreateResumeFromProfile(ctx, userID, "Updated role document", rc)
	require.NoError(t, err)
	res.Resume.VersionID = nextVersion
	next, err := svc.MaterializeApplicationDocs(ctx, userID, "11111", "Same organization", "Same role", "", res, domain.DocumentPolicies{}, rc, nil, deps)
	require.NoError(t, err)
	require.NotEqual(t, first.ResumePath, next.ResumePath)
	require.NotEqual(t, filepath.Base(first.ResumePath), filepath.Base(next.ResumePath))
}
