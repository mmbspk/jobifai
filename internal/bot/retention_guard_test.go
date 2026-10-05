package bot

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/llm"
)

type refusingWorkGuard struct{ err error }

func (g refusingWorkGuard) BeginWork(context.Context, string) error { return g.err }
func (g refusingWorkGuard) EndWork(string)                          {}
func TestManager_PrepareAndApplyAcquireRetentionGuardBeforeWork(t *testing.T) {
	blocked := errors.New("retention guard canceled")
	m := &Manager{submitWorkGuard: refusingWorkGuard{blocked}}
	require.ErrorIs(t, m.PrepareReviewDocuments(context.Background(), "u", "j"), blocked)
	_, err := m.ApplyFromURL(context.Background(), "u", "https://example.com", "", false)
	require.ErrorIs(t, err, blocked)
	require.ErrorIs(t, m.runSubmit(context.Background(), "u", SubmitRequest{}), blocked)
}

func TestAutomaticApply_AcquiresGuardBeforeOpeningPlatform(t *testing.T) {
	b := &Bot{cfg: Config{UserID: "u", Documents: &documents.Service{WorkGuard: refusingWorkGuard{context.Canceled}}}}
	require.False(t, b.submitEasyApply(context.Background(), nil, linkedInJob{}, nil, 0, "", nil, llm.UsageSnapshot{}))
	require.Equal(t, seekSubmitCannotApply, b.submitSeekApplication(context.Background(), nil, seekJob{}, nil, 0, "", nil, llm.UsageSnapshot{}))
}

func TestAutomaticApply_PublishesReferencesBeforeRetention(t *testing.T) {
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	calls := 0
	b := &Bot{cfg: Config{DB: db, UserID: "u", OnApplied: func() {
		calls++
		var count int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM jobs_applied WHERE user_id='u' AND resume_content_version_id='version' AND document_refs_json='pack'`).Scan(&count))
		require.Equal(t, calls, count)
	}}}
	b.recordApplied(linkedInJob{ID: "linkedin", URL: "https://example.com/1"}, "resume.pdf", "", "version", "", "pack", 0, nil)
	b.recordSeekApplied(seekJob{ID: "seek", URL: "https://example.com/2"}, "resume.pdf", "", "version", "", "pack", 0, nil)
	require.Equal(t, 2, calls)
}
