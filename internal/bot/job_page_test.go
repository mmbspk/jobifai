package bot

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/jobpage"
	"github.com/user/jobifai/internal/quota"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const pageTestUser = "page-hook-user"

func pageHookDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testDB(t)
	_, err := db.Exec("INSERT INTO users(id,email,password_hash,display_name,is_admin) VALUES(?,?,'test','Reader',1)", pageTestUser, pageTestUser+"@example.test")
	require.NoError(t, err)
	return db
}
func pageHookManager(t *testing.T, db *sql.DB) *Manager {
	t.Helper()
	settings := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "page-test-key")
	require.NoError(t, settings.Set(pageTestUser, "resume_profile", domain.ResumeProfile{PersonalInformation: domain.PersonalInformation{Name: "Reader", Email: "reader@example.test"}}))
	m := NewManager(t.Context(), db, settings, secrets, browser.NewSessionStore(db, secrets), nil, nil, nil, nil, "")
	m.SetJobPageReading(jobpage.NewSQLStore(db))
	return m
}
func pageHookHTML(platform, id, description string) string {
	desc := ""
	if description != "" {
		if platform == "seek" {
			desc = "<section data-automation='jobAdDetails'>" + description + "</section>"
		} else {
			desc = "<section class='jobs-description'><div class='jobs-description-content__text'>" + description + "</div></section>"
		}
	}
	company := "<a data-automation='advertiser-name'>Acme</a>"
	button := "Quick Apply"
	if platform == "linkedin" {
		company = "<div class='job-details-jobs-unified-top-card__company-name'><a href='/company/acme'>Acme</a></div>"
		button = "Easy Apply"
	}
	return fmt.Sprintf("<!doctype html><html><head><title>Engineer | Acme</title></head><body><main data-job-id='%s'><h1 data-automation='job-detail-title'>Engineer</h1>%s%s<button onclick=\"sessionStorage.setItem('read-only-apply-click','true')\">%s</button></main></body></html>", id, company, desc, button)
}

func TestJobPageProductionHookCanonicalBoards(t *testing.T) {
	description := strings.Repeat("Develop reliable services, review code, and operate production systems. ", 8)
	db := pageHookDB(t)
	m := pageHookManager(t, db)
	br := workerFixtureBrowser(t, func(raw string) string {
		platform := "linkedin"
		id := linkedInExpectedID(raw)
		if strings.Contains(raw, "seek") {
			platform = "seek"
			id = seekJobIDFromURL(raw)
		}
		return pageHookHTML(platform, id, description)
	})
	b := New(Config{UserID: pageTestUser, JobPageReader: m.pageReader, JobPageStore: m.pageStore})
	for _, board := range []struct {
		platform domain.Platform
		url      string
	}{{domain.PlatformLinkedIn, "https://www.linkedin.com/jobs/view/12345678"}, {domain.PlatformSeek, "https://au.seek.com/job/22345678"}, {domain.PlatformSeek, "https://www.seek.com.au/job/22345679"}} {
		t.Run(board.url, func(t *testing.T) {
			page := navigateAndWait(t, br, board.url)
			result, err := b.readJobPage(t.Context(), page, board.platform, linkedInJob{URL: board.url, Title: "Engineer", Company: "Acme"})
			require.NoError(t, err)
			require.Equal(t, jobpage.SourceDeterministic, result.Source)
			require.Equal(t, "Engineer", result.Identity.Title)
			require.Equal(t, "Acme", result.Identity.Company)
			require.Contains(t, result.Description, "Develop reliable services")
			require.Equal(t, 0, result.Metrics.HTTPAttempts)
			clicked, err := page.Eval("()=>sessionStorage.getItem('read-only-apply-click')==='true'")
			require.NoError(t, err)
			require.False(t, clicked.Value.Bool())
		})
	}
}

