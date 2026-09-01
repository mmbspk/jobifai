package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/resume"
	"github.com/user/jobifai/internal/scraper"
)

// ConfigReader is the subset of config.Store the Manager needs.
type ConfigReader interface {
	Get(userID, key string, dst any) error
}

// SecretsReader is the subset of config.SecretsStore the Manager needs.
type SecretsReader interface {
	Get(userID, key string) (string, error)
}

type botEntry struct {
	bot    *Bot
	cancel context.CancelFunc
	status domain.BotStatus
}

// Manager is the long-lived bot controller exposed to HTTP handlers.
// It supports concurrent bots, one per user, and satisfies handler.BotController.
type Manager struct {
	mu           sync.Mutex
	bots         map[string]*botEntry // key = userID
	db           *sql.DB
	cfgStore     ConfigReader
	secrets      SecretsReader
	sessions     *browser.SessionStore
	tailor       ResumeTailor
	scorer       JobScorer
	halal        JobHalalChecker
	renderer     ResumeRenderer
	marketDir    string
	ctx          context.Context // lifetime context; cancelled on server shutdown
	seekBrowsers     map[string]*rod.Browser // persistent Seek browser per user
	linkedInBrowsers map[string]*rod.Browser // persistent LinkedIn browser per user
	browserMu        sync.Mutex              // protects seekBrowsers + linkedInBrowsers
	applyMu          sync.Mutex              // protects applyInProgress
	applyInProgress  map[string]bool         // userID → AI Apply call in flight
}

func NewManager(
	ctx context.Context,
	db *sql.DB,
	cfgStore ConfigReader,
	secrets SecretsReader,
	sessions *browser.SessionStore,
	tailor ResumeTailor,
	scorer JobScorer,
	halal JobHalalChecker,
	renderer ResumeRenderer,
	marketDir string,
) *Manager {
	return &Manager{
		ctx:          ctx,
		db:           db,
		cfgStore:     cfgStore,
		secrets:      secrets,
		sessions:     sessions,
		tailor:       tailor,
		scorer:       scorer,
		halal:        halal,
		renderer:     renderer,
		marketDir:    marketDir,
		bots:             make(map[string]*botEntry),
		seekBrowsers:     make(map[string]*rod.Browser),
		linkedInBrowsers: make(map[string]*rod.Browser),
		applyInProgress:  make(map[string]bool),
	}
}

func (m *Manager) entry(userID string) *botEntry {
	if e, ok := m.bots[userID]; ok {
		return e
	}
	e := &botEntry{status: domain.BotStatus{State: domain.BotStateIdle}}
	m.bots[userID] = e
	return e
}

// Start builds the bot config from DB and launches the loop for userID.
func (m *Manager) Start(ctx context.Context, userID string, platform domain.Platform) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	e := m.entry(userID)
	if e.status.State == domain.BotStateRunning {
		return errors.New("bot is already running")
	}

	cfg, err := m.buildConfig(userID, platform)
	if err != nil {
		return err
	}

	botCtx, cancel := context.WithCancel(m.ctx)
	e.cancel = cancel
	e.bot = New(*cfg)
	now := time.Now()
	location := ""
	if targets := cfg.Preferences.EffectiveSearchTargets(); len(targets) > 0 {
		location = targets[0].Location
	}
	e.status = domain.BotStatus{
		State:      domain.BotStateRunning,
		Platform:   platform,
		Location:   location,
		StartedAt:  &now,
		DailyLimit: cfg.Settings.HumanBehavior.DailyApplicationLimit,
	}

	go func() {
		_ = e.bot.Start(botCtx)
		cancel()
		m.mu.Lock()
		fin := time.Now()
		e.status.State = e.bot.State()
		e.status.FinishedAt = &fin
		m.mu.Unlock()
	}()

	return nil
}

// Stop signals the running bot to stop for userID.
func (m *Manager) Stop(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.bots[userID]
	if !ok {
		return
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.bot != nil {
		e.bot.Stop()
	}
	fin := time.Now()
	e.status.State = domain.BotStateStopped
	e.status.FinishedAt = &fin
}

// Pause suspends the bot at the next job boundary for userID.
func (m *Manager) Pause(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.bots[userID]
	if !ok || e.bot == nil {
		return
	}
	e.bot.Pause()
	e.status.State = domain.BotStatePaused
}

// Resume unblocks a paused bot for userID.
func (m *Manager) Resume(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.bots[userID]
	if !ok || e.bot == nil {
		return
	}
	e.bot.Resume()
	e.status.State = domain.BotStateRunning
}

// Status returns the current bot status for userID.
func (m *Manager) Status(userID string) domain.BotStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entry(userID)
	if m.db != nil {
		var n int
		_ = m.db.QueryRowContext(m.ctx,
			"SELECT COUNT(*) FROM jobs_applied WHERE user_id = ? AND date(applied_at) = date('now')", userID,
		).Scan(&n)
		e.status.TodayCount = n
	}
	if e.status.State == domain.BotStateIdle && m.cfgStore != nil {
		var gs domain.GeneralSettings
		if err := m.cfgStore.Get(userID, "general_settings", &gs); err == nil {
			e.status.DailyLimit = gs.HumanBehavior.DailyApplicationLimit
		}
	}
	s := e.status
	if e.bot != nil {
		s.Keyword = e.bot.Keyword()
		if e.bot.IsPaused() {
			s.State = domain.BotStatePaused
		}
	}
	return s
}

// SubmitNow immediately submits a job from the approved queue for userID.
func (m *Manager) SubmitNow(userID string, req SubmitRequest) {
	go func() { _ = m.runSubmit(m.ctx, userID, req) }()
}

// SubmitSync runs the apply flow synchronously and returns the outcome error.
// Used by the "Apply Anyway" path so the HTTP response reflects the real result.
func (m *Manager) SubmitSync(ctx context.Context, userID string, req SubmitRequest) error {
	return m.runSubmit(ctx, userID, req)
}

