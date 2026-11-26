package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/applicationanswers"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

func TestApplicationAnswerManagerDeferralsPreserveOperationalRetryBudget(t *testing.T) {
	_, endpoint := launchTestBrowserEndpoint(t)
	parsedEndpoint, err := url.Parse(endpoint)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsedEndpoint.Port())
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body>
<main><h1>Coordinator</h1><p>Fixture</p>
<button data-automation="job-detail-apply" onclick="this.remove();document.getElementById('apply').style.display='block'">Quick Apply</button>
<form id="apply" role="dialog" data-testid="quick-apply-form" style="display:none;padding:20px">
  <div data-test-form-element><label for="notice">Notice period</label><input id="notice" required></div>
  <button type="button" data-testid="continue-button">Continue</button>
</form></main></body></html>`)
	}))
	t.Cleanup(server.Close)
	database := openMappingDB(t)
	_, err = database.Exec("INSERT INTO users(id,email,password_hash) VALUES('candidate','candidate@fixture.test','fixture')")
	require.NoError(t, err)
	cfg := config.NewStore(database)
	secrets := config.NewSecretsStore(database, "fixture-key")
	sessions := browser.NewSessionStore(database, secrets)
	require.NoError(t, cfg.Set(domain.SystemUserID, config.KeyGeneralSettings, domain.GeneralSettings{
		Browser:       domain.BrowserConfig{RemoteDebugPort: port},
		HumanBehavior: domain.HumanBehaviorConfig{InteractionPauseMin: 0.01, InteractionPauseMax: 0.02, PauseBetweenJobsMin: 1, PauseBetweenJobsMax: 2},
	}))
	profile := basicProfile()
	profile.ApplicationDefaults.NoticePeriod = ""
	profile.ApplicationDefaults.Availability = ""
	profile.ApplicationDefaults.FormAnswers = nil
	require.NoError(t, cfg.Set("candidate", "resume_profile", profile))
	const jobID = "123456789"
	jobURL := server.URL + "/job/" + jobID
	_, err = database.Exec(`INSERT INTO jobs_pending_review(job_id,user_id,company,role,platform,link,attempt_count,resume_path,cover_letter_path,suitability_score) VALUES(?,'candidate','Fixture','Coordinator','seek',?,1,'/fixture/resume.pdf','/fixture/cover.pdf',8)`, jobID, jobURL)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	m := NewManager(ctx, database, cfg, secrets, sessions, nil, nil, nil, nil, "")
	m.SetApplicationAnswers(applicationanswers.NewService(database, cfg))
	req := SubmitRequest{JobID: jobID, Link: jobURL, Platform: "seek", Company: "Fixture", Role: "Coordinator", ResumePath: "/fixture/resume.pdf", CoverPath: "/fixture/cover.pdf"}

	for i := 0; i < 3; i++ {
		err = m.runSubmit(ctx, "candidate", req)
		require.ErrorIs(t, err, errAdaptiveQuestionReview)
		require.True(t, isCandidateDeferral(err))
		var attempts int
		var resumePath, coverPath string
		require.NoError(t, database.QueryRow(`SELECT attempt_count,resume_path,cover_letter_path FROM jobs_pending_review WHERE job_id=? AND user_id='candidate'`, jobID).Scan(&attempts, &resumePath, &coverPath))
		require.Equal(t, 1, attempts, "candidate facts do not consume the remaining operational retry")
		require.Equal(t, req.ResumePath, resumePath)
		require.Equal(t, req.CoverPath, coverPath)
	}
	var deferrals int
	require.NoError(t, database.QueryRow(`SELECT COUNT(*) FROM apply_attempts WHERE user_id='candidate' AND job_id=? AND outcome='blocked' AND reason_code='answer_confirmation'`, jobID).Scan(&deferrals))
	require.Equal(t, 3, deferrals)
	var failureCode string
	require.NoError(t, database.QueryRow(`SELECT failure_code FROM jobs_skipped WHERE id=? AND user_id='candidate'`, jobID).Scan(&failureCode))
	require.Equal(t, "answer_confirmation", failureCode)

	// A review-store outage is an operational failure even when encountered
	// while trying to record a missing candidate fact. It consumes one retry.
	outage := errors.New("fixture answer review store unavailable")
	m.SetApplicationAnswers(&answerRecorderStub{recordErr: outage})
	err = m.runSubmit(ctx, "candidate", req)
	require.ErrorIs(t, err, outage)
	require.False(t, isCandidateDeferral(err))
	var attempts int
	require.NoError(t, database.QueryRow(`SELECT attempt_count FROM jobs_pending_review WHERE job_id=? AND user_id='candidate'`, jobID).Scan(&attempts))
	require.Equal(t, 2, attempts)
	var applied int
	require.NoError(t, database.QueryRow(`SELECT COUNT(*) FROM jobs_applied WHERE user_id='candidate'`).Scan(&applied))
	require.Zero(t, applied)
}