func TestJobPageManagerReadAndGenerateUseCurrentListing(t *testing.T) {
	db := pageHookDB(t)
	m := pageHookManager(t, db)
	description := strings.Repeat("Current listing duties and requirements. ", 10)
	br := workerFixtureBrowser(t, func(raw string) string { return pageHookHTML("seek", seekJobIDFromURL(raw), description) })
	m.seekBrowsers[pageTestUser] = br
	result, err := m.ReadJobPage(t.Context(), pageTestUser, "https://au.seek.com/job/22345678")
	require.NoError(t, err)
	require.Equal(t, jobpage.SourceDeterministic, result.Source)
	require.Equal(t, "22345678", result.Identity.JobID)
	text, err := m.FetchJobPage(t.Context(), pageTestUser, "https://au.seek.com/job/22345679")
	require.NoError(t, err)
	require.Contains(t, text, "Job title: Engineer\nCompany: Acme\n\n")
	require.NotContains(t, text, "\\n")
	var events int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM job_page_read_events WHERE user_id=?", pageTestUser).Scan(&events))
	require.Equal(t, 2, events)
}

type pageQuotaMeter struct {
	before, records atomic.Int32
	lastRun         atomic.Value
	reject          bool
}

func (s *pageQuotaMeter) BeforeBrowserIncrement(context.Context, string) error {
	s.before.Add(1)
	if s.reject {
		return quota.ErrExceeded
	}
	return nil
}
func (s *pageQuotaMeter) RecordBrowserIncrement(_ context.Context, _ string, _ string, runID string, _ string, _ int64) error {
	s.records.Add(1)
	s.lastRun.Store(runID)
	return nil
}
func (*pageQuotaMeter) BrowserIncrementDuration() (time.Duration, error) {
	return 30 * time.Second, nil
}

func TestJobPageQuotaPreventsBrowserCreation(t *testing.T) {
	db := pageHookDB(t)
	m := pageHookManager(t, db)
	meter := &pageQuotaMeter{reject: true}
	m.SetBrowserMeter(meter)
	// A regression that consults the browser first would dereference the absent
	// session store. The prepaid gate must precede browser initialization.
	m.sessions = nil
	_, err := m.ReadJobPage(t.Context(), pageTestUser, "https://au.seek.com/job/22345678")
	require.ErrorIs(t, err, quota.ErrExceeded)
	require.Equal(t, int32(1), meter.before.Load())
	require.Zero(t, meter.records.Load())
	require.Empty(t, m.seekBrowsers)
	require.Empty(t, m.linkedInBrowsers)
	_, err = m.FetchJobPage(t.Context(), pageTestUser, "https://www.linkedin.com/jobs/view/12345678")
	require.ErrorIs(t, err, quota.ErrExceeded)
	require.Empty(t, m.seekBrowsers)
	require.Empty(t, m.linkedInBrowsers)
}

func TestJobPageSubmitQuotaPreventsBrowserInitialization(t *testing.T) {
	db := pageHookDB(t)
	m := pageHookManager(t, db)
	meter := &pageQuotaMeter{reject: true}
	m.SetBrowserMeter(meter)
	err := m.runSubmit(t.Context(), pageTestUser, SubmitRequest{JobID: "submit-quota", Platform: "seek", Link: "https://au.seek.com/job/22345678", Role: "Engineer", Company: "Acme"})
	require.ErrorIs(t, err, quota.ErrExceeded)
	require.Equal(t, int32(1), meter.before.Load())
	require.Zero(t, meter.records.Load())
	require.Empty(t, m.seekBrowsers)
	require.Empty(t, m.linkedInBrowsers)
	var stage, outcome string
	require.NoError(t, db.QueryRow("SELECT stage,outcome FROM apply_attempts WHERE job_id='submit-quota' AND user_id=? ORDER BY started_at DESC LIMIT 1", pageTestUser).Scan(&stage, &outcome))
	require.Equal(t, "billing", stage)
	require.Equal(t, string(OutcomeBlocked), outcome)
}