// runSubmit executes the full apply flow for a queued job and records the result.
// Called directly by SubmitSync and inside a goroutine by SubmitNow.
func (m *Manager) runSubmit(ctx context.Context, userID string, req SubmitRequest) error {
	b, gs, err := m.setupBot(userID, domain.Platform(req.Platform), "")
	if err != nil {
		log.Error().Err(err).Str("job", req.Role).Msg("approve: setup bot")
		return err
	}

	// Reject immediately if this job has already been attempted twice.
	var attemptCount int
	_ = m.db.QueryRow(
		`SELECT COALESCE(attempt_count,0) FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
		req.JobID, userID,
	).Scan(&attemptCount)
	if attemptCount >= 2 {
		var earlyScore int
		var earlyReason string
		_ = m.db.QueryRow(
			`SELECT COALESCE(suitability_score,0), COALESCE(suitability_reasoning,'')
			 FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
			req.JobID, userID,
		).Scan(&earlyScore, &earlyReason)
		earlyLabel := "easy apply"
		if req.Platform == "seek" {
			earlyLabel = "quick apply"
		}
		_, _ = m.db.Exec(
			`INSERT OR IGNORE INTO jobs_skipped(id,user_id,platform,company,role,location,link,skip_reason,suitability_score,suitability_reasoning,viewed_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,datetime('now'))`,
			req.JobID, userID, req.Platform, req.Company, req.Role, req.Location, req.Link,
			earlyLabel+": application attempted twice, could not complete", earlyScore, earlyReason,
		)
		_, _ = m.db.Exec(`DELETE FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`, req.JobID, userID)
		return fmt.Errorf("application has already been attempted twice — it has been moved to Cannot Apply")
	}

	// Seek: reuse a persistent browser per user so auth0 session state (including
	// the post-PKCE cookies from each Quick Apply) is preserved naturally between
	// consecutive approvals — no token-rotation issues.
	// LinkedIn (and remote-debug mode): new browser per job, same as before.
	var br *rod.Browser
	var jobPage *rod.Page
	ownsBr := false
	if req.Platform == "seek" && gs.Browser.RemoteDebugPort == 0 {
		seekBr, brErr := m.getSeekBrowser(userID, gs)
		if brErr != nil {
			log.Error().Err(brErr).Str("job", req.Role).Msg("approve: seek browser")
			return brErr
		}
		p, pageErr := seekBr.Page(proto.TargetCreateTarget{URL: req.Link})
		if pageErr != nil {
			m.InvalidateSeekBrowser(userID)
			log.Error().Err(pageErr).Str("job", req.Role).Msg("approve: open seek job tab")
			return pageErr
		}
		br, jobPage = seekBr, p
	} else {
		newBr, newPage, launchErr := b.launchBrowser(m.ctx)
		if launchErr != nil {
			log.Error().Err(launchErr).Str("job", req.Role).Msg("approve: launch browser")
			return launchErr
		}
		if gs.Browser.RemoteDebugPort == 0 {
			ownsBr = true
		}
		if navErr := newPage.Navigate(req.Link); navErr != nil {
			log.Error().Err(navErr).Str("job", req.Role).Msg("approve: navigate to job")
			newPage.Close()
			if ownsBr {
				newBr.Close()
			}
			return navErr
		}
		br, jobPage = newBr, newPage
	}
	defer jobPage.Close()
	if ownsBr {
		defer br.Close()
	}

	var score int
	var halalVerdict string
	if err := m.db.QueryRow(
		`SELECT COALESCE(suitability_score,0), COALESCE(halal_verdict,'') FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
		req.JobID, userID,
	).Scan(&score, &halalVerdict); err != nil {
		log.Warn().Err(err).Str("job_id", req.JobID).Msg("runSubmit: failed to fetch score from pending review")
	}

	var managerLazy *lazyDocGen
	if req.Platform == "seek" {
		// Read description directly from the already-open jobPage tab — avoids
		// opening a second tab at the same URL just to extract text.
		_ = jobPage.Timeout(30 * time.Second).WaitLoad()
		_ = jobPage.Timeout(5 * time.Second).WaitStable(500 * time.Millisecond)
		jobDesc := req.Role + " at " + req.Company
		if rawHTML, err := jobPage.HTML(); err == nil {
			if d := scraper.ParseHTML(rawHTML); len(strings.TrimSpace(d.Description)) >= 100 {
				jobDesc = d.Description
			}
		}
		managerLazy = &lazyDocGen{b: b, ctx: m.ctx, job: linkedInJob{Company: req.Company, Title: req.Role}, jobDesc: jobDesc, resumeOverride: req.ResumePath, coverOverride: req.CoverPath}
	} else {
		managerDetails := b.fetchJob(m.ctx, linkedInJob{URL: req.Link, Company: req.Company, Title: req.Role})
		managerLazy = &lazyDocGen{b: b, ctx: m.ctx, job: linkedInJob{Company: req.Company, Title: req.Role}, jobDesc: managerDetails.Description, resumeOverride: req.ResumePath, coverOverride: req.CoverPath}
	}

	// Extract location from the job page; fall back to the value stored in pending_review.
	location := extractJobLocation(jobPage, domain.Platform(req.Platform))
	if location == "" {
		location = req.Location
	}

	var applyErr error
	if req.Platform == "seek" {
		applyErr = b.seekApply(m.ctx, jobPage, managerLazy)
	} else {
		applyErr = b.easyApply(m.ctx, jobPage, managerLazy)
	}
	if applyErr != nil {
		if errors.Is(applyErr, errAlreadyApplied) {
			log.Info().Str("company", req.Company).Str("job", req.Role).Msg("approve: already applied, recording ✓")
		} else {
			applyLabel := "easy apply"
			if req.Platform == "seek" {
				applyLabel = "quick apply"
			}
			log.Error().Err(applyErr).Str("company", req.Company).Str("job", req.Role).Msgf("approve: %s failed", applyLabel)
			if _, err := m.db.Exec(
				`INSERT OR IGNORE INTO jobs_skipped(id,user_id,platform,company,role,location,link,skip_reason,suitability_score,suitability_reasoning,viewed_at)
				 VALUES(?,?,?,?,?,?,?,?,?,?,datetime('now'))`,
				req.JobID, userID, req.Platform, req.Company, req.Role, location, req.Link, applyLabel+": "+applyErr.Error(), score, req.SuitabilityReasoning,
			); err != nil {
				log.Error().Err(err).Str("job_id", req.JobID).Msg("runSubmit: failed to record skipped job")
			}
			// Persist any generated doc paths and increment attempt count so the
			// next re-approval reuses the docs instead of re-running the LLM.
			genResume, genCover := managerLazy.peek()
			savedResume := genResume
			if savedResume == "" {
				savedResume = req.ResumePath
			}
			savedCover := genCover
			if savedCover == "" {
				savedCover = req.CoverPath
			}
			_, _ = m.db.Exec(
				`UPDATE jobs_pending_review
				 SET attempt_count = attempt_count + 1,
				     resume_path = CASE WHEN ? != '' THEN ? ELSE resume_path END,
				     cover_letter_path = CASE WHEN ? != '' THEN ? ELSE cover_letter_path END
				 WHERE job_id = ? AND user_id = ?`,
				savedResume, savedResume, savedCover, savedCover, req.JobID, userID,
			)
			return applyErr
		}
	}

	resumePath, coverPath := managerLazy.get()
	if _, err := m.db.Exec(
		`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,suitability_score,halal_verdict,applied_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		req.JobID, userID, req.Platform, req.Company, req.Role, location, req.Link, resumePath, coverPath,
		score, halalVerdict, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		log.Error().Err(err).Str("job_id", req.JobID).Msg("runSubmit: failed to record applied job")
	}
	if _, err := m.db.Exec(`DELETE FROM jobs_pending_review WHERE user_id = ? AND link = ?`, userID, req.Link); err != nil {
		log.Error().Err(err).Str("job_id", req.JobID).Msg("runSubmit: failed to delete from pending review")
	}
	if _, err := m.db.Exec(`DELETE FROM jobs_approved_queue WHERE job_id = ? AND user_id = ?`, req.JobID, userID); err != nil {
		log.Error().Err(err).Str("job_id", req.JobID).Msg("runSubmit: failed to delete from approved queue")
	}
	log.Info().Str("company", req.Company).Str("job", req.Role).Msg("approve: submitted ✓")
	return nil
}

