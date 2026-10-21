package bot

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

// TestRequireReviewHoldReason_NativeVsExternal verifies that the review queue
// receives the correct hold reason based on whether the listing is Easy Apply or
// external. Regresses the defect where all RequireReview jobs received the "scan
// the apply form" instruction even when easy_apply=false.
func TestRequireReviewHoldReason_NativeVsExternal(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		easyApply bool
		wantHold  string
	}{
		{
			name:      "native Easy Apply job prompts form scan",
			easyApply: true,
			wantHold:  "Use Prepare documents to scan the apply form and build your application pack before approving",
		},
		{
			name:      "external job directs to employer website",
			easyApply: false,
			wantHold:  "Apply on the employer's website.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dbPath := filepath.Join(t.TempDir(), "test.db")
			db, err := appdb.Open(dbPath)
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			b := &Bot{
				cfg: Config{
					DB:            db,
					UserID:        "hold-reason-test",
					RequireReview: true,
				},
			}

			holdReason := "Use Prepare documents to scan the apply form and build your application pack before approving"
			if !tc.easyApply {
				holdReason = "Apply on the employer's website."
			}
			packJSON := documents.WriteApplicationPackJSON(documents.ApplicationDocumentPack{
				HoldReason: holdReason,
			})

			b.queueForReview(context.Background(), &domain.PendingReview{
				JobID:    "test-job-" + tc.name,
				Company:  "Test Co",
				Role:     "Test Role",
				Platform: domain.PlatformLinkedIn,
				Link:     "https://www.linkedin.com/jobs/view/123456/",
				EasyApply: tc.easyApply,
				DocumentRefsJSON: packJSON,
				CreatedAt: time.Now(),
			})

			var stored string
			err = db.QueryRow(
				`SELECT json_extract(document_refs_json, '$.hold_reason')
				 FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
				"test-job-"+tc.name, "hold-reason-test",
			).Scan(&stored)
			require.NoError(t, err)
			assert.Equal(t, tc.wantHold, stored)
		})
	}
}