func TestJobPagePreparationPersistsTypedReadHoldWithoutApplying(t *testing.T) {
	db := pageHookDB(t)
	m := pageHookManager(t, db)
	m.SetDocuments(&documents.Service{Store: documents.NewStore(db)})
	meter := &pageQuotaMeter{}
	m.SetBrowserMeter(meter)
	br := workerFixtureBrowser(t, func(raw string) string { return pageHookHTML("seek", seekJobIDFromURL(raw), "") })
	m.seekBrowsers[pageTestUser] = br
	packJSON := documents.WriteApplicationPackJSON(documents.ApplicationDocumentPack{Selection: documents.ApplicationDocumentSelection{ResumeUseSite: true, CoverSkip: true}})
	_, err := db.Exec("INSERT INTO jobs_pending_review(job_id,user_id,company,role,platform,link,created_at,prepare_status,document_refs_json) VALUES('prepared-job',?,'Acme','Engineer','seek','https://au.seek.com/job/22345678',CURRENT_TIMESTAMP,'running:reader-prep',?)", pageTestUser, packJSON)
	require.NoError(t, err)
	err = m.PrepareReviewDocuments(t.Context(), pageTestUser, "prepared-job", "reader-prep")
	require.Error(t, err)
	require.Equal(t, jobpage.FailureMissing, jobpage.ErrorCode(err))
	require.Equal(t, int32(1), meter.records.Load())
	require.Equal(t, "prepare-reader-prep", meter.lastRun.Load())
	var stored string
	require.NoError(t, db.QueryRow("SELECT document_refs_json FROM jobs_pending_review WHERE job_id='prepared-job' AND user_id=?", pageTestUser).Scan(&stored))
	pack := documents.ParseApplicationPackJSON(stored)
	require.Equal(t, "listing_read", pack.HoldStage)
	require.NotEmpty(t, pack.HoldReason)
	require.True(t, pack.Selection.ResumeUseSite)
	require.True(t, pack.Selection.CoverSkip)
	probe := navigateAndWait(t, br, "https://au.seek.com/job/22345678")
	clicked, err := probe.Eval("()=>sessionStorage.getItem('read-only-apply-click')==='true'")
	require.NoError(t, err)
	require.False(t, clicked.Value.Bool())
}

func TestJobPageManagerLazyPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	b := New(Config{JobPageReader: jobpage.NewReader(nil, nil, nil)})
	lazy, err := managerLazyWithReadError(b, ctx, SubmitRequest{Platform: "seek", Link: "https://au.seek.com/job/22345678", Role: "Engineer", Company: "Acme"}, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "listing_read", lazy.holdStage)
	code, _ := listingFailureMessage(err)
	require.Equal(t, string(jobpage.FailureCanceled), code)
}
func TestJobPageBrowserLockWaitHonorsCancellation(t *testing.T) {
	m := &Manager{}
	m.browserMu.Lock()
	defer m.browserMu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := m.getSeekBrowser(pageTestUser, domain.GeneralSettings{}, ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestJobPageReadDoesNotKillAnActiveApplicationProfile(t *testing.T) {
	profile := t.TempDir()
	launch := launcher.New().Headless(true).UserDataDir(profile).NoSandbox(os.Getenv("CI") != "")
	if bin := os.Getenv("ROD_BROWSER_BIN"); bin != "" {
		launch = launch.Bin(bin)
	}
	endpoint, err := launch.Launch()
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("Chromium required: %v", err)
		}
		t.Skipf("Chromium unavailable: %v", err)
	}
	active := rod.New().ControlURL(endpoint)
	require.NoError(t, active.Connect())
	t.Cleanup(func() { _ = active.Close(); launch.Cleanup() })
	require.True(t, browser.ProfileInUse(profile))
	db := pageHookDB(t)
	manager := pageHookManager(t, db)
	settings := manager.cfgStore.(*config.Store)
	general := domain.GeneralSettings{}
	general.Browser.ChromeProfilePath = profile
	require.NoError(t, settings.Set(domain.SystemUserID, "general_settings", general))
	for _, board := range []struct{ platform, url string }{
		{"seek", "https://au.seek.com/job/22345678"},
		{"linkedin", "https://www.linkedin.com/jobs/view/12345678"},
	} {
		t.Run(board.platform, func(t *testing.T) {
			require.NoError(t, manager.sessions.Save(pageTestUser, board.platform, "fixture", []byte("[]")))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err := manager.ReadJobPage(ctx, pageTestUser, board.url)
			require.ErrorContains(t, err, "profile is already active")
			require.Equal(t, jobpage.FailureBrowser, jobpage.ErrorCode(err))
			_, err = active.Pages()
			require.NoError(t, err, "read-only page loading must preserve the active browser")
			require.True(t, browser.ProfileInUse(profile))
		})
	}
	require.Empty(t, manager.seekBrowsers)
	require.Empty(t, manager.linkedInBrowsers)
}
