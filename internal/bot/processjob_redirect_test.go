package bot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

type capturingScorer struct {
	gotDesc string
	score   int
}

func (s *capturingScorer) EvaluateJob(_ context.Context, _ *domain.ResumeProfile, desc string) (domain.JobScore, error) {
	s.gotDesc = desc
	score := s.score
	if score == 0 {
		score = 8
	}
	return domain.JobScore{Score: score, Reasoning: "Evaluated verified listing"}, nil
}

// Exercise real processJob routing: a redirect is not proof of expiry, and
// another selected job's description/Apply button must never be consumed.
func TestProcessJob_ListingResolution(t *testing.T) {
	verifiedDescription := strings.Repeat("Verified intended listing responsibilities and requirements. ", 4)
	for _, tc := range []struct {
		name, path, html string
		scored           bool
	}{
		{"collections redirect", "/jobs/collections/similar-jobs/?currentJobId=4478417422&referenceJobId=4479748087", `<main data-job-id="4478417422"><h1>Wrong role</h1><button>Easy Apply</button></main>`, false},
		{"non-job redirect", "/jobs/not-found", `<main>Unable to load listing</main>`, false},
		{"external with recommendation", "", `<main data-job-id="4479748087"><h1>Principal Developer at MYOB</h1><div data-job-description>` + verifiedDescription + `</div><a href="https://employer.example/apply">Apply</a><section data-job-id="4478417422"><button>Easy Apply</button></section></main>`, true},
		{"thin description", "", `<main data-job-id="4479748087"><h1>Principal Developer at MYOB</h1><a href="https://employer.example/apply">Apply</a><div data-job-description>Short description.</div></main>`, false},
		{"URL correct but stale pane", "", `<main data-job-id="4478417422"><button>Easy Apply</button></main>`, false},
		{"verified listing without recognized control", "", `<main data-job-id="4479748087"><h1>Principal Developer at MYOB</h1><div data-job-description>` + verifiedDescription + `</div><button>Continue to employer</button></main>`, true},
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
			failure := !tc.scored || tc.name == "verified listing without recognized control"
			if failure {
				require.Equal(t, 1, count)
			} else {
				require.Zero(t, count)
			}
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_applied WHERE user_id='u1'`).Scan(&count))
			require.Zero(t, count)
			if tc.scored {
				require.Contains(t, scorer.gotDesc, strings.TrimSpace(verifiedDescription))
				require.Contains(t, scorer.gotDesc, "Company: MYOB")
				var score int
				var reasoning string
				query := `SELECT suitability_score,suitability_reasoning FROM jobs_pending_review WHERE job_id=? AND user_id='u1'`
				if failure {
					query = `SELECT suitability_score,suitability_reasoning FROM jobs_skipped WHERE id=? AND user_id='u1'`
				}
				require.NoError(t, db.QueryRow(query, job.ID).Scan(&score, &reasoning))
				require.Equal(t, 8, score)
				require.Equal(t, "Evaluated verified listing", reasoning)
			} else {
				require.Empty(t, scorer.gotDesc)
				var diagnostic string
				require.NoError(t, db.QueryRow(`SELECT failure_details_json FROM jobs_skipped WHERE id=? AND user_id='u1'`, job.ID).Scan(&diagnostic))
				require.NotEmpty(t, diagnostic)
			}
		})
	}
}

func TestProcessJob_UnrecognizedControlStillRejectsLowScore(t *testing.T) {
	br := launchTestBrowser(t)
	srv := fixtureServer(t, "4479748087", "", `<main data-job-id="4479748087"><h1>Role</h1><div data-job-description>`+strings.Repeat("Actual job responsibilities and requirements. ", 5)+`</div><button>Continue to employer</button></main>`, "")
	db, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer db.Close()
	scorer := &capturingScorer{score: 3}
	b := &Bot{cfg: Config{DB: db, UserID: "u1", Scorer: scorer}, stopCh: make(chan struct{}), seenCache: newJobSeenCache()}
	job := linkedInJob{ID: "4479748087", URL: srv.URL + "/jobs/view/4479748087", Title: "Role", Company: "Company"}
	require.False(t, b.processJob(context.Background(), br, job))
	require.NotEmpty(t, scorer.gotDesc)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_pending_review WHERE user_id='u1'`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_skipped WHERE user_id='u1' AND suitability_score=3`).Scan(&count))
	require.Equal(t, 1, count)
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
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_pending_review WHERE user_id='u1'`).Scan(&count))
	require.Zero(t, count)
}

func TestProcessJob_CapturedSDUIExternal_IsScored(t *testing.T) {
	br := launchTestBrowser(t)
	html, err := os.ReadFile(filepath.Join("testdata", "linkedin", "sdui-external.html"))
	require.NoError(t, err)
	srv := fixtureServer(t, "4479800927", "", string(html), "")
	db := testDB(t)
	scorer := &capturingScorer{}
	b := &Bot{cfg: Config{DB: db, UserID: "u1", Platform: domain.PlatformLinkedIn, Scorer: scorer}, stopCh: make(chan struct{}), seenCache: newJobSeenCache()}
	job := linkedInJob{ID: "4479800927", URL: srv.URL + "/jobs/view/4479800927", Title: "Mid-Level Product Manager", Company: "Zeller"}
	require.False(t, b.processJob(context.Background(), br, job))
	require.Contains(t, scorer.gotDesc, "Project Delivery:")
	require.Greater(t, len(scorer.gotDesc), 1000)
	var score int
	var reasoning string
	var easy bool
	require.NoError(t, db.QueryRow(`SELECT suitability_score,suitability_reasoning,easy_apply FROM jobs_pending_review WHERE user_id='u1' AND job_id=?`, job.ID).Scan(&score, &reasoning, &easy))
	require.Equal(t, 8, score)
	require.Equal(t, "Evaluated verified listing", reasoning)
	require.False(t, easy)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_applied WHERE user_id='u1'`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_skipped WHERE user_id='u1'`).Scan(&count))
	require.Zero(t, count)
}

func TestEvaluationRetry_CapturedExternalRechecksAndScores(t *testing.T) {
	br := launchTestBrowser(t)
	html, err := os.ReadFile(filepath.Join("testdata", "linkedin", "sdui-external.html"))
	require.NoError(t, err)
	srv := fixtureServer(t, "4479800927", "", string(html), "")
	db := testDB(t)
	scorer := &capturingScorer{}
	b := New(Config{DB: db, UserID: "u1", Platform: domain.PlatformLinkedIn, Scorer: scorer})
	url := srv.URL + "/jobs/view/4479800927"
	_, err = db.Exec(`INSERT INTO jobs_approved_queue(job_id,user_id,platform,company,role,location,link,approved_at,needs_evaluation) VALUES('4479800927','u1','linkedin','Zeller','Mid-Level Product Manager','Melbourne',?,datetime('now'),1)`, url)
	require.NoError(t, err)
	b.seenCache.mark("4479800927", seenSkipped)
	require.Zero(t, b.processApprovedQueue(context.Background(), br, 10))
	require.Contains(t, scorer.gotDesc, "Project Delivery:")
	var score int
	require.NoError(t, db.QueryRow(`SELECT suitability_score FROM jobs_pending_review WHERE user_id='u1' AND job_id='4479800927'`).Scan(&score))
	require.Equal(t, 8, score)
	var queued int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM jobs_approved_queue WHERE user_id='u1'`).Scan(&queued))
	require.Zero(t, queued)
}
