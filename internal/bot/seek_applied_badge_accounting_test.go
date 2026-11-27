package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/go-rod/rod"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

// A badge can describe an old application or a visit to an external employer.
// Neither observation proves a new submission or its date.
func TestSeekObservedAppliedBadgeSkipsWithoutNewSubmission(t *testing.T) {
	for _, source := range []string{"card", "verified page"} {
		t.Run(source, func(t *testing.T) {
			database := pageHookDB(t)
			manager := pageHookManager(t, database)
			scorer := &capturingScorer{}
			appliedCallbacks := 0
			b := New(Config{DB: database, UserID: pageTestUser, Platform: domain.PlatformSeek, Scorer: scorer, JobPageReader: manager.pageReader, JobPageStore: manager.pageStore, OnApplied: func() { appliedCallbacks++ }})
			b.seenCache = newJobSeenCache()
			job := seekJob{ID: "96103223", URL: "https://www.seek.com.au/job/96103223", Title: "Engineer", Company: "Acme", AlreadyApplied: source == "card"}
			var browser *rod.Browser
			if source == "verified page" {
				browser = workerFixtureBrowser(t, func(string) string { return seekObservedBadgeHTML(job.ID) })
			}
			before := b.countAppliedToday()
			require.False(t, b.processSeekJobEvaluation(t.Context(), browser, job, false))
			require.Equal(t, before, b.countAppliedToday(), "observing an old badge cannot spend today's application limit")
			require.Zero(t, appliedCallbacks)
			require.Empty(t, scorer.gotDesc, "already-applied observations do not score or start an application")
			var applied, attempts, learned int
			require.NoError(t, database.QueryRow("SELECT count(*) FROM jobs_applied WHERE user_id=?", pageTestUser).Scan(&applied))
			require.Zero(t, applied)
			require.NoError(t, database.QueryRow("SELECT count(*) FROM apply_attempts WHERE user_id=?", pageTestUser).Scan(&attempts))
			require.Zero(t, attempts)
			require.NoError(t, database.QueryRow("SELECT count(*) FROM form_field_mappings WHERE user_id=?", pageTestUser).Scan(&learned))
			require.Zero(t, learned)
			var reason, platform string
			require.NoError(t, database.QueryRow("SELECT skip_reason,platform FROM jobs_skipped WHERE user_id=? AND id=?", pageTestUser, job.ID).Scan(&reason, &platform))
			require.Equal(t, "already_applied_indicator", reason)
			require.Equal(t, string(domain.PlatformSeek), platform)
			require.Equal(t, "in skipped list", b.alreadyAppliedReason(job.ID), "future discovery should not retry the observed badge")
			if browser != nil {
				var verifiedReads int
				require.NoError(t, database.QueryRow("SELECT count(*) FROM job_page_read_events WHERE user_id=? AND record_id=? AND failure_code=''", pageTestUser, job.ID).Scan(&verifiedReads))
				require.Equal(t, 1, verifiedReads, "the page branch must follow a successful owned listing read")
				probe := navigateAndWait(t, browser, job.URL)
				state, err := probe.Eval("()=>({input:localStorage.getItem('badge-input')==='true',click:localStorage.getItem('badge-click')==='true'})")
				require.NoError(t, err)
				require.False(t, state.Value.Get("input").Bool())
				require.False(t, state.Value.Get("click").Bool())
			}
		})
	}
}

func seekObservedBadgeHTML(id string) string {
	description := strings.Repeat("Maintain secure services and ship reliable production systems. ", 8)
	html := pageHookHTML("seek", id, description)
	html = strings.Replace(html, "</main>", "<div id='applied-date-message'>Visited employer application site previously</div></main>", 1)
	return html + "<script>document.addEventListener('input',()=>localStorage.setItem('badge-input','true'));document.addEventListener('click',()=>localStorage.setItem('badge-click','true'));</script>"
}

func TestSeekObservedAppliedBadgePreservesUnresolvedAttempt(t *testing.T) {
	for _, source := range []string{"card", "verified page"} {
		t.Run(source, func(t *testing.T) {
			database := pageHookDB(t)
			manager := pageHookManager(t, database)
			b := New(Config{DB: database, UserID: pageTestUser, Platform: domain.PlatformSeek, JobPageReader: manager.pageReader, JobPageStore: manager.pageStore})
			job := seekJob{ID: "96103223", URL: "https://www.seek.com.au/job/96103223", Title: "Engineer", Company: "Acme", AlreadyApplied: source == "card"}
			previous := newAttemptContext(pageTestUser, string(domain.PlatformSeek), job.ID, job.URL, job.Title, job.Company, "fixture", "")
			beginAttempt(database, previous)
			finishAttempt(database, previous, OutcomeSubmissionUnknown, "submitted", "seek_submit_unconfirmed", "No verified delivery result")
			var finishedBefore string
			require.NoError(t, database.QueryRow("SELECT finished_at FROM apply_attempts WHERE id=?", previous.AttemptID).Scan(&finishedBefore))
			var browser *rod.Browser
			if source == "verified page" {
				browser = workerFixtureBrowser(t, func(string) string { return seekObservedBadgeHTML(job.ID) })
			}
			require.False(t, b.processSeekJobEvaluation(t.Context(), browser, job, false))
			var outcome, stage, code, finished string
			require.NoError(t, database.QueryRow("SELECT outcome,stage,reason_code,finished_at FROM apply_attempts WHERE id=?", previous.AttemptID).Scan(&outcome, &stage, &code, &finished))
			require.Equal(t, string(OutcomeSubmissionUnknown), outcome)
			require.Equal(t, "submitted", stage)
			require.Equal(t, "seek_submit_unconfirmed", code)
			require.Equal(t, finishedBefore, finished)
			require.Error(t, unresolvedSubmission(database, pageTestUser, job.ID, job.URL, ""), "the badge cannot reconcile or clear an uncertain submission")
			var applied, attempts int
			require.NoError(t, database.QueryRow("SELECT count(*) FROM jobs_applied WHERE user_id=?", pageTestUser).Scan(&applied))
			require.Zero(t, applied)
			require.NoError(t, database.QueryRow("SELECT count(*) FROM apply_attempts WHERE user_id=?", pageTestUser).Scan(&attempts))
			require.Equal(t, 1, attempts)
		})
	}
}