// checkLiveBrowser returns the browser for userID from store if it still responds to
// br.Pages(). If the browser is dead, it is removed from the map and nil is returned.
// Caller must hold m.browserMu.
func (m *Manager) checkLiveBrowser(store map[string]*rod.Browser, userID string) *rod.Browser {
	br, ok := store[userID]
	if !ok {
		return nil
	}
	if _, err := br.Pages(); err != nil {
		delete(store, userID)
		return nil
	}
	return br
}

// getSeekBrowser returns the persistent Seek browser for userID, creating it if needed.
// The browser is kept alive across consecutive SubmitNow calls so auth0 session state
// (including post-PKCE cookies) is preserved naturally — no token rotation issues.
func (m *Manager) getSeekBrowser(userID string, gs domain.GeneralSettings) (*rod.Browser, error) {
	m.browserMu.Lock()
	defer m.browserMu.Unlock()

	if br := m.checkLiveBrowser(m.seekBrowsers, userID); br != nil {
		return br, nil
	}
	log.Info().Str("user_id", userID).Msg("seek: persistent browser was dead or missing, recreating")

	cookies, err := m.sessions.Load(userID, "seek")
	if err != nil {
		return nil, errors.New("no saved Seek session, log in via Settings → Secrets first")
	}

	dir := browser.ProfileDir(userID, "seek", gs.Browser.ChromeProfilePath)
	browser.PrepareChromeProfileDir(dir)
	l := launcher.New().Headless(!gs.Browser.ShowBrowser).UserDataDir(dir)
	wsURL, launchErr := l.Launch()
	if launchErr != nil && strings.Contains(launchErr.Error(), "SingletonLock") {
		log.Warn().Err(launchErr).Str("dir", dir).Msg("seek: profile locked, force-unlocking and retrying")
		if old := m.seekBrowsers[userID]; old != nil {
			_ = old.Close()
			delete(m.seekBrowsers, userID)
		}
		browser.ForceUnlockChromeProfile(dir)
		wsURL, launchErr = launcher.New().Headless(!gs.Browser.ShowBrowser).UserDataDir(dir).Launch()
	}
	if launchErr != nil {
		return nil, errors.New("launch chrome: " + launchErr.Error())
	}
	br := rod.New().ControlURL(wsURL)
	if connErr := br.Connect(); connErr != nil {
		return nil, errors.New("connect chrome: " + connErr.Error())
	}

	warmPage, pageErr := br.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if pageErr != nil {
		_ = br.Close()
		return nil, errors.New("open warm-up page: " + pageErr.Error())
	}
	if len(cookies) > 0 {
		_ = warmPage.SetCookies(browser.ToCookieParams(cookies))
	}
	if navErr := warmPage.Navigate("https://au.seek.com/jobs"); navErr != nil {
		_ = br.Close()
		return nil, errors.New("seek warm-up navigate: " + navErr.Error())
	}
	_ = warmPage.Timeout(30 * time.Second).WaitLoad()
	_ = warmPage.Timeout(5 * time.Second).WaitStable(2 * time.Second)

	// Recover with stored credentials when warm-up lands logged out.
	if state, _ := browser.PageLoginState(warmPage, "seek"); state == browser.LoginStateNo {
		tmpBot, _, setupErr := m.setupBot(userID, domain.PlatformSeek, "")
		if setupErr == nil && tmpBot.cfg.SeekEmail != "" {
			if recoverErr := tmpBot.seekEnsureLoggedIn(warmPage); recoverErr != nil {
				log.Warn().Err(recoverErr).Str("user_id", userID).Msg("seek: warm-up auto-login failed")
			}
		}
	}

	// Persist warm-up rotated tokens so the next server restart loads a valid
	// (unconsumed) refresh token — getSeekBrowser rotates on navigate but unlike
	// launchBrowser it must save explicitly here.
	m.saveBrowserCookies(warmPage, userID, "seek")

	_ = warmPage.Close()

	m.seekBrowsers[userID] = br
	log.Info().Str("user_id", userID).Msg("seek: persistent browser initialized")
	return br, nil
}

// InvalidateSeekBrowser closes and removes the persistent Seek browser for userID.
// Should be called when the user's Seek session is deleted so the next approval
// starts fresh with the new session cookies.
func (m *Manager) InvalidateSeekBrowser(userID string) {
	m.browserMu.Lock()
	defer m.browserMu.Unlock()
	if br, ok := m.seekBrowsers[userID]; ok {
		_ = br.Close()
		delete(m.seekBrowsers, userID)
		log.Info().Str("user_id", userID).Msg("seek: persistent browser closed")
	}
}

// saveBrowserCookies marshals all cookies from page and persists them for userID/platform.
// Skips persistence when the page is not confirmed logged in, so a guest jar cannot
// overwrite a previously-valid session.
func (m *Manager) saveBrowserCookies(page *rod.Page, userID, platform string) {
	if m.sessions == nil {
		return
	}
	state, stateErr := browser.PageLoginState(page, platform)
	if !browser.ShouldPersistCookies(state, stateErr) {
		log.Warn().Str("user_id", userID).Str("platform", platform).Str("state", string(state)).
			Msg("session cookies not saved after warm-up — not confirmed logged in")
		return
	}
	warmCookies, err := proto.NetworkGetAllCookies{}.Call(page)
	if err != nil {
		return
	}
	fresh := make([]browser.Cookie, 0, len(warmCookies.Cookies))
	for _, c := range warmCookies.Cookies {
		fresh = append(fresh, browser.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   string(c.Domain),
			Path:     c.Path,
			Expires:  float64(c.Expires),
			HTTPOnly: c.HTTPOnly,
			Secure:   bool(c.Secure),
			SameSite: string(c.SameSite),
		})
	}
	raw, marshalErr := browser.MarshalCookies(fresh)
	if marshalErr != nil {
		log.Warn().Err(marshalErr).Str("platform", platform).Msg("warm-up cookie marshal failed")
		return
	}
	if saveErr := m.sessions.Save(userID, platform, "session", raw); saveErr != nil {
		log.Warn().Err(saveErr).Str("platform", platform).Msg("warm-up cookie save failed")
		return
	}
	log.Debug().Str("user_id", userID).Str("platform", platform).Msg("session cookies saved after warm-up")
}

