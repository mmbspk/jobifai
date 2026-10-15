package bot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

type capturingScorer struct{ gotDesc string }

func (s *capturingScorer) EvaluateJob(_ context.Context, _ *domain.ResumeProfile, desc string) (domain.JobScore, error) {
	s.gotDesc = desc
	return domain.JobScore{Score: 8}, nil
}

// Exercise real processJob routing: a redirect is not proof of expiry, and
// another selected job's description/Apply button must never be consumed.
func TestProcessJob_ListingResolution(t *testing.T) {
	for _, tc := range []struct {
		name, path, html string
		scored           bool
	}{
		{"collections redirect", "/jobs/collections/similar-jobs/?currentJobId=4478417422&referenceJobId=4479748087", `<main data-job-id="4478417422"><h1>Wrong role</h1><button>Easy Apply</button></main>`, false},
		{"non-job redirect", "/jobs/not-found", `<main>Unable to load listing</main>`, false},
		{"external with recommendation", "", `<main data-job-id="4479748087"><h1>Principal Developer at MYOB</h1><a href="https://employer.example/apply">Apply</a><section data-job-id="4478417422"><button>Easy Apply</button></section></main>`, true},
		{"thin description", "", `<main data-job-id="4479748087"><h1>Principal Developer at MYOB</h1><a href="https://employer.example/apply">Apply</a><div data-job-description>Short description.</div></main>`, true},
		{"URL correct but stale pane", "", `<main data-job-id="4478417422"><button>Easy Apply</button></main>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			br := launchTestBrowser(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.path != "" && r.URL.Path == "/jobs/view/4479748087" {
					http.Redirect(w, r, tc.path, http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, tc.html)
			}))
			defer srv.Close()
			db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
			require.NoError(t, err)
			defer db.Close()
			scorer := &capturingScorer{}
			b := &Bot{cfg: Config{DB: db, UserID: "u1", Platform: domain.PlatformLinkedIn, Scorer: scorer}, stopCh: make(chan struct{}), seenCache: newJobSeenCache()}
			job := linkedInJob{ID: "4479748087", URL: srv.URL + "/jobs/view/4479748087", Title: "Principal Developer", Company: "MYOB"}
			require.False(t, b.processJob(context.Background(), br, job))
			var count int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_skipped WHERE user_id='u1'`).Scan(&count))
			require.Zero(t, count)
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_applied WHERE user_id='u1'`).Scan(&count))
			require.Zero(t, count)
			var easy bool
			var pack string
			require.NoError(t, db.QueryRow(`SELECT easy_apply,document_refs_json FROM jobs_pending_review WHERE job_id=? AND user_id='u1'`, job.ID).Scan(&easy, &pack))
			require.False(t, easy)
			if tc.scored {
				require.Equal(t, "Principal Developer at MYOB", scorer.gotDesc)
			} else {
				require.Empty(t, scorer.gotDesc)
				require.Contains(t, pack, "Could not verify")
			}
		})
	}
}

func TestProcessJob_BrowserFailurePreservesReview(t *testing.T) {
	br := launchTestBrowser(t)
	require.NoError(t, br.Close())
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer db.Close()
	b := &Bot{cfg: Config{DB: db, UserID: "u1", Platform: domain.PlatformLinkedIn}, seenCache: newJobSeenCache()}
	require.False(t, b.processJob(context.Background(), br, linkedInJob{ID: "4479748087", URL: "https://www.linkedin.com/jobs/view/4479748087", Title: "Role", Company: "Company"}))
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_skipped WHERE user_id='u1'`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_pending_review WHERE user_id='u1'`).Scan(&count))
	require.Equal(t, 1, count)
}