func TestSeekObservedAppliedBadgePreservesVerifiedHistory(t *testing.T) {
	for _, cardBadge := range []bool{true, false} {
		t.Run(map[bool]string{true: "card", false: "page discovery"}[cardBadge], func(t *testing.T) {
			database := pageHookDB(t)
			callbacks := 0
			b := New(Config{DB: database, UserID: pageTestUser, Platform: domain.PlatformSeek, OnApplied: func() { callbacks++ }})
			job := seekJob{ID: "96103223", URL: "https://www.seek.com.au/job/96103223", Title: "Engineer", Company: "Acme", AlreadyApplied: cardBadge}
			const date, resume, cover, refs = "2025-01-02T03:04:05Z", "prior-resume-version", "prior-cover-version", "preserved-approved-document-references"
			_, err := database.Exec("INSERT INTO jobs_applied(id,user_id,platform,company,role,link,resume_content_version_id,cover_letter_content_version_id,document_refs_json,applied_at) VALUES(?,?,?,?,?,?,?,?,?,?)", job.ID, pageTestUser, string(domain.PlatformSeek), job.Company, job.Title, job.URL, resume, cover, refs, date)
			require.NoError(t, err)
			before := b.countAppliedToday()
			require.False(t, b.processSeekJobEvaluation(context.Background(), nil, job, false), "known verified history exits before browser access")
			require.Equal(t, before, b.countAppliedToday())
			require.Zero(t, callbacks)
			var gotDate, gotResume, gotCover, gotRefs string
			require.NoError(t, database.QueryRow("SELECT applied_at,resume_content_version_id,cover_letter_content_version_id,document_refs_json FROM jobs_applied WHERE user_id=? AND id=?", pageTestUser, job.ID).Scan(&gotDate, &gotResume, &gotCover, &gotRefs))
			require.Equal(t, date, gotDate)
			require.Equal(t, resume, gotResume)
			require.Equal(t, cover, gotCover)
			require.Equal(t, refs, gotRefs)
		})
	}
}

// Legacy/manual history may use an internal ID while discovery uses SEEK's ID.
// A new badge must not create a duplicate current-day application for that URL.
func TestSeekObservedAppliedBadgePreservesLegacyLinkedHistory(t *testing.T) {
	for _, source := range []string{"card", "verified page"} {
		t.Run(source, func(t *testing.T) {
			database := pageHookDB(t)
			manager := pageHookManager(t, database)
			callbacks := 0
			b := New(Config{DB: database, UserID: pageTestUser, Platform: domain.PlatformSeek, JobPageReader: manager.pageReader, JobPageStore: manager.pageStore, OnApplied: func() { callbacks++ }})
			job := seekJob{ID: "96103223", URL: "https://www.seek.com.au/job/96103223", Title: "Engineer", Company: "Acme", AlreadyApplied: source == "card"}
			const internalID, date, resume, cover, refs = "legacy-internal-job", "2025-01-02T03:04:05Z", "prior-resume-version", "prior-cover-version", "preserved-approved-document-references"
			_, err := database.Exec("INSERT INTO jobs_applied(id,user_id,platform,company,role,link,resume_content_version_id,cover_letter_content_version_id,document_refs_json,applied_at) VALUES(?,?,?,?,?,?,?,?,?,?)", internalID, pageTestUser, string(domain.PlatformSeek), job.Company, job.Title, job.URL, resume, cover, refs, date)
			require.NoError(t, err)
			var browser *rod.Browser
			if source == "verified page" {
				browser = workerFixtureBrowser(t, func(string) string { return seekObservedBadgeHTML(job.ID) })
			}
			before := b.countAppliedToday()
			require.False(t, b.processSeekJobEvaluation(t.Context(), browser, job, false))
			require.Equal(t, before, b.countAppliedToday())
			require.Zero(t, callbacks)
			var applied int
			require.NoError(t, database.QueryRow("SELECT count(*) FROM jobs_applied WHERE user_id=?", pageTestUser).Scan(&applied))
			require.Equal(t, 1, applied, "a discovered badge cannot duplicate legacy linked history")
			var gotDate, gotResume, gotCover, gotRefs string
			require.NoError(t, database.QueryRow("SELECT applied_at,resume_content_version_id,cover_letter_content_version_id,document_refs_json FROM jobs_applied WHERE user_id=? AND id=?", pageTestUser, internalID).Scan(&gotDate, &gotResume, &gotCover, &gotRefs))
			require.Equal(t, date, gotDate)
			require.Equal(t, resume, gotResume)
			require.Equal(t, cover, gotCover)
			require.Equal(t, refs, gotRefs)
			var reason string
			require.NoError(t, database.QueryRow("SELECT skip_reason FROM jobs_skipped WHERE user_id=? AND id=?", pageTestUser, job.ID).Scan(&reason))
			require.Equal(t, "already_applied_indicator", reason)
		})
	}
}