// getLinkedInBrowser returns the persistent LinkedIn browser for userID, creating it if needed.
// Mirrors getSeekBrowser: cookies are loaded, a warm-up page is navigated to linkedin.com
// (so LinkedIn sets fresh session cookies), then the warm-up tab is closed and the browser
// is stored for reuse. Each AI Apply call opens a new tab in this browser.
func (m *Manager) getLinkedInBrowser(userID string, gs domain.GeneralSettings) (*rod.Browser, error) {
	m.browserMu.Lock()
	defer m.browserMu.Unlock()

	if br := m.checkLiveBrowser(m.linkedInBrowsers, userID); br != nil {
		return br, nil
	}
	log.Info().Str("user_id", userID).Msg("linkedin: persistent browser was dead or missing, recreating")

	cookies, err := m.sessions.Load(userID, "linkedin")
	if err != nil {
		return nil, errors.New("no saved LinkedIn session — log in via Settings → Secrets first")
	}

	dir := browser.ProfileDir(userID, "linkedin", gs.Browser.ChromeProfilePath)
	browser.PrepareChromeProfileDir(dir)
	l := launcher.New().Headless(!gs.Browser.ShowBrowser).UserDataDir(dir)
	wsURL, launchErr := l.Launch()
	if launchErr != nil && strings.Contains(launchErr.Error(), "SingletonLock") {
		log.Warn().Err(launchErr).Str("dir", dir).Msg("linkedin: profile locked, force-unlocking and retrying")
		if old := m.linkedInBrowsers[userID]; old != nil {
			_ = old.Close()
			delete(m.linkedInBrowsers, userID)
		}
		browser.ForceUnlockChromeProfile(dir)
		wsURL, launchErr = launcher.New().Headless(!gs.Browser.ShowBrowser).UserDataDir(dir).Launch()
	}
	if launchErr != nil {
		return nil, errors.New("launch chrome: " + launchErr.Error())
	}
	br := rod.New().ControlURL(wsURL)
	if connErr := br.Connect(); connErr != nil {
		return nil, errors.New("connect chrome: " + connErr.Error())
	}

	warmPage, pageErr := br.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if pageErr != nil {
		_ = br.Close()
		return nil, errors.New("open warm-up page: " + pageErr.Error())
	}
	if len(cookies) > 0 {
		_ = warmPage.SetCookies(browser.ToCookieParams(cookies))
	}
	if navErr := warmPage.Navigate("https://www.linkedin.com"); navErr != nil {
		_ = br.Close()
		return nil, errors.New("linkedin warm-up: " + navErr.Error())
	}
	_ = warmPage.Timeout(30 * time.Second).WaitLoad()
	_ = warmPage.Timeout(8 * time.Second).WaitStable(2 * time.Second)

	if state, _ := browser.PageLoginState(warmPage, "linkedin"); state == browser.LoginStateNo {
		tmpBot, _, setupErr := m.setupBot(userID, domain.PlatformLinkedIn, "")
		if setupErr == nil && tmpBot.cfg.LinkedInEmail != "" {
			if recoverErr := tmpBot.linkedinEnsureLoggedIn(warmPage); recoverErr != nil {
				log.Warn().Err(recoverErr).Str("user_id", userID).Msg("linkedin: warm-up auto-login failed")
			}
		}
	}

	// Persist updated cookies so the next call starts with a fresh session.
	m.saveBrowserCookies(warmPage, userID, "linkedin")

	_ = warmPage.Close()
	m.linkedInBrowsers[userID] = br
	log.Info().Str("user_id", userID).Msg("linkedin: persistent browser initialized")
	return br, nil
}

// InvalidateLinkedInBrowser closes and removes the persistent LinkedIn browser for userID.
func (m *Manager) InvalidateLinkedInBrowser(userID string) {
	m.browserMu.Lock()
	defer m.browserMu.Unlock()
	if br, ok := m.linkedInBrowsers[userID]; ok {
		_ = br.Close()
		delete(m.linkedInBrowsers, userID)
		log.Info().Str("user_id", userID).Msg("linkedin: persistent browser closed")
	}
}

func (m *Manager) buildConfig(userID string, platform domain.Platform) (*Config, error) {
	var gs domain.GeneralSettings
	if err := m.cfgStore.Get(userID, "general_settings", &gs); err != nil {
		log.Warn().Err(err).Str("user_id", userID).Msg("buildConfig: failed to load general settings, using defaults")
	}
	if gs.HumanBehavior.DailyApplicationLimit == 0 {
		gs.HumanBehavior.DailyApplicationLimit = 40
	}

	var prefs domain.WorkPreferences
	if err := m.cfgStore.Get(userID, "work_preferences", &prefs); err != nil {
		log.Warn().Err(err).Str("user_id", userID).Msg("buildConfig: failed to load work preferences, using defaults")
	}
	prefs.Normalize()

	var profile domain.ResumeProfile
	if err := m.cfgStore.Get(userID, "resume_profile", &profile); err != nil {
		return nil, errors.New("no resume profile saved, add one at /api/settings/resume first")
	}

	resolved := platform
	if !slices.Contains(domain.SupportedPlatforms, platform) {
		return nil, errors.New("unsupported platform: " + string(platform))
	}

	cookies, err := m.sessions.Load(userID, string(resolved))
	if err != nil {
		return nil, errors.New("no saved session for " + string(resolved) + ", log in via Settings → Secrets first")
	}

	// Build per-user LLM components respecting task-model overrides.
	// Falls back to startup-time components when no API key is available.
	tailor := m.tailor
	scorer := m.scorer
	var tracker *llm.UsageTracker
	if client := m.userLLMClient(userID, gs); client != nil {
		tracker = &llm.UsageTracker{}
		client = client.WithTracker(tracker)
		tm := gs.LLM.TaskModels
		tailor = resume.NewTailor(
			taskClient(client, tm, "tailoring"),
			taskClient(client, tm, "cover_letter"),
			taskClient(client, tm, "form_filling"),
		)
		scorer = resume.NewScorer(taskClient(client, tm, "scoring"))
	}

	cfg := &Config{
		Platform:    resolved,
		Settings:    gs,
		Preferences: prefs,
		Profile:     &profile,
		ProfileLoader: func() *domain.ResumeProfile {
			var p domain.ResumeProfile
			if err := m.cfgStore.Get(userID, "resume_profile", &p); err != nil {
				return nil
			}
			return &p
		},
		Cookies:       cookies,
		Tailor:        tailor,
		Scorer:        scorer,
		HalalChecker:  m.halalCheckerFor(gs),
		Renderer:      m.renderer,
		DB:            m.db,
		UserID:        userID,
		RequireReview: gs.RequireReview,
		MarketDir:     m.marketDir,
		LLMTracker:    tracker,
		Sessions:      m.sessions,
	}

	if platform == domain.PlatformSeek {
		if raw, err := m.secrets.Get(userID, "cred:seek"); err == nil {
			var cred struct {
				Email    string `json:"email"`
				Password string `json:"password"`
			}
			if json.Unmarshal([]byte(raw), &cred) == nil {
				cfg.SeekEmail = cred.Email
				cfg.SeekPassword = cred.Password
			}
		}
	}
	if platform == domain.PlatformLinkedIn {
		if raw, err := m.secrets.Get(userID, "cred:linkedin"); err == nil {
			var cred struct {
				Email    string `json:"email"`
				Password string `json:"password"`
			}
			if json.Unmarshal([]byte(raw), &cred) == nil {
				cfg.LinkedInEmail = cred.Email
				cfg.LinkedInPassword = cred.Password
			}
		}
	}

	return cfg, nil
}

