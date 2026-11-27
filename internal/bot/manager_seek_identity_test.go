package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

func seekURLApplyFixture(t *testing.T, badge bool) *Manager {
	t.Helper()
	database := pageHookDB(t)
	manager := pageHookManager(t, database)
	settings := config.NewStore(database)
	manager.scorer = &capturingScorer{}
	require.NoError(t, settings.Set(pageTestUser, "resume_profile", basicProfile()))
	require.NoError(t, settings.Set(domain.SystemUserID, "general_settings", domain.GeneralSettings{HumanBehavior: domain.HumanBehaviorConfig{PauseBetweenJobsMin: 1, PauseBetweenJobsMax: 2}}))
	require.NoError(t, settings.Set(domain.SystemUserID, keyApplyEngineConfig, domain.ApplyEngineConfig{Mode: domain.ApplyEngineAdaptive}))
	manager.userTypeChecker = func(string) (bool, bool) { return true, false }
	manager.seekBrowsers[pageTestUser] = workerFixtureBrowser(t, func(raw string) string {
		page := seekWorkerHTML(raw)
		if strings.Contains(raw, "/apply") {
			page = strings.Replace(page, "<label for=\"notice\">How soon can you leave?</label><input id=\"notice\">", "", 1)
			page = strings.Replace(page, "localStorage.setItem('lastNotice',document.getElementById('notice').value);", "", 1)
			return strings.Replace(page, "form.innerHTML='<h1 data-automation=\"application-success-title\">Application submitted</h1>'", "history.pushState({},'', '/job/82345680/apply/success');form.innerHTML='"+seekSentBannerFixture+"'", 1)
		}
		page = strings.Replace(page, "<body>", "<body><main data-job-id='82345680'>", 1)
		page = strings.Replace(page, "</body>", "</main></body>", 1)
		description := "<section data-automation='jobAdDetails'>" + strings.Repeat("Coordinate reliable services and maintain production applications. ", 10) + "</section>"
		page = strings.Replace(page, "</main>", description+"</main>", 1)
		if badge {
			page = strings.Replace(page, "</main>", "<div id='applied-date-message'>Applied previously</div></main>", 1)
		}
		return page
	})
	return manager
}

func TestApplyFromURL_SeekNativeKeepsListingIdentityThroughConfirmation(t *testing.T) {
	manager := seekURLApplyFixture(t, false)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	result, err := manager.ApplyFromURL(ctx, pageTestUser, "https://au.seek.com/job/82345680", "", false)
	require.NoError(t, err)
	require.Equal(t, "82345680", result.JobID)
	var confirmed, applied int
	require.NoError(t, manager.db.QueryRow("SELECT count(*) FROM apply_attempts WHERE user_id=? AND job_id=? AND outcome='submitted_confirmed'", pageTestUser, result.JobID).Scan(&confirmed))
	require.Equal(t, 1, confirmed)
	require.NoError(t, manager.db.QueryRow("SELECT count(*) FROM jobs_applied WHERE user_id=? AND id=?", pageTestUser, result.JobID).Scan(&applied))
	require.Equal(t, 1, applied)
	probe := navigateAndWait(t, manager.seekBrowsers[pageTestUser], "https://au.seek.com/job/82345680")
	state, err := probe.Eval("()=>localStorage.getItem('submits')")
	require.NoError(t, err)
	require.Equal(t, "1", state.Value.String())
}

func TestApplyFromURL_SeekObservedBadgeCannotInventSubmissionOrClearUnknown(t *testing.T) {
	manager := seekURLApplyFixture(t, true)
	previous := newAttemptContext(pageTestUser, "seek", "legacy-record", "https://au.seek.com/job/82345680", "Coordinator", "Acme Inc.", "fixture", "")
	beginAttempt(manager.db, previous)
	finishAttempt(manager.db, previous, OutcomeSubmissionUnknown, "submitted", "seek_submit_unconfirmed", "No verified result")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, err := manager.ApplyFromURL(ctx, pageTestUser, previous.URL, "", false)
	require.ErrorIs(t, err, ErrAlreadyApplied)
	var applied int
	require.NoError(t, manager.db.QueryRow("SELECT count(*) FROM jobs_applied WHERE user_id=?", pageTestUser).Scan(&applied))
	require.Zero(t, applied)
	var outcome, reason string
	require.NoError(t, manager.db.QueryRow("SELECT outcome FROM apply_attempts WHERE id=?", previous.AttemptID).Scan(&outcome))
	require.Equal(t, string(OutcomeSubmissionUnknown), outcome)
	require.NoError(t, manager.db.QueryRow("SELECT skip_reason FROM jobs_skipped WHERE user_id=? AND id='82345680'", pageTestUser).Scan(&reason))
	require.Equal(t, "already_applied_indicator", reason)
	require.Error(t, unresolvedSubmission(manager.db, pageTestUser, "82345680", previous.URL, ""))
}