// halalCheckerFor returns the halal checker when the setting is enabled, or nil.
func (m *Manager) halalCheckerFor(gs domain.GeneralSettings) JobHalalChecker {
	if gs.HalalJobFilter {
		return m.halal
	}
	return nil
}

// userLLMClient resolves the API key for userID and returns a ready client, or
// nil if no key is stored. When UseProxy is true, proxy_key takes precedence
// over llm_api_key — matching the same logic used by the handler layer.
func (m *Manager) userLLMClient(userID string, gs domain.GeneralSettings) *llm.Client {
	var apiKey string
	if gs.LLM.UseProxy {
		if pk, err := m.secrets.Get(userID, "proxy_key"); err == nil && pk != "" {
			apiKey = pk
		}
	}
	if apiKey == "" {
		pk, err := m.secrets.Get(userID, "llm_api_key")
		if err != nil || pk == "" {
			return nil
		}
		apiKey = pk
	}
	return llm.New(gs.LLM, apiKey)
}

// taskClient returns a client with model/token overrides for the given task key.
func taskClient(base *llm.Client, tm map[string]domain.TaskModel, task string) *llm.Client {
	if m, ok := tm[task]; ok && m.Model != "" {
		return base.WithModel(m.Model, m.MaxTokens)
	}
	return base
}

// setupBot loads user config and returns a ready Bot plus the resolved GeneralSettings.
// market overrides DefaultResumeMarket when non-empty (used by ApplyFromURL).
func (m *Manager) setupBot(userID string, platform domain.Platform, market string) (*Bot, domain.GeneralSettings, error) {
	var gs domain.GeneralSettings
	if err := m.cfgStore.Get(userID, "general_settings", &gs); err != nil {
		log.Warn().Err(err).Msg("setupBot: load general settings")
	}
	if market != "" {
		gs.DefaultResumeMarket = market
	}

	var profile domain.ResumeProfile
	if err := m.cfgStore.Get(userID, "resume_profile", &profile); err != nil {
		return nil, gs, errors.New("no resume profile saved — add one at Settings first")
	}

	cookies, err := m.sessions.Load(userID, string(platform))
	if err != nil {
		if platform != domain.PlatformSeek {
			return nil, gs, fmt.Errorf("no saved %s session — log in via Settings → Secrets first", platform)
		}
		cookies = nil // Seek: getSeekBrowser handles session loading
	}

	tailor := m.tailor
	scorer := m.scorer
	var tracker *llm.UsageTracker
	if client := m.userLLMClient(userID, gs); client != nil {
		tracker = &llm.UsageTracker{}
		client = client.WithTracker(tracker)
		tm := gs.LLM.TaskModels
		tailor = resume.NewTailor(
			taskClient(client, tm, "tailoring"),
			taskClient(client, tm, "cover_letter"),
			taskClient(client, tm, "form_filling"),
		)
		scorer = resume.NewScorer(taskClient(client, tm, "scoring"))
	}

	b := &Bot{
		cfg: Config{
			Platform:     platform,
			Settings:     gs,
			Cookies:      cookies,
			DB:           m.db,
			UserID:       userID,
			Tailor:       tailor,
			Scorer:       scorer,
			HalalChecker: m.halalCheckerFor(gs),
			Renderer:     m.renderer,
			Profile:      &profile,
			MarketDir:    m.marketDir,
			LLMTracker:   tracker,
			Sessions:     m.sessions,
		},
		state:  domain.BotStateIdle,
		stopCh: make(chan struct{}),
	}
	if platform == domain.PlatformSeek {
		if raw, err := m.secrets.Get(userID, "cred:seek"); err == nil {
			var cred struct{ Email, Password string }
			if json.Unmarshal([]byte(raw), &cred) == nil {
				b.cfg.SeekEmail = cred.Email
				b.cfg.SeekPassword = cred.Password
			}
		}
	}
	if platform == domain.PlatformLinkedIn {
		if raw, err := m.secrets.Get(userID, "cred:linkedin"); err == nil {
			var cred struct{ Email, Password string }
			if json.Unmarshal([]byte(raw), &cred) == nil {
				b.cfg.LinkedInEmail = cred.Email
				b.cfg.LinkedInPassword = cred.Password
			}
		}
	}
	return b, gs, nil
}

// ApplyFromURL opens a browser at jobURL, scores the job, checks for Easy/Quick Apply,
// and either applies immediately (background goroutine) or routes to the appropriate queue.
func (m *Manager) ApplyFromURL(ctx context.Context, userID, jobURL, market string, force bool) (ApplyFromURLResult, error) {
	// Prevent concurrent AI Apply calls for the same user — a second call arriving
	// while the first is still in-flight (browser open, LLMs running) would open a
	// second browser session and apply twice to the same job.
	m.applyMu.Lock()
	if m.applyInProgress[userID] {
		m.applyMu.Unlock()
		return ApplyFromURLResult{}, fmt.Errorf("an AI Apply is already in progress — please wait for it to finish")
	}
	m.applyInProgress[userID] = true
	m.applyMu.Unlock()
	defer func() {
		m.applyMu.Lock()
		delete(m.applyInProgress, userID)
		m.applyMu.Unlock()
	}()

	platform, err := detectPlatformFromURL(jobURL)
	if err != nil {
		return ApplyFromURLResult{}, err
	}

	b, gs, err := m.setupBot(userID, platform, market)
	if err != nil {
		return ApplyFromURLResult{}, err
	}

	// Fast path: check our own DB before opening a browser.
	// If we already have a record for this URL the user doesn't need to re-apply.
	// Skip when force=true — let the DOM check (detectSeekPageApplied) be authoritative.
	if !force {
		var dbApplied int
		if dbErr := m.db.QueryRow(
			`SELECT COUNT(*) FROM jobs_applied WHERE user_id = ? AND link = ?`, userID, jobURL,
		).Scan(&dbApplied); dbErr == nil && dbApplied > 0 {
			var company, role string
			_ = m.db.QueryRow(
				`SELECT company, role FROM jobs_applied WHERE user_id = ? AND link = ? LIMIT 1`, userID, jobURL,
			).Scan(&company, &role)
			return ApplyFromURLResult{Company: company, Role: role}, ErrAlreadyApplied
		}
	}

	// Open browser and navigate — use persistent browsers for LinkedIn and Seek
	// (same tab-per-job pattern) so no warm-up window is left open.
	var br *rod.Browser
	var jobPage *rod.Page
	ownsBr := false
	if platform == domain.PlatformSeek && gs.Browser.RemoteDebugPort == 0 {
		seekBr, brErr := m.getSeekBrowser(userID, gs)
		if brErr != nil {
			return ApplyFromURLResult{}, fmt.Errorf("seek browser: %w", brErr)
		}
		p, pageErr := seekBr.Page(proto.TargetCreateTarget{URL: jobURL})
		if pageErr != nil {
			m.InvalidateSeekBrowser(userID)
			return ApplyFromURLResult{}, fmt.Errorf("open seek tab: %w", pageErr)
		}
		br, jobPage = seekBr, p
	} else if platform == domain.PlatformLinkedIn && gs.Browser.RemoteDebugPort == 0 {
		liBr, brErr := m.getLinkedInBrowser(userID, gs)
		if brErr != nil {
			return ApplyFromURLResult{}, fmt.Errorf("linkedin browser: %w", brErr)
		}
		p, pageErr := liBr.Page(proto.TargetCreateTarget{URL: jobURL})
		if pageErr != nil {
			m.InvalidateLinkedInBrowser(userID)
			return ApplyFromURLResult{}, fmt.Errorf("open linkedin tab: %w", pageErr)
		}
		br, jobPage = liBr, p
	} else {
		// Remote debug port: connect to existing Chrome, open a new tab directly.
		newBr, warmPage, launchErr := b.launchBrowser(ctx)
		if launchErr != nil {
			return ApplyFromURLResult{}, fmt.Errorf("launch browser: %w", launchErr)
		}
		newPage, pageErr := newBr.Page(proto.TargetCreateTarget{URL: jobURL})
		warmPage.Close()
		if pageErr != nil {
			return ApplyFromURLResult{}, fmt.Errorf("open job tab: %w", pageErr)
		}
		br, jobPage = newBr, newPage
	}

	// Wait for the page to render. Cap both to avoid hanging on dynamic SPAs:
	// WaitLoad waits for the window.onload event; WaitStable waits for DOM to stop
	// changing — LinkedIn's SPA updates indefinitely, so we must bound it.
	_ = jobPage.Timeout(30 * time.Second).WaitLoad()
	_ = jobPage.Timeout(8 * time.Second).WaitStable(2 * time.Second)

	// Log where we actually landed — helps diagnose session/redirect issues.
	if cu, cuErr := jobPage.Eval(`() => window.location.href`); cuErr == nil {
		log.Debug().Str("url", cu.Value.String()).Str("platform", string(platform)).Msg("ai apply: page loaded")
	}

	// Validate we actually landed on a job page. LinkedIn may redirect when the
	// session is expired or bot-detection kicks in.
	if platform == domain.PlatformLinkedIn {
		if cu, cuErr := jobPage.Eval(`() => window.location.href`); cuErr == nil {
			cu := cu.Value.String()
			switch {
			case strings.Contains(cu, "/feed") || strings.Contains(cu, "/authwall"):
				jobPage.Close()
				if ownsBr {
					br.Close()
				}
				return ApplyFromURLResult{}, fmt.Errorf("LinkedIn redirected to %s — your session may be expired, please log in via Settings → Secrets and try again", cu)
			case strings.Contains(cu, "/login") || strings.Contains(cu, "/uas/login") || strings.Contains(cu, "/checkpoint"):
				jobPage.Close()
				if ownsBr {
					br.Close()
				}
				return ApplyFromURLResult{}, fmt.Errorf("LinkedIn requires login — please save a fresh LinkedIn session in Settings → Secrets")
			case !strings.Contains(cu, "/jobs/"):
				jobPage.Close()
				if ownsBr {
					br.Close()
				}
				return ApplyFromURLResult{}, fmt.Errorf("unexpected LinkedIn page (%s) — paste a direct job URL (linkedin.com/jobs/view/…)", cu)
			}
		}
	}

	company, role := extractJobMeta(jobPage, platform)
	if company == "" {
		company = string(platform)
	}
	if role == "" {
		role = "Unknown Role"
	}
	location := extractJobLocation(jobPage, platform)

	// DOM check: page itself shows a prior application (handles jobs applied outside this app).
	var pageApplied bool
	switch platform {
	case domain.PlatformSeek:
		pageApplied = detectSeekPageApplied(jobPage)
	default:
		pageApplied = b.linkedInPageApplied(jobPage)
	}
	if pageApplied {
		jobPage.Close()
		if ownsBr {
			br.Close()
		}
		// Always surface as already_applied — never nil (which the UI treats as fresh Applied ✓).
		var alreadyTracked int
		if err := m.db.QueryRow(`SELECT COUNT(*) FROM jobs_applied WHERE user_id=? AND link=?`, userID, jobURL).Scan(&alreadyTracked); err != nil {
			log.Warn().Err(err).Str("url", jobURL).Msg("ai apply: failed to check already-tracked status")
		}
		if alreadyTracked == 0 {
			rid := newJobID()
			if _, err := m.db.Exec(
				`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,suitability_score,halal_verdict,applied_at)
				 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
				rid, userID, string(platform), company, role, location, jobURL, "", "", 0, "",
				time.Now().UTC().Format(time.RFC3339),
			); err != nil {
				log.Error().Err(err).Str("url", jobURL).Msg("ai apply: failed to record manually-applied job")
			}
			if _, err := m.db.Exec(`DELETE FROM jobs_pending_review WHERE user_id=? AND link=?`, userID, jobURL); err != nil {
				log.Error().Err(err).Str("url", jobURL).Msg("ai apply: failed to delete pending review for manually-applied job")
			}
		}
		return ApplyFromURLResult{Company: company, Role: role}, ErrAlreadyApplied
	}

	jobID := newJobID()

	applyLabel := "easy apply"
	if platform == domain.PlatformSeek {
		applyLabel = "quick apply"
	}

	// STEP 1: Easy/Quick Apply check BEFORE scoring — external Apply jobs must fail
	// fast so the UI does not sit on "Submitting application…" during an LLM score.
	var isEasyApply bool
	switch platform {
	case domain.PlatformSeek:
		isEasyApply = detectSeekEasyApply(jobPage)
	default:
		isEasyApply = detectLinkedInEasyApply(jobPage)
	}

	// Extract job description from the already-loaded page (needed for score + pending).
	jobDesc := role + " at " + company
	if rawHTML, htmlErr := jobPage.HTML(); htmlErr == nil {
		if d := scraper.ParseHTML(rawHTML); len(strings.TrimSpace(d.Description)) >= 100 {
			jobDesc = d.Description
		}
	}

	if !isEasyApply {
		// Light score for Top Matches context — bounded so this path cannot hang.
		score, scoreReason := 0, ""
		if b.cfg.Scorer != nil {
			scoreCtx, scoreCancel := context.WithTimeout(ctx, 45*time.Second)
			if jScore, scoreErr := b.cfg.Scorer.EvaluateJob(scoreCtx, b.cfg.Profile, jobDesc); scoreErr == nil {
				score, scoreReason = jScore.Score, jScore.Reasoning
			} else {
				log.Warn().Err(scoreErr).Msg("ai apply: scoring failed for non-easy-apply job")
			}
			scoreCancel()
		}
		jobPage.Close()
		if ownsBr {
			br.Close()
		}
		var pendingCount int
		if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs_pending_review WHERE user_id=? AND link=?`, userID, jobURL).Scan(&pendingCount); err != nil {
			log.Warn().Err(err).Str("user_id", userID).Str("url", jobURL).Msg("ai apply: pending check failed")
		}
		if pendingCount == 0 {
			if _, err := m.db.ExecContext(ctx,
				`INSERT OR IGNORE INTO jobs_pending_review
				 (job_id,user_id,company,role,location,platform,link,resume_path,cover_letter_path,
				  suitability_score,suitability_reasoning,easy_apply,created_at)
				 VALUES(?,?,?,?,?,?,?,?,?,?,?,0,datetime('now'))`,
				jobID, userID, company, role, location, string(platform), jobURL, "", "", score, scoreReason,
			); err != nil {
				log.Error().Err(err).Str("user_id", userID).Str("url", jobURL).Msg("ai apply: insert pending review failed")
			}
		}
		return ApplyFromURLResult{Company: company, Role: role, Score: score, ScoreReason: scoreReason, JobID: jobID}, ErrNotEasyApply
	}

	// STEP 2: Score Easy/Quick Apply jobs (bounded).
	score, scoreReason := 0, ""
	threshold := gs.JobSuitabilityScore
	if threshold == 0 {
		threshold = 7
	}
	scoredOK := false
	if b.cfg.Scorer != nil {
		scoreCtx, scoreCancel := context.WithTimeout(ctx, 90*time.Second)
		if jScore, scoreErr := b.cfg.Scorer.EvaluateJob(scoreCtx, b.cfg.Profile, jobDesc); scoreErr == nil {
			score, scoreReason = jScore.Score, jScore.Reasoning
			scoredOK = true
		} else {
			log.Warn().Err(scoreErr).Msg("ai apply: scoring failed, proceeding without score gate")
		}
		scoreCancel()
	}

	// STEP 3: Score threshold (skip when force=true).
	if !force && scoredOK && score < threshold {
		jobPage.Close()
		if ownsBr {
			br.Close()
		}
		return ApplyFromURLResult{
			Company:      company,
			Role:         role,
			Score:        score,
			ScoreReason:  scoreReason,
			JobID:        jobID,
			ScoreWarning: true,
		}, nil
	}

	// STEP 4: Apply — use request ctx (includes 4m handler timeout) so a hung
	// browser/form cannot block "Submitting application…" indefinitely.
	lazy := &lazyDocGen{b: b, ctx: ctx, job: linkedInJob{Company: company, Title: role}, jobDesc: jobDesc}
	defer jobPage.Close()
	if ownsBr {
		defer br.Close()
	}

	var applyErr error
	if platform == domain.PlatformSeek {
		applyErr = b.seekApply(ctx, jobPage, lazy)
	} else {
		applyErr = b.easyApply(ctx, jobPage, lazy)
	}

	if applyErr != nil && !errors.Is(applyErr, errAlreadyApplied) {
		log.Error().Err(applyErr).Str("job", role).Msg("ai apply: failed")
		if isSeekApplyBlockedError(applyErr) {
			var pendingCount int
			if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs_pending_review WHERE user_id=? AND link=?`, userID, jobURL).Scan(&pendingCount); err != nil {
				log.Warn().Err(err).Str("user_id", userID).Str("url", jobURL).Msg("ai apply: pending check failed")
			}
			if pendingCount == 0 {
				if _, err := m.db.ExecContext(ctx,
					`INSERT OR IGNORE INTO jobs_pending_review
					 (job_id,user_id,company,role,location,platform,link,resume_path,cover_letter_path,
					  suitability_score,suitability_reasoning,easy_apply,created_at)
					 VALUES(?,?,?,?,?,?,?,?,?,?,?,0,datetime('now'))`,
					jobID, userID, company, role, location, string(platform), jobURL, "", "", score, scoreReason,
				); err != nil {
					log.Error().Err(err).Str("user_id", userID).Str("url", jobURL).Msg("ai apply: insert pending review failed")
				}
			}
			return ApplyFromURLResult{Company: company, Role: role, Score: score, ScoreReason: scoreReason, JobID: jobID},
				fmt.Errorf("%s not automatable: %w", applyLabel, applyErr)
		}
		var skippedCount int
		if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs_skipped WHERE user_id=? AND link=?`, userID, jobURL).Scan(&skippedCount); err != nil {
			log.Warn().Err(err).Str("user_id", userID).Str("url", jobURL).Msg("ai apply: skipped check failed")
		}
		if skippedCount == 0 {
			if _, err := m.db.ExecContext(ctx,
				`INSERT OR IGNORE INTO jobs_skipped(id,user_id,platform,company,role,location,link,skip_reason,suitability_score,suitability_reasoning,viewed_at)
				 VALUES(?,?,?,?,?,?,?,?,?,?,datetime('now'))`,
				jobID, userID, string(platform), company, role, location, jobURL,
				applyLabel+": "+applyErr.Error(), score, scoreReason,
			); err != nil {
				log.Error().Err(err).Str("user_id", userID).Str("url", jobURL).Msg("ai apply: insert skipped failed")
			}
		}
		return ApplyFromURLResult{Company: company, Role: role, Score: score, ScoreReason: scoreReason},
			fmt.Errorf("%s failed: %w", applyLabel, applyErr)
	}

	// Defence in depth: never record Applied unless the platform UI confirms it.
	// Stops false "Applied ✓" when Easy Apply detection misfired on external Apply,
	// or when success-page heuristics fired without a real submission.
	if !errors.Is(applyErr, errAlreadyApplied) {
		confirmed := false
		switch platform {
		case domain.PlatformSeek:
			confirmed = detectSeekPageApplied(jobPage)
			if !confirmed {
				_ = jobPage.Navigate(jobURL)
				_ = jobPage.Timeout(30 * time.Second).WaitLoad()
				_ = jobPage.Timeout(5 * time.Second).WaitStable(2 * time.Second)
				confirmed = detectSeekPageApplied(jobPage)
			}
		default:
			confirmed = b.linkedInPageApplied(jobPage)
			if !confirmed {
				_ = jobPage.Navigate(jobURL)
				_ = jobPage.Timeout(30 * time.Second).WaitLoad()
				_ = jobPage.Timeout(5 * time.Second).WaitStable(2 * time.Second)
				confirmed = b.linkedInPageApplied(jobPage)
			}
		}
		if !confirmed {
			log.Warn().Str("job", role).Str("url", jobURL).Msg("ai apply: bot reported success but job page does not show Applied")
			return ApplyFromURLResult{Company: company, Role: role, Score: score, ScoreReason: scoreReason},
				fmt.Errorf("%s failed: application was not confirmed on the job page (this may be an external Apply job — only Easy/Quick Apply is supported)", applyLabel)
		}
	}

	resumePath, coverPath := lazy.get()
	if _, err := m.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,suitability_score,halal_verdict,applied_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		jobID, userID, string(platform), company, role, location, jobURL, resumePath, coverPath, score, "",
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		log.Error().Err(err).Str("user_id", userID).Str("url", jobURL).Msg("ai apply: insert applied failed")
	}
	if _, err := m.db.ExecContext(ctx, `DELETE FROM jobs_pending_review WHERE user_id = ? AND link = ?`, userID, jobURL); err != nil {
		log.Error().Err(err).Str("user_id", userID).Str("url", jobURL).Msg("ai apply: delete pending review failed")
	}
	log.Info().Str("job", role).Str("company", company).Msg("ai apply: submitted ✓")
	return ApplyFromURLResult{Company: company, Role: role, Score: score, ScoreReason: scoreReason}, nil
}

func newJobID() string {
	return uuid.NewString()
}

// extractJobLocation tries to read the job location from the loaded job page.
// Returns an empty string if no location can be found.
func extractJobLocation(page *rod.Page, platform domain.Platform) string {
	var js string
	switch platform {
	case domain.PlatformSeek:
		js = `() => {
			const sels = [
				'[data-automation="job-detail-location"]',
				'[data-automation="jobLocation"]',
				'[data-testid="job-detail-location"]',
				'[data-automation="job-location"]',
				'[data-automation="job-locations"]',
				'[data-testid="job-location"]',
				'span[class*="location"]',
			];
			for (const s of sels) {
				const el = document.querySelector(s);
				if (el) { const t = el.textContent.trim(); if (t) return t; }
			}
			return '';
		}`
	default: // LinkedIn and others
		js = `() => {
			const sels = [
				'.jobs-unified-top-card__bullet',
				'[data-test-job-location]',
				'span[class*="topcard__flavor--bullet"]',
				'.job-details-jobs-unified-top-card__primary-description span',
			];
			for (const s of sels) {
				const el = document.querySelector(s);
				if (el) { const t = el.textContent.trim(); if (t) return t; }
			}
			return '';
		}`
	}
	res, err := page.Eval(js)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(res.Value.String())
}



// extractJobMeta pulls company and role from the job page.
// Seek uses DOM data-automation selectors. LinkedIn and others use a JS extractor
// that reads the aria-label accessibility attribute for company (stable across
// LinkedIn's hashed CSS class deployments) and derives role from document.title.
func extractJobMeta(page *rod.Page, platform domain.Platform) (company, role string) {
	if platform == domain.PlatformSeek {
		for _, sel := range []string{
			"[data-automation='advertiser-name']",
			"[data-automation='job-detail-company-name']",
			"[data-automation*='advertiser']",
		} {
			if elems, err := page.Elements(sel); err == nil && len(elems) > 0 {
				if txt, err := elems[0].Text(); err == nil && txt != "" {
					company = txt
					break
				}
			}
		}
		for _, sel := range []string{
			"[data-automation='job-detail-title']",
			"h1[data-automation]",
			"h1",
		} {
			if elems, err := page.Elements(sel); err == nil && len(elems) > 0 {
				if txt, err := elems[0].Text(); err == nil && txt != "" {
					role = txt
					break
				}
			}
		}
		if company != "" || role != "" {
			return
		}
	}

	// LinkedIn's new UI uses fully hashed CSS class names that change on every
	// deployment, so class-based selectors are unreliable. Instead we use:
	//   • aria-label="Company, <name>." — LinkedIn sets this for screen-reader
	//     accessibility and it is stable across UI changes.
	//   • document.title — parse role after stripping known suffixes.
	res, err := page.Eval(`() => {
		let company = '';
		const companyEl = document.querySelector('[aria-label^="Company, "]');
		if (companyEl) {
			const lbl = companyEl.getAttribute('aria-label') || '';
			company = lbl.replace(/^Company,\s*/i, '').replace(/[.,]\s*$/, '').trim();
		}
		if (!company) {
			for (const a of document.querySelectorAll('a[href*="/company/"]')) {
				const t = (a.textContent || '').trim();
				if (t && t.length > 0 && t.length < 100) { company = t; break; }
			}
		}

		// LinkedIn title format: "Role | Company | LinkedIn"  OR  "Role at Company | LinkedIn"
		let title = (document.title || '').trim();
		title = title.replace(/\s*\|\s*LinkedIn\s*$/i, '').trim();
		if (company) {
			const esc = company.replace(/[-[\]{}()*+?.,\\^$|#]/g, '\\$&');
			title = title.replace(new RegExp('\\s*\\|\\s*' + esc + '\\s*$'), '').trim();
			title = title.replace(new RegExp('\\s+at\\s+' + esc + '\\s*$', 'i'), '').trim();
		} else {
			const idx = title.lastIndexOf(' | ');
			if (idx > 0) {
				company = title.substring(idx + 3).trim();
				title   = title.substring(0, idx).trim();
			} else {
				const atIdx = title.indexOf(' at ');
				if (atIdx > 0) {
					company = title.substring(atIdx + 4).trim();
					title   = title.substring(0, atIdx).trim();
				}
			}
		}
		if (!title) {
			const h1 = document.querySelector('h1');
			if (h1) title = (h1.textContent || '').trim();
		}
		return JSON.stringify({title, company});
	}`)
	if err != nil {
		return "", ""
	}
	var m struct{ Title, Company string }
	if json.Unmarshal([]byte(res.Value.String()), &m) == nil {
		return m.Company, m.Title
	}
	return "", ""
}
