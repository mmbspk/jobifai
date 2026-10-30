// Package bot contains the job-application automation orchestrator.
// Platform runners are pluggable: implement platformRunner and register via init().
package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/browser"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/resume"
	"github.com/user/jobifai/internal/scraper"
)

// lazyDocGen generates resume + cover letter on first demand and caches the
// result. Generation only happens when the toggle is on and a file upload
// field is actually encountered during form filling.
type lazyDocGen struct {
	b                     *Bot
	ctx                   context.Context
	job                   linkedInJob
	jobDesc               string
	attemptID             string // set by each entry point; empty in processApprovedQueue path
	once                  sync.Once
	resume                string
	cover                 string
	formOnce              sync.Once
	formJSON              []byte
	resumeOverride        string // pre-generated resume path from a prior attempt; skips LLM if set
	coverOverride         string // pre-generated cover letter path from a prior attempt; skips LLM if set
	resumeVersionOverride string
	coverVersionOverride  string
	resumeUseSiteOverride bool
	coverSkipOverride     bool
	frozen                bool
	formCaps              documents.FormDocumentCapabilities
	holdReason            string
	resumeVersionID       string
	coverVersionID        string
	refsJSON              string
	useSiteResume         bool
	matMu                 sync.Mutex
	materialized          bool
	capsFingerprint       string
	reviewPrepareOnly     bool
	prepareScanComplete   bool
	packRestored          bool
	packMetaApplied       bool
	pack                  documents.ApplicationDocumentPack
}

// get returns the generated resume and cover letter paths, generating them on
// the first call. If override paths from a prior attempt are set, they are
// returned directly without calling the LLM again.
func (l *lazyDocGen) get() (resume, cover string) {
	if l.frozen {
		l.restoreFrozenPack()
		return l.resume, l.cover
	}
	if l.b.cfg.Documents != nil {
		l.ensureMaterialized()
		if l.policyBlocked() {
			return "", ""
		}
		return l.resume, l.cover
	}
	if !l.b.cfg.Settings.GenerateNewResumeDocs {
		return "", ""
	}
	l.once.Do(func() {
		l.resume, l.cover = l.b.generateDocs(l.ctx, l.job, l.jobDesc)
	})
	if l.resumeOverride != "" {
		return l.resumeOverride, l.cover
	}
	if l.coverOverride != "" {
		return l.resume, l.coverOverride
	}
	return l.resume, l.cover
}

// peek returns already-generated paths without triggering new generation.
// Returns override paths if set, otherwise whatever get() has already cached.
// Returns empty strings if generation never ran.
func (l *lazyDocGen) peek() (resume, cover string) {
	if l.frozen {
		l.restoreFrozenPack()
	}
	if l.resumeOverride != "" {
		resume = l.resumeOverride
	} else {
		resume = l.resume
	}
	if l.coverOverride != "" {
		cover = l.coverOverride
	} else {
		cover = l.cover
	}
	return resume, cover
}

// It is computed once per job session and cached.
func (l *lazyDocGen) formProfileJSON() []byte {
	l.formOnce.Do(func() {
		if p := l.b.currentProfile(); p != nil {
			trimmed := resume.ForFormFilling(p)
			l.formJSON, _ = json.Marshal(trimmed)
		}
	})
	return l.formJSON
}

// ResumeTailor is the subset of resume.Tailor the bot uses.
type ResumeTailor interface {
	TailorProfile(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (*domain.ResumeProfile, error)
	WriteCoverLetter(ctx context.Context, profile *domain.ResumeProfile, jobDesc, tone string) (string, error)
	// AnswerFormQuestion picks the best answer for a job-application form field.
	// profileJSON is a pre-serialized trimmed profile cached once per job session.
	// options is non-nil for radio/select, the returned string must match one of the labels.
	// For free-text fields options is nil and a short phrase is expected.
	AnswerFormQuestion(ctx context.Context, profileJSON []byte, question string, options []string) (string, error)
	// IdentifyFormFields sends a screenshot to the LLM and returns the visible
	// unanswered form fields. Used as a last-resort fallback when jsScanFields
	// returns nothing but the DOM probe confirms visible inputs are present.
	IdentifyFormFields(ctx context.Context, imageBytes []byte) ([]domain.IdentifiedField, error)
}

// ResumeRenderer is the subset of resume.PDFRenderer the bot uses.
type ResumeRenderer interface {
	RenderResume(ctx context.Context, profile *domain.ResumeProfile, styleName, cssOverride string, opts *resume.RenderOptions) ([]byte, error)
	RenderCoverLetter(ctx context.Context, body string, styleName, cssOverride string) ([]byte, error)
}

// JobScorer evaluates job-profile suitability.
type JobScorer interface {
	EvaluateJob(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (domain.JobScore, error)
}

// JobHalalChecker evaluates job permissibility under Islamic employment ethics.
type JobHalalChecker interface {
	CheckHalal(ctx context.Context, title, company, description string) (domain.HalalVerdict, error)
}

// Config bundles everything the bot needs to run.
type Config struct {
	OnApplied       func() // successful application persistence schedules retention reconciliation
	Platform        domain.Platform
	Settings        domain.GeneralSettings
	Preferences     domain.WorkPreferences
	Profile         *domain.ResumeProfile
	ProfileLoader   func() *domain.ResumeProfile // if set, called per-job to get the latest profile
	Cookies         []browser.Cookie
	Tailor          ResumeTailor    // nil = no LLM tailoring
	Scorer          JobScorer       // nil = evaluation unavailable; never a passing score
	HalalChecker    JobHalalChecker // nil = halal filter disabled
	CoverLetterTone string          // "" = default; see domain.GeneralSettings.CoverLetterTone
	Renderer        ResumeRenderer
	DB              *sql.DB
	UserID          string // owner of this bot session
	AutomationRunID string // one ID per automation session for LLM billing attribution
	ApplicationID   string // set when applying to a specific application record
	RequireReview   bool
	// Logger is an optional run-scoped logger with user_id, automation_run_id,
	// and log_id hook pre-wired. Nil means use the global log.Logger.
	Logger           *zerolog.Logger
	MarketDir        string             // path to resume_markets/ directory
	Documents        *documents.Service // versioned document foundation (#58/#59)
	LLMTracker       *llm.UsageTracker  // optional; tracks per-job token usage for success log
	SeekEmail        string             // stored credentials for auto-login on session expiry
	SeekPassword     string
	LinkedInEmail    string
	LinkedInPassword string
	Sessions         *browser.SessionStore // if set, updated with fresh cookies after each auth
	BrowserMeter     BrowserUsageMeter     // optional; meters active automation against credit balance
}

// SubmitRequest bundles the fields needed to submit a single approved job.
type SubmitRequest struct {
	JobID                  string
	Company                string
	Role                   string
	Location               string
	Platform               string
	Link                   string
	ResumePath             string
	CoverPath              string
	ResumeContentVersionID string
	CoverContentVersionID  string
	DocumentRefsJSON       string
	FrozenDocuments        bool
	SuitabilityReasoning   string
}

// Bot runs the Easy Apply automation loop for a single platform session.
type Bot struct {
	cfg            Config
	log            zerolog.Logger // run-scoped; carries user_id, automation_run_id, log_id hook
	mu             sync.Mutex
	state          domain.BotState
	currentKeyword string
	stopCh         chan struct{}
	pauseMu        sync.Mutex
	pauseCh        chan struct{} // non-nil and open when paused; closed on resume

	seekSessionExpired bool // set during runSeek when a job page reveals the Seek session is no longer valid

	seekSeams *seekAuthSeams // nil in production; injected in tests

	seenCache          *jobSeenCache
	listingFailureCode string
	listingFailedJobs  map[string]bool

	evaluationFailedJobs map[string]bool // distinct jobs since the last completed evaluation
	automaticStopReason  string

	quotaStopOnce bool // guards quota_stop event; set after first emission per run
}

// SetKeyword stores the keyword currently being searched (thread-safe).
func (b *Bot) SetKeyword(k string) {
	b.mu.Lock()
	b.currentKeyword = k
	b.mu.Unlock()
}

// Keyword returns the keyword currently being searched (thread-safe).
func (b *Bot) Keyword() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.currentKeyword
}

func New(cfg Config) *Bot {
	var botLog zerolog.Logger
	if cfg.Logger != nil {
		botLog = *cfg.Logger
	} else {
		botLog = log.Logger
	}
	return &Bot{cfg: cfg, log: botLog, state: domain.BotStateIdle, stopCh: make(chan struct{}), seenCache: newJobSeenCache()}
}

// ErrAuthRequired signals that the bot detected a platform authentication
// challenge it cannot resolve automatically. The user must re-authenticate
// via Settings → Connect before the bot can continue.
var ErrAuthRequired = errors.New("platform authentication required")

// seekAuthSeams holds injectable stubs for the SEEK re-auth lifecycle.
// All fields default to nil (production uses real implementations).
// Set non-nil in tests to exercise the recovery logic without launching Chrome.
type seekAuthSeams struct {
	ensureLoggedIn  func(*rod.Page) error
	closeBrowser    func()
	relaunchBrowser func() (*rod.Browser, *rod.Page, error)
	checkLoginState func(*rod.Page) (browser.LoginState, error)
}

// authRefreshPollInterval is how often waitForSessionRefresh checks the session
// store. Override in tests to speed up polling.
var authRefreshPollInterval = 5 * time.Second

// waitForSessionRefresh blocks until the stored session for userID/platform is
// updated (user re-authenticated via Connect browser) or until the timeout
// elapses. During the wait the bot state is user_action_required. Returns nil
// when a fresh session is detected, error on timeout or bot stop.
//
// If browser billing was previously active it is deactivated before the wait
// (no charge during the idle wait) and reactivated after successful re-auth.
// Failed re-auth does not reactivate billing.
func (b *Bot) waitForSessionRefresh(ctx context.Context, userID, platform string, timeout time.Duration) error {
	if b.cfg.Sessions == nil {
		return fmt.Errorf("no session store available")
	}
	initial, err := b.cfg.Sessions.Status(userID, platform)
	if err != nil {
		return fmt.Errorf("read session status: %w", err)
	}

	// Suspend billing during the wait (no charge for idle time).
	meterWasActive := b.cfg.BrowserMeter != nil && b.cfg.BrowserMeter.WasActivated()
	if b.cfg.BrowserMeter != nil {
		b.cfg.BrowserMeter.Deactivate()
	}

	b.mu.Lock()
	b.state = domain.BotStateUserActionRequired
	b.mu.Unlock()

	deadline := time.Now().Add(timeout)
	refreshed := false
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waitForSessionRefresh: %w", ctx.Err())
		case <-b.stopCh:
			return fmt.Errorf("bot stopped while waiting for %s authentication", platform)
		case <-time.After(authRefreshPollInterval):
		}
		current, serr := b.cfg.Sessions.Status(userID, platform)
		if serr != nil {
			continue
		}
		if current.UpdatedAt.After(initial.UpdatedAt) {
			refreshed = true
			break
		}
	}
	if !refreshed {
		return fmt.Errorf("timed out waiting for %s authentication", platform)
	}
	// Reactivate billing now that a fresh session has been confirmed —
	// only if billing was previously active (initial auth remains free).
	if meterWasActive {
		if activateErr := b.cfg.BrowserMeter.Activate(ctx); activateErr != nil {
			return fmt.Errorf("browser billing reactivation after session refresh: %w", activateErr)
		}
	}
	return nil
}

// startProfileBrowser launches a fresh Chrome process from the persistent profile,
// injects the configured session cookies, and navigates to the platform home URL.
// It does NOT perform SingletonLock force-unlock — callers must ensure the profile
// is free before calling (e.g. after a Connect browser has saved its session and closed).
// On error, any started browser is closed before returning.
func (b *Bot) startProfileBrowser() (br *rod.Browser, page *rod.Page, err error) {
	dir := browser.ProfileDir(b.cfg.UserID, string(b.cfg.Platform), b.cfg.Settings.Browser.ChromeProfilePath)
	browser.PrepareChromeProfileDir(dir) // clears stale singleton locks without force-killing

	l := launcher.New().
		Headless(!b.cfg.Settings.Browser.ShowBrowser).
		Set("--disable-blink-features", "AutomationControlled").
		Set("--exclude-switches", "enable-automation").
		UserDataDir(dir)

	u, launchErr := l.Launch()
	if launchErr != nil {
		return nil, nil, fmt.Errorf("launch chrome: %w", launchErr)
	}
	br = rod.New().ControlURL(u)
	if connectErr := br.Connect(); connectErr != nil {
		return nil, nil, fmt.Errorf("connect chrome: %w", connectErr)
	}
	page, err = br.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		_ = br.Close()
		return nil, nil, fmt.Errorf("open blank page: %w", err)
	}
	if len(b.cfg.Cookies) > 0 {
		if setErr := page.SetCookies(browser.ToCookieParams(b.cfg.Cookies)); setErr != nil {
			b.log.Warn().Err(setErr).Msg("browser: set cookies")
		}
	}
	homeURL := "https://www.linkedin.com"
	if b.cfg.Platform == domain.PlatformSeek {
		homeURL = "https://au.seek.com/jobs"
	}
	if navErr := page.Navigate(homeURL); navErr != nil {
		_ = br.Close()
		return nil, nil, fmt.Errorf("navigate %s: %w", b.cfg.Platform, navErr)
	}
	_ = page.Timeout(30 * time.Second).WaitLoad()
	_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)
	return br, page, nil
}

// seekRecoverAuth encapsulates the SEEK re-auth recovery protocol.
// closeFn is called to release the current bot browser before the wait begins.
//
// Returns (newBr, newPage, nil)  → recovery complete; caller MUST set BotStateRunning.
// Returns (nil, nil, nil)        → ensureLoggedIn returned no error; nothing to recover.
// Returns (nil, nil, err)        → unrecoverable failure.
//
// BotStateRunning is intentionally NOT set here — state transitions to Running
// only in launchBrowser after this returns success, so the bot never reports
// Running before a fully-verified browser is in hand.
//
// All four operations are injectable via b.seekSeams for unit testing without Chrome.
func (b *Bot) seekRecoverAuth(ctx context.Context, page *rod.Page, closeFn func()) (newBr *rod.Browser, newPage *rod.Page, err error) {
	ensureLoggedIn := func(p *rod.Page) error { return b.seekEnsureLoggedIn(p) }
	relaunch := func() (*rod.Browser, *rod.Page, error) { return b.startProfileBrowser() }
	checkState := func(p *rod.Page) (browser.LoginState, error) {
		return browser.PageLoginState(p, "seek")
	}
	if s := b.seekSeams; s != nil {
		if s.ensureLoggedIn != nil {
			ensureLoggedIn = s.ensureLoggedIn
		}
		if s.relaunchBrowser != nil {
			relaunch = s.relaunchBrowser
		}
		if s.checkLoginState != nil {
			checkState = s.checkLoginState
		}
	}

	authErr := ensureLoggedIn(page)
	if authErr == nil {
		return nil, nil, nil // happy path — no auth problem
	}
	if !errors.Is(authErr, ErrAuthRequired) {
		// Non-auth errors (network, page parsing, SEEK outage) are released
		// cleanly and propagated immediately — no wait for user action.
		closeFn()
		return nil, nil, fmt.Errorf("seek: %w", authErr)
	}

	// ErrAuthRequired: release the Chrome profile so the user can open Connect.
	// ForceUnlock is NOT used — Connect closes cleanly before saving its session.
	b.log.Warn().Msg("seek: auth required — releasing Chrome profile for Connect browser; waiting up to 5 min")
	closeFn()

	if waitErr := b.waitForSessionRefresh(ctx, b.cfg.UserID, "seek", 5*time.Minute); waitErr != nil {
		return nil, nil, fmt.Errorf("seek: %w", waitErr)
	}

	// Reload the freshly saved cookies so the relaunched browser picks them up
	// alongside the refreshed persistent profile state (Auth0, localStorage, etc.)
	if b.cfg.Sessions != nil {
		if fresh, loadErr := b.cfg.Sessions.Load(b.cfg.UserID, "seek"); loadErr == nil && len(fresh) > 0 {
			b.cfg.Cookies = fresh
		}
	}

	newBr, newPage, err = relaunch()
	if err != nil {
		return nil, nil, fmt.Errorf("seek: relaunch after re-auth: %w", err)
	}

	loginState, _ := checkState(newPage)
	if loginState == browser.LoginStateNo {
		if newBr != nil {
			_ = newBr.Close()
		}
		return nil, nil, fmt.Errorf("seek: not authenticated after re-auth — open Settings → Platforms → Connect SEEK to sign in again")
	}

	// Recovery complete. BotStateRunning is set by the caller (launchBrowser)
	// only after it confirms this success — never inside the recovery path itself.
	return newBr, newPage, nil
}

func (b *Bot) warmSeenCache() {
	if b.seenCache == nil {
		b.seenCache = newJobSeenCache()
	}
	n, err := b.seenCache.load(b.cfg.DB, b.cfg.UserID)
	if err != nil {
		b.log.Warn().Err(err).Msg("bot: job seen cache load failed, falling back to per-job DB lookups")
		return
	}
	b.log.Info().Int("known_jobs", n).Msg("bot: job seen cache loaded")
}

func (b *Bot) State() domain.BotState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Bot) markSeekSessionExpired() {
	b.mu.Lock()
	b.seekSessionExpired = true
	b.mu.Unlock()
}

func (b *Bot) isSeekSessionExpired() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seekSessionExpired
}

// bailIfSeekSessionExpired returns true (and transitions the bot to error state)
// when a Seek job page has revealed the session is invalid. Callers in runSeek
// use this to stop the run before more rows pollute jobs_pending_review or
// jobs_skipped.
func (b *Bot) bailIfSeekSessionExpired() bool {
	b.mu.Lock()
	expired := b.seekSessionExpired
	b.mu.Unlock()
	if !expired {
		return false
	}
	b.log.Error().Msg("seek: stopping run — Seek session expired, re-add your Seek session in Settings → Secrets")
	b.mu.Lock()
	b.state = domain.BotStateError
	b.mu.Unlock()
	return true
}

// Start runs the bot loop and blocks until it finishes.
// The caller is responsible for running this in a goroutine if needed.
func (b *Bot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.state == domain.BotStateRunning {
		b.mu.Unlock()
		return fmt.Errorf("bot already running")
	}
	b.stopCh = make(chan struct{})
	b.listingFailureCode = ""
	b.listingFailedJobs = nil
	b.evaluationFailedJobs = nil
	b.automaticStopReason = ""
	b.state = domain.BotStateRunning
	b.mu.Unlock()
	b.run(ctx)
	return nil
}

// Stop signals the bot to stop after the current job.
func (b *Bot) Stop() {
	b.mu.Lock()
	select {
	case <-b.stopCh:
	default:
		close(b.stopCh)
	}
	b.mu.Unlock()
	// Clear pauseCh so IsPaused() returns false after Stop, even if Pause was active.
	// waitIfPaused already holds a copy of the channel and will unblock via stopCh.
	b.pauseMu.Lock()
	b.pauseCh = nil
	b.pauseMu.Unlock()
}

// Pause suspends the bot at the next job boundary (current job completes first).
func (b *Bot) Pause() {
	b.pauseMu.Lock()
	defer b.pauseMu.Unlock()
	if b.pauseCh == nil {
		b.pauseCh = make(chan struct{})
	}
}

// Resume unblocks a paused bot.
func (b *Bot) Resume() {
	b.pauseMu.Lock()
	defer b.pauseMu.Unlock()
	if b.pauseCh != nil {
		close(b.pauseCh)
		b.pauseCh = nil
	}
}

// IsPaused reports whether the bot is currently waiting at a pause point.
func (b *Bot) IsPaused() bool {
	b.pauseMu.Lock()
	defer b.pauseMu.Unlock()
	return b.pauseCh != nil
}

// waitIfPaused blocks at a job boundary until resumed, stopped, or ctx cancelled.
// Deactivates browser billing during the wait and reactivates it on resume.
func (b *Bot) waitIfPaused(ctx context.Context) {
	b.pauseMu.Lock()
	ch := b.pauseCh
	b.pauseMu.Unlock()
	if ch == nil {
		return
	}
	if b.cfg.BrowserMeter != nil {
		b.cfg.BrowserMeter.Deactivate()
	}
	b.log.Info().Msg("bot: paused — waiting for resume")
	var resumed bool
	select {
	case <-ch:
		resumed = true
	case <-b.stopCh:
	case <-ctx.Done():
	}
	if resumed && b.cfg.BrowserMeter != nil {
		if err := b.cfg.BrowserMeter.Activate(ctx); err != nil {
			b.log.Warn().Err(err).Msg("bot: browser meter reactivation after resume failed")
		}
	}
}

// ── Platform runner registry ───────────────────────────────────────────────

// platformRunner is the interface each job-board integration must implement.
// To add a new job site: create a new file (e.g. seek.go), implement this
// interface, and register it with registerRunner in an init() function.
type platformRunner interface {
	run(ctx context.Context, b *Bot)
}

type platformRunnerFunc func(ctx context.Context, b *Bot)

func (f platformRunnerFunc) run(ctx context.Context, b *Bot) { f(ctx, b) }

var runnerRegistry = map[domain.Platform]platformRunner{}

// errAlreadyApplied is returned by easyApply when LinkedIn shows the job was
// already applied to. Callers should record it as applied rather than skipped.
var errAlreadyApplied = fmt.Errorf("already applied")

// ErrNotEasyApply is returned by Manager.ApplyFromURL when the job page does
// not have an Easy Apply / Quick Apply button.
var ErrNotEasyApply = errors.New("not_easy_apply")

// ErrAlreadyApplied is returned by Manager.ApplyFromURL when the DB or the
// job page itself indicates the user already submitted an application.
var ErrAlreadyApplied = errors.New("already_applied")

// ApplyFromURLResult is returned by Manager.ApplyFromURL.
type ApplyFromURLResult struct {
	Company      string
	Role         string
	Score        int
	ScoreReason  string
	JobID        string // set when ScoreWarning=true; use with existing approve/reject endpoints
	ScoreWarning bool   // true = score below threshold; job saved in pending_review for confirmation
}

// detectPlatformFromURL infers the job platform from the URL host.
func detectPlatformFromURL(rawURL string) (domain.Platform, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid job URL")
	}
	host := strings.ToLower(u.Host)
	switch {
	case strings.Contains(host, "linkedin.com"):
		return domain.PlatformLinkedIn, nil
	case strings.Contains(host, "seek.com"):
		return domain.PlatformSeek, nil
	default:
		return "", fmt.Errorf("unsupported job site — only LinkedIn and Seek are supported")
	}
}

func registerRunner(p domain.Platform, r platformRunner) {
	runnerRegistry[p] = r
}

func init() {
	registerRunner(domain.PlatformLinkedIn, platformRunnerFunc(runLinkedIn))
}

// ── main loop ─────────────────────────────────────────────────────────────

func (b *Bot) run(ctx context.Context) {
	defer func() {
		b.mu.Lock()
		if b.state != domain.BotStateError {
			b.state = domain.BotStateStopped
		}
		b.mu.Unlock()
		b.log.Info().Str("platform", string(b.cfg.Platform)).Msg("bot stopped")
	}()

	b.log.Info().Str("platform", string(b.cfg.Platform)).Msg("bot starting")

	runner, ok := runnerRegistry[b.cfg.Platform]
	if !ok {
		b.log.Warn().Str("platform", string(b.cfg.Platform)).Msg("platform not yet supported")
		b.mu.Lock()
		b.state = domain.BotStateError
		b.mu.Unlock()
		return
	}
	runner.run(ctx, b)
}

// ── LinkedIn Easy Apply ────────────────────────────────────────────────────

func runLinkedIn(ctx context.Context, b *Bot) {
	br, page, err := b.launchBrowser(ctx)
	if err != nil {
		b.log.Error().Err(err).Msg("linkedin: launch browser failed")
		return
	}
	defer func() { _ = br.Close() }()
	defer func() {
		if b.cfg.BrowserMeter != nil {
			b.cfg.BrowserMeter.Stop()
		}
	}()

	b.warmSeenCache()

	// Verify session is still valid after browser launch; try stored credentials once.
	if info, e := page.Info(); e == nil {
		u := info.URL
		if strings.Contains(u, "/login") || strings.Contains(u, "/checkpoint") || strings.Contains(u, "/authwall") {
			if recoverErr := b.linkedinEnsureLoggedIn(page); recoverErr != nil {
				b.log.Error().Err(recoverErr).Msg("linkedin: session expired, re-login via Settings → Secrets")
				b.mu.Lock()
				b.state = domain.BotStateError
				b.mu.Unlock()
				return
			}
		}
	}

	// Auth verified — activate browser billing. Stop immediately on ErrExceeded
	// so the user gets a clear quota error rather than continuing with no billing.
	if b.cfg.BrowserMeter != nil {
		if err := b.cfg.BrowserMeter.Activate(ctx); err != nil {
			b.log.Error().Err(err).Msg("linkedin: browser billing activation failed")
			return
		}
	}

	limit := b.cfg.Settings.HumanBehavior.DailyApplicationLimit
	if limit == 0 {
		limit = 40
	}
	appliedToday := b.countAppliedToday()
	b.log.Info().Int("applied_today", appliedToday).Int("limit", limit).Msg("linkedin: starting, daily progress")

	// Process previously approved jobs first.
	if appliedToday < limit {
		appliedToday += b.processApprovedQueue(ctx, br, limit-appliedToday)
	}

	targets := b.cfg.Preferences.EffectiveSearchTargets()
	for _, target := range targets {
		arrangement := searchArrangementLabel(target)
		if arrangement != "" || strings.TrimSpace(target.Location) != "" {
			b.log.Info().Str("location", target.Location).Str("arrangement", arrangement).
				Msg("linkedin: searching location target")
		}

		for _, keyword := range b.cfg.Preferences.Positions {
			b.waitIfPaused(ctx)
			if reason := b.stopReason(ctx); reason != "" {
				b.log.Info().Str("keyword", keyword).Msgf("linkedin: stopped, %s", reason)
				return
			}
			if appliedToday >= limit {
				b.log.Info().Int("limit", limit).Msg("linkedin: stopped, daily application limit reached")
				return
			}
			b.SetKeyword(keyword)
			n := b.processKeyword(ctx, br, page, keyword, limit-appliedToday, target)
			appliedToday += n

			// If no jobs were found, check whether the browser connection was lost
			// (e.g. VPN reset) and attempt a reconnect before continuing.
			if n == 0 && isCDPDead(page) {
				b.log.Warn().Msg("linkedin: browser connection lost, attempting reconnect")
				_ = br.Close()
				newBr, newPage, err := b.launchBrowser(ctx)
				if err != nil {
					b.log.Error().Err(err).Msg("linkedin: reconnect failed, stopping")
					return
				}
				br, page = newBr, newPage
				b.log.Info().Msg("linkedin: browser reconnected, retrying keyword")
				appliedToday += b.processKeyword(ctx, br, page, keyword, limit-appliedToday, target)
			}
		}
	}
	b.SetKeyword("")
	b.log.Info().Int("applied_today", appliedToday).Msg("linkedin: stopped, all location targets and keywords processed")
}

// isCDPDead returns true when the browser's CDP connection is no longer usable
// (e.g. after a VPN reset drops the underlying TCP connection).
// String match is intentional: rod does not expose a typed sentinel for CDP
// disconnection. Validated against go-rod v0.116+ — update this comment if the message changes.
func isCDPDead(page *rod.Page) bool {
	_, err := page.Eval(`() => true`)
	return err != nil && strings.Contains(err.Error(), "closed network connection")
}

// isCDPFatal reports whether err indicates the Chrome WebSocket / underlying
// pipe is dead. After this happens, every subsequent CDP call will fail with
// the same error — looping wastes minutes. Treat it like context.Canceled.
func isCDPFatal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "websocket: close") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "websocket: bad close code")
}

func (b *Bot) launchBrowser(ctx context.Context) (*rod.Browser, *rod.Page, error) {
	// If a remote debug port is configured, connect to the existing Chrome instance.
	if port := b.cfg.Settings.Browser.RemoteDebugPort; port != 0 {
		wsURL := fmt.Sprintf("ws://127.0.0.1:%d/json/version", port)
		u, err := launcher.ResolveURL(wsURL)
		if err != nil {
			b.log.Warn().Err(err).Int("port", port).Msg("remote debug: resolve URL failed, falling back to new browser")
		} else {
			br := rod.New().ControlURL(u)
			if err := br.Connect(); err != nil {
				b.log.Warn().Err(err).Int("port", port).Msg("remote debug: connect failed, falling back to new browser")
			} else {
				b.log.Info().Int("port", port).Msg("browser: connected to existing Chrome via remote debug")
				page, err := br.Page(proto.TargetCreateTarget{URL: "about:blank"})
				if err != nil {
					_ = br.Close()
					return nil, nil, fmt.Errorf("open page: %w", err)
				}
				return br, page, nil
			}
		}
	}

	l := launcher.New().
		Headless(!b.cfg.Settings.Browser.ShowBrowser).
		Set("--disable-blink-features", "AutomationControlled").
		Set("--exclude-switches", "enable-automation")

	// Always use a persistent profile for job platforms so Auth0/localStorage
	// from Connect browser is reused. Path can be overridden in General settings.
	dir := browser.ProfileDir(b.cfg.UserID, string(b.cfg.Platform), b.cfg.Settings.Browser.ChromeProfilePath)
	browser.PrepareChromeProfileDir(dir)
	l = l.UserDataDir(dir)

	u, err := l.Launch()
	if err != nil && strings.Contains(err.Error(), "SingletonLock") {
		b.log.Warn().Err(err).Str("dir", dir).Msg("browser: profile locked, force-unlocking and retrying")
		browser.ForceUnlockChromeProfile(dir)
		u, err = launcher.New().
			Headless(!b.cfg.Settings.Browser.ShowBrowser).
			Set("--disable-blink-features", "AutomationControlled").
			Set("--exclude-switches", "enable-automation").
			UserDataDir(dir).
			Launch()
	}
	if err != nil {
		return nil, nil, fmt.Errorf("launch chrome: %w", err)
	}
	br := rod.New().ControlURL(u)
	if err := br.Connect(); err != nil {
		return nil, nil, fmt.Errorf("connect chrome: %w", err)
	}
	// Set cookies on a blank page BEFORE navigating to the platform,
	// so the session cookies are present on the very first request.
	page, err := br.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		_ = br.Close()
		return nil, nil, fmt.Errorf("open blank page: %w", err)
	}
	if len(b.cfg.Cookies) > 0 {
		if err := page.SetCookies(browser.ToCookieParams(b.cfg.Cookies)); err != nil {
			b.log.Warn().Err(err).Msg("browser: set cookies")
		}
	}
	homeURL := "https://www.linkedin.com"
	if b.cfg.Platform == domain.PlatformSeek {
		homeURL = "https://au.seek.com/jobs"
	}
	if err := page.Navigate(homeURL); err != nil {
		_ = br.Close()
		return nil, nil, fmt.Errorf("navigate %s: %w", b.cfg.Platform, err)
	}
	// Wait for the page to fully load and for Auth0 silent re-auth to complete.
	// Seek uses Auth0 SPA which refreshes the access token via a hidden iframe on
	// first load — navigating away before this finishes leaves the session without
	// an access token and Quick Apply redirects to login.
	_ = page.Timeout(30 * time.Second).WaitLoad()
	_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)

	// Recover a dead session with stored credentials before giving up.
	// For SEEK, only ErrAuthRequired triggers the user re-auth wait. Other errors
	// (network failures, page parsing issues, SEEK outages) are propagated immediately.
	// seekRecoverAuth encapsulates the full recovery logic and is injectable for tests.
	switch b.cfg.Platform {
	case domain.PlatformSeek:
		closeFn := func() { _ = br.Close() }
		if b.seekSeams != nil && b.seekSeams.closeBrowser != nil {
			closeFn = b.seekSeams.closeBrowser
		}
		recoveredBr, recoveredPage, recovErr := b.seekRecoverAuth(ctx, page, closeFn)
		if recovErr != nil {
			return nil, nil, recovErr
		}
		if recoveredBr != nil {
			// Recovery complete: adopt the relaunched browser.
			// BotStateRunning is set HERE — only after seekRecoverAuth confirms
			// a new browser is live and auth has been verified on the fresh page.
			br, page = recoveredBr, recoveredPage
			b.mu.Lock()
			b.state = domain.BotStateRunning
			b.mu.Unlock()
		}
	case domain.PlatformLinkedIn:
		if err := b.linkedinEnsureLoggedIn(page); err != nil {
			b.log.Warn().Err(err).Msg("browser: linkedin session not authenticated after warm-up")
		}
	}

	// Only refresh the saved cookie jar when we positively see a logged-in UI.
	// Persisting a guest jar would wipe a previously-good session.
	if b.cfg.Sessions != nil {
		state, stateErr := browser.PageLoginState(page, string(b.cfg.Platform))
		if browser.ShouldPersistCookies(state, stateErr) {
			result, cookieErr := proto.NetworkGetAllCookies{}.Call(page)
			if cookieErr == nil {
				var fresh []browser.Cookie
				for _, c := range result.Cookies {
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
				if raw, marshalErr := browser.MarshalCookies(fresh); marshalErr != nil {
					b.log.Warn().Err(marshalErr).Msg("browser: failed to marshal refreshed session cookies")
				} else if err := b.cfg.Sessions.Save(b.cfg.UserID, string(b.cfg.Platform), "session", raw); err != nil {
					b.log.Warn().Err(err).Msg("browser: failed to refresh session cookies")
				} else {
					b.log.Debug().Msg("browser: session cookies refreshed after auth warm-up")
				}
			}
		} else {
			b.log.Warn().Str("state", string(state)).Msg("browser: skipping cookie refresh — not confirmed logged in")
		}
	}

	return br, page, nil
}

// filterLinkedInJobs drops jobs already recorded as applied, skipped, top matches, etc.
func (b *Bot) filterLinkedInJobs(jobs []linkedInJob) []linkedInJob {
	fresh := make([]linkedInJob, 0, len(jobs))
	skipped := 0
	for _, job := range jobs {
		if reason := b.alreadyAppliedReason(job.ID); reason != "" {
			skipped++
			b.log.Info().Msgf("linkedin: skip (%s): %q @ %s", reason, job.Title, job.Company)
			continue
		}
		fresh = append(fresh, job)
	}
	if skipped > 0 {
		b.log.Info().Int("skipped_known", skipped).Int("to_process", len(fresh)).Msg("linkedin: filtered known jobs from search results")
	}
	return fresh
}

func (b *Bot) processKeyword(ctx context.Context, br *rod.Browser, page *rod.Page, keyword string, remaining int, target domain.SearchTarget) int {
	jobs, err := b.scrapeLinkedInJobs(ctx, page, keyword, target)
	if err != nil {
		b.log.Error().Err(err).Str("keyword", keyword).Msg("linkedin: scrape jobs failed")
		return 0
	}
	if len(jobs) == 0 {
		b.log.Info().Str("keyword", keyword).Msg("linkedin: no new jobs found for keyword")
		return 0
	}
	jobs = b.filterLinkedInJobs(jobs)
	if len(jobs) == 0 {
		b.log.Info().Str("keyword", keyword).Msg("linkedin: all search results already known, nothing to process")
		return 0
	}
	b.log.Info().Msgf("linkedin: found %d jobs for %q, processing", len(jobs), keyword)
	applied := 0
	for _, job := range jobs {
		b.waitIfPaused(ctx)
		if reason := b.stopReason(ctx); reason != "" {
			b.log.Info().Str("keyword", keyword).Msgf("linkedin: stopped mid-keyword, %s", reason)
			return applied
		}
		if applied >= remaining {
			b.log.Info().Str("keyword", keyword).Int("remaining", remaining).Msg("linkedin: stopped mid-keyword, daily limit reached")
			return applied
		}
		b.humanPause()
		if b.processJob(ctx, br, job) {
			applied++
		}
	}
	b.log.Info().Str("keyword", keyword).Int("applied", applied).Msg("linkedin: keyword done")
	return applied
}

func (b *Bot) stopped(ctx context.Context) bool {
	select {
	case <-b.stopCh:
		return true
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// stopReason returns a human-readable reason the bot should stop, or "".
// Also returns non-empty when the browser billing quota is exhausted.
func (b *Bot) stopReason(ctx context.Context) string {
	select {
	case <-b.stopCh:
		b.mu.Lock()
		reason := b.automaticStopReason
		b.mu.Unlock()
		if reason != "" {
			return reason
		}
		return "manually stopped"
	case <-ctx.Done():
		return "context cancelled: " + ctx.Err().Error()
	default:
	}
	if b.cfg.BrowserMeter != nil {
		if !DestructiveActionAllowed(b.cfg.BrowserMeter) {
			if !b.quotaStopOnce {
				b.quotaStopOnce = true
				meterErr := b.cfg.BrowserMeter.Err()
				failureCode := "browser_quota_exceeded"
				if meterErr != nil && !isJobifaiQuotaExceeded(meterErr) {
					failureCode = "browser_meter_failure"
				}
				b.log.Info().
					Str("event", "quota_stop").
					Str("schema_version", "1").
					Str("automation_run_id", b.cfg.AutomationRunID).
					Str("failure_code", failureCode).
					Msg("run stopped: browser quota or meter failure")
			}
			return "browser automation quota exceeded"
		}
	}
	return ""
}

// ── Job scraping ───────────────────────────────────────────────────────────

type linkedInJob struct {
	ID             string
	Company        string
	Title          string
	Location       string
	URL            string
	PostedDate     string // extracted from listing card time element
	AlreadyApplied bool   // true when LinkedIn shows an "Applied" indicator on the card
	EasyApply      bool   // true when LinkedIn shows an Easy Apply indicator on the card
}

func (b *Bot) scrapeLinkedInJobs(ctx context.Context, page *rod.Page, keyword string, target domain.SearchTarget) ([]linkedInJob, error) {
	b.navigateLinkedInSearch(page, keyword, target)

	cap := b.cfg.Settings.MaxJobsPerKeyword
	if cap <= 0 {
		cap = 25
	}
	fallbackLocation := domain.FormatSearchLocation(target.Location, domain.PlatformLinkedIn)
	jobs := b.collectLinkedInCards(page, cap, fallbackLocation)
	b.log.Info().Msgf("linkedin: search returned %d jobs for %q", len(jobs), keyword)
	return jobs, nil
}

func (b *Bot) navigateLinkedInSearch(page *rod.Page, keyword string, target domain.SearchTarget) {
	searchURL := b.buildLinkedInSearchURL(keyword, target)
	arrangement := searchArrangementLabel(target)
	b.log.Info().
		Str("keyword", keyword).
		Str("location", target.Location).
		Str("arrangement", arrangement).
		Msg("linkedin: searching")
	b.log.Debug().Str("url", redactURL(searchURL)).Msg("linkedin: navigating to search URL")
	if err := page.Timeout(30 * time.Second).Navigate(searchURL); err != nil {
		b.log.Warn().Err(err).Msg("linkedin: navigate timed out, proceeding anyway")
	}
	b.log.Info().Msg("linkedin: navigate done, waiting for load")
	if err := page.Timeout(30 * time.Second).WaitLoad(); err != nil {
		b.log.Warn().Err(err).Msg("linkedin: WaitLoad timed out, proceeding anyway")
	}
	b.log.Info().Msg("linkedin: page ready, scraping cards")
}

func (b *Bot) collectLinkedInCards(page *rod.Page, cap int, fallbackLocation string) []linkedInJob {
	seen := map[string]bool{}
	var jobs []linkedInJob

	const maxPages = 10
	for pageNum := 0; len(jobs) < cap && pageNum < maxPages; pageNum++ {
		if pageNum > 0 {
			b.log.Info().Msgf("linkedin: navigating to page %d", pageNum+1)
			if !clickNextPage(page) {
				b.log.Info().Msg("linkedin: no next page, stopping")
				break
			}
			time.Sleep(3 * time.Second)
		}
		prev := len(jobs)
		jobs = scrollPageCards(page, seen, jobs, cap, fallbackLocation)
		b.log.Info().Msgf("linkedin: page %d yielded %d new jobs", pageNum+1, len(jobs)-prev)
		if len(jobs) == prev {
			break // page yielded nothing new even after scrolling
		}
	}

	if len(jobs) > cap {
		jobs = jobs[:cap]
	}
	return jobs
}

// scrollPageCards scrolls through the current results page collecting cards until stalled or cap reached.
func scrollPageCards(page *rod.Page, seen map[string]bool, jobs []linkedInJob, cap int, fallbackLocation string) []linkedInJob {
	const maxScrolls = 8
	stalled := 0
	for scroll := 0; len(jobs) < cap && scroll < maxScrolls; scroll++ {
		cards := fetchLinkedInCards(page)
		log.Info().Msgf("linkedin: scroll %d, found %d cards", scroll, len(cards))
		prev := len(jobs)
		jobs = deduplicateCards(cards, seen, jobs, fallbackLocation)
		if len(jobs) >= cap || len(cards) == 0 {
			break
		}
		if scroll > 0 && len(jobs) == prev {
			stalled++
			if stalled >= 2 {
				break
			}
		} else {
			stalled = 0
		}
		scrollForMore(page, cards)
		time.Sleep(3 * time.Second)
	}
	return jobs
}

func clickNextPage(page *rod.Page) bool {
	btn, err := page.Timeout(5 * time.Second).Element(`button[aria-label="View next page"]`)
	if err != nil {
		return false
	}
	return btn.Click(proto.InputMouseButtonLeft, 1) == nil
}

func fetchLinkedInCards(page *rod.Page) rod.Elements {
	cards, err := page.Elements(".job-card-container")
	if err != nil || len(cards) == 0 {
		cards, _ = page.Elements("[data-job-id]")
	}
	return cards
}

func deduplicateCards(cards rod.Elements, seen map[string]bool, jobs []linkedInJob, fallbackLocation string) []linkedInJob {
	for _, card := range cards {
		job := extractLinkedInJob(card)
		if job.Location == "" {
			job.Location = fallbackLocation
		}
		if job.ID != "" && !seen[job.ID] {
			seen[job.ID] = true
			jobs = append(jobs, job)
		}
	}
	return jobs
}

func scrollForMore(page *rod.Page, cards rod.Elements) {
	if len(cards) > 0 {
		_ = cards[len(cards)-1].Timeout(3 * time.Second).ScrollIntoView()
	} else {
		_, _ = page.Eval(`() => window.scrollTo(0, document.body.scrollHeight)`)
	}
}

// deduplicateTitle removes the duplicated-line artefact LinkedIn's DOM produces,
// e.g. "Senior Engineer\nSenior Engineer" → "Senior Engineer".
func deduplicateTitle(raw string) string {
	raw = strings.TrimSpace(raw)
	if idx := strings.IndexByte(raw, '\n'); idx != -1 {
		first := strings.TrimSpace(raw[:idx])
		if strings.TrimSpace(raw[idx+1:]) == first {
			return first
		}
	}
	return raw
}

func extractLinkedInJob(el *rod.Element) linkedInJob {
	var job linkedInJob
	if id, err := el.Attribute("data-job-id"); err == nil && id != nil {
		job.ID = *id
	}
	if t, err := el.Element(".job-card-list__title, .job-card-container__link"); err == nil {
		raw, _ := t.Text()
		job.Title = deduplicateTitle(raw)
		if href, e := t.Attribute("href"); e == nil && href != nil {
			job.URL = "https://www.linkedin.com" + *href
		}
	}
	if c, err := el.Element(".job-card-container__company-name, .artdeco-entity-lockup__subtitle"); err == nil {
		job.Company, _ = c.Text()
	}
	if l, err := el.Element(".job-card-container__metadata-wrapper li span"); err == nil {
		job.Location, _ = l.Text()
	} else if l, err := el.Element(".job-card-container__metadata-wrapper li"); err == nil {
		job.Location, _ = l.Text()
	}
	// Extract posting date from the card's time element (datetime attr preferred, text fallback).
	if t, err := el.Element("time[datetime]"); err == nil {
		if dt, e := t.Attribute("datetime"); e == nil && dt != nil && *dt != "" {
			job.PostedDate = *dt
		} else {
			job.PostedDate, _ = t.Text()
		}
	} else if t, err := el.Element(".job-card-container__listdate, .job-card-container__footer-wrapper time"); err == nil {
		job.PostedDate, _ = t.Text()
	}
	// Detect if we've already applied, LinkedIn shows an "Applied" badge on the card.
	// Use only specific selectors, the text fallback is intentionally narrow to avoid
	// false positives from "500+ people applied" or "Easy Apply" text on cards.
	if _, err := el.Element(".job-card-container__footer-job-state, .artdeco-inline-feedback--success"); err == nil {
		job.AlreadyApplied = true
	} else if txt, err := el.Text(); err == nil {
		lower := strings.ToLower(txt)
		if strings.Contains(lower, "you applied") || strings.Contains(lower, "applied on") {
			job.AlreadyApplied = true
		}
	}
	// Easy Apply badge on listing card — detail page re-check is authoritative.
	if _, err := el.Element("[aria-label*='Easy Apply'], .jobs-apply-button--top-card"); err == nil {
		job.EasyApply = true
	}
	return job
}

// ── Job processing ─────────────────────────────────────────────────────────

func (b *Bot) processJob(ctx context.Context, br *rod.Browser, job linkedInJob) bool {
	return b.processLinkedInJob(ctx, br, job, false)
}

func (b *Bot) processLinkedInJob(ctx context.Context, br *rod.Browser, job linkedInJob, reevaluate bool) bool {
	b.log.Info().Str("job_id", job.ID).Str("url", redactURL(job.URL)).Str("role", job.Title).Str("company", job.Company).Msgf("linkedin: processing, %q @ %s", job.Title, job.Company)

	if reason := b.alreadyAppliedReason(job.ID); reason != "" && !(reevaluate && (reason == "in approved queue" || reason == "in Top Matches")) {
		b.log.Info().Str("job_id", job.ID).Msgf("linkedin: skip (%s): %q @ %s", reason, job.Title, job.Company)
		return false
	}
	if !reevaluate && b.alreadyQueued(ctx, job.Company, job.Title) {
		b.log.Info().Str("job_id", job.ID).Msgf("linkedin: skip, duplicate listing already queued: %q @ %s", job.Title, job.Company)
		return false
	}
	// Card-level applied indicator (LinkedIn shows "Applied" badge on already-applied cards).
	// Use recordSkipped rather than recordApplied: the bot did not submit an application here.
	// This also avoids false positives from browser crashes leaving phantom "Applied" badges.
	if job.AlreadyApplied {
		b.recordSkipped(job, "already_applied_indicator", 0, "", nil)
		b.log.Info().Str("company", job.Company).Str("role", job.Title).Str("job_id", job.ID).Msg("linkedin: card shows applied badge, recording to skipped")
		return false
	}
	if b.isBlacklisted(job) {
		b.recordSkipped(job, "blacklisted", 0, "", nil)
		b.log.Info().Str("job_id", job.ID).Msgf("linkedin: skip, blacklisted: %q @ %s", job.Title, job.Company)
		return false
	}
	if !b.cfg.Preferences.ExperienceLevel.MatchesExperienceTitle(job.Title) {
		b.recordSkipped(job, "experience_level", 0, "", nil)
		b.log.Info().Str("job_id", job.ID).Msgf("linkedin: skip, experience level: %q @ %s", job.Title, job.Company)
		return false
	}

	// Open the job page — authoritative Easy Apply detection and description extraction.
	// No HTTP fallback: the browser page is the sole content source. An HTTP fetch would
	// follow redirects without identity checks and could return a collections page description,
	// corrupting scores or causing wrong-job apply attempts.
	var details scraper.JobDetails
	jobPage, pageErr := br.Page(proto.TargetCreateTarget{URL: job.URL})
	if pageErr != nil {
		b.log.Info().Str("error", sanitizeErr(pageErr)).
			Str("job_id", job.ID).Str("url", redactURL(job.URL)).
			Str("role", job.Title).Str("company", job.Company).
			Msg("linkedin: browser page open failed — skipping")
		b.preserveLinkedInReview(ctx, job, 0, "Not scored: browser page could not be opened.", fmt.Errorf("%w: %v", errLinkedInUnknown, pageErr))
		return false
	}
	_ = jobPage.Timeout(30 * time.Second).WaitLoad()
	resolution, resolveErr := waitLinkedInJob(ctx, jobPage, linkedInExpectedID(job.URL), job)
	logLinkedInResolution(resolution, job, "", "classification")
	// A missing/ambiguous CTA does not invalidate verified job content. Score
	// that content normally, but never try to apply while the control is unknown.
	controlUnverified := resolveErr != nil && resolution.IdentityVerified && errors.Is(resolveErr, errLinkedInUnknown)
	if resolveErr != nil && !controlUnverified {
		_ = jobPage.Close()
		if ctx.Err() == nil {
			b.preserveLinkedInReview(ctx, job, 0, "Listing identity or application control could not be verified; suitability has not been evaluated.", resolveErr)
		}
		return false
	}
	if resolution.Kind == "already_applied" {
		_ = jobPage.Close()
		b.recordSkipped(job, "already_applied_indicator", 0, "", nil)
		return false
	}
	controlError := resolveErr
	resolution, resolveErr = readLinkedInDescription(ctx, jobPage, job)
	if resolveErr != nil {
		_ = jobPage.Close()
		if ctx.Err() == nil {
			b.preserveLinkedInReview(ctx, job, 0, "", resolveErr)
		}
		return false
	}
	details.Description = resolution.Description
	if resolution.Kind == "already_applied" {
		_ = jobPage.Close()
		b.recordSkipped(job, "already_applied_indicator", 0, "", nil)
		return false
	}
	easyApply := resolution.Kind == "native"
	controlUnverified = resolution.Kind == "unknown"
	if controlUnverified && controlError == nil {
		controlError = resolution.resolutionError()
	}
	_ = jobPage.Close()
	if err := listingEvaluationError(strings.TrimSpace(details.Description), b.cfg.Scorer); err != nil {
		b.recordListingFailure(ctx, domain.PlatformLinkedIn, job, 0, "", nil, err)
		return false
	}
	if !controlUnverified {
		b.noteListingHealthy()
	}
	if details.PostedDate == "" {
		details.PostedDate = job.PostedDate
	}
	if controlUnverified {
		b.log.Warn().Err(controlError).Str("job_id", job.ID).Str("url", redactURL(job.URL)).
			Msg("linkedin: application control unverified; evaluating verified listing without applying")
	} else if easyApply {
		b.log.Info().Msgf("linkedin: Easy Apply detected: %q @ %s", job.Title, job.Company)
	} else {
		b.log.Info().Msgf("linkedin: not Easy Apply, will route to Top Matches: %q @ %s", job.Title, job.Company)
	}

	llmBefore := b.llmSnapshot()
	evaluationText := listingScoringContext(job, details.Description, resolution.HeaderText)
	score, reasoning, halalVerdict, ok := b.checkScore(ctx, job, evaluationText)
	if !ok {
		return false
	}
	if controlUnverified {
		b.preserveLinkedInReviewWithVerdict(ctx, job, score, reasoning, halalVerdict, controlError)
		return false
	}

	// Docs are generated lazily at the file-upload step, only if the toggle is on
	// and a file field is actually encountered during form filling.
	lazy := &lazyDocGen{b: b, ctx: ctx, job: job, jobDesc: evaluationText}

	if b.cfg.RequireReview {
		packJSON := documents.WriteApplicationPackJSON(documents.ApplicationDocumentPack{
			HoldReason: requireReviewHoldReason(easyApply),
		})
		b.queueForReview(ctx, &domain.PendingReview{
			JobID:                job.ID,
			Company:              job.Company,
			Role:                 job.Title,
			Location:             job.Location,
			Platform:             domain.PlatformLinkedIn,
			Link:                 job.URL,
			DocumentRefsJSON:     packJSON,
			SuitabilityScore:     score,
			SuitabilityReasoning: reasoning,
			DueDate:              details.DueDate,
			PostedDate:           details.PostedDate,
			EasyApply:            easyApply,
			HalalVerdict:         unmarshalHalalVerdict(halalVerdict),
			CreatedAt:            time.Now(),
		})
		return false
	}

	if !easyApply {
		b.queueForReview(ctx, &domain.PendingReview{
			JobID:                job.ID,
			Company:              job.Company,
			Role:                 job.Title,
			Location:             job.Location,
			Platform:             domain.PlatformLinkedIn,
			Link:                 job.URL,
			ResumePath:           "",
			CoverLetterPath:      "",
			SuitabilityScore:     score,
			SuitabilityReasoning: reasoning,
			DueDate:              details.DueDate,
			PostedDate:           details.PostedDate,
			EasyApply:            false,
			HalalVerdict:         unmarshalHalalVerdict(halalVerdict),
			CreatedAt:            time.Now(),
		})
		b.log.Info().Str("job_id", job.ID).Str("url", redactURL(job.URL)).Str("role", job.Title).Str("company", job.Company).Msgf("linkedin: routed to Top Matches (not Easy Apply): %q @ %s", job.Title, job.Company)
		return false
	}

	return b.submitEasyApply(ctx, br, job, lazy, score, reasoning, halalVerdict, llmBefore)
}

// linkedInPageApplied returns true when the open job detail page shows an "Applied" indicator,
// meaning the user has already applied to this job manually or in a prior bot run.
// Uses a single JS eval round-trip instead of per-selector Element() calls (which each block
// for 3 s on a miss) — eliminates an 18-second delay on fresh unapplied jobs in the new UI.
func (b *Bot) linkedInPageApplied(page *rod.Page, expectedID string) bool {
	r, err := resolveLinkedInJob(page, expectedID, false)
	return err == nil && r.IdentityVerified && r.Kind == "already_applied"
}

// detectLinkedInEasyApply returns true when the job details pane shows LinkedIn's
// Easy Apply CTA. This follows LinkedIn's UI contract — not job-specific heuristics:
//
//	Easy Apply  → accessible name contains "Easy Apply" (or "LinkedIn Apply …")
//	External    → primary CTA is plain "Apply" / "Apply on company website"
//
// Only the main job pane is considered (similar-jobs / aside rails are ignored).
// requireReviewHoldReason returns the hold_reason for RequireReview queue entries.
// Native Easy Apply listings prompt the user to scan the apply form; external
// listings redirect to the employer's site and need no form-scan step.
func requireReviewHoldReason(easyApply bool) string {
	if easyApply {
		return "Use Prepare documents to scan the apply form and build your application pack before approving"
	}
	return "Apply on the employer's website."
}

func detectLinkedInEasyApply(page *rod.Page) bool {
	info, err := page.Info()
	if err != nil {
		return false
	}
	r, err := resolveLinkedInJob(page, linkedInExpectedID(info.URL), false)
	return err == nil && r.Kind == "native"
}

func (b *Bot) llmCtx(ctx context.Context, task, jobID string) context.Context {
	attr := domain.LLMCallContext{
		UserID:          b.cfg.UserID,
		AutomationRunID: b.cfg.AutomationRunID,
		JobID:           jobID,
	}
	if jobID != "" {
		attr.ApplicationID = jobID
	} else if b.cfg.ApplicationID != "" {
		attr.ApplicationID = b.cfg.ApplicationID
	}
	ctx = llm.AttachAttribution(ctx, attr)
	return llm.WithTask(ctx, task)
}

func (b *Bot) fetchJob(ctx context.Context, job linkedInJob) scraper.JobDetails {
	details, err := scraper.FetchJob(ctx, job.URL)
	if err != nil {
		b.log.Warn().Err(err).Msg("linkedin: fetch job desc")
		return scraper.JobDetails{Description: job.Title + " at " + job.Company}
	}
	return details
}

func (b *Bot) checkScore(ctx context.Context, job linkedInJob, jobDesc string) (score int, reasoning string, halalVerdict []byte, ok bool) {
	minScore := b.cfg.Settings.JobSuitabilityScore
	if minScore == 0 {
		minScore = 6
	}
	if b.cfg.Scorer == nil {
		return 0, "", nil, false
	}
	result, err := b.cfg.Scorer.EvaluateJob(b.llmCtx(ctx, "evaluate job", job.ID), b.currentProfile(), jobDesc)
	if err != nil {
		if ctx.Err() == nil {
			b.recordListingFailure(ctx, domain.PlatformLinkedIn, job, 0, "", nil, fmt.Errorf("%w: %w", errJobEvaluationFailed, err))
		}
		b.handleEvaluationFailure(ctx, domain.PlatformLinkedIn, job.ID, err)
		return 0, "", nil, false
	}
	if result.Score < minScore {
		b.resetEvaluationFailures()
		b.recordSkipped(job, fmt.Sprintf("score %d < %d", result.Score, minScore), result.Score, result.Reasoning, nil)
		b.log.Info().Str("job_id", job.ID).Str("url", redactURL(job.URL)).Int("score", result.Score).Int("min_score", minScore).Msgf("linkedin: skip, score %d < %d for %q @ %s", result.Score, minScore, job.Title, job.Company)
		return result.Score, result.Reasoning, nil, false
	}
	b.log.Info().Str("job_id", job.ID).Str("url", redactURL(job.URL)).Int("score", result.Score).Msgf("linkedin: score %d/%d, %q @ %s", result.Score, 10, job.Title, job.Company)

	// Halal check, only runs after score passes to avoid wasted LLM calls.
	// HARAM → skip; DOUBTFUL → let through but carry verdict for storage.
	if b.cfg.HalalChecker != nil {
		verdict, err := b.cfg.HalalChecker.CheckHalal(b.llmCtx(ctx, "halal check", job.ID), job.Title, job.Company, jobDesc)
		if err != nil {
			if ctx.Err() == nil {
				b.recordListingFailure(ctx, domain.PlatformLinkedIn, job, result.Score, result.Reasoning, nil, fmt.Errorf("%w: halal check: %w", errJobEvaluationFailed, err))
			}
			b.handleEvaluationFailure(ctx, domain.PlatformLinkedIn, job.ID, err)
			return result.Score, result.Reasoning, nil, false
		}
		b.resetEvaluationFailures()
		if verdict.Verdict == "HARAM" {
			verdictJSON, _ := json.Marshal(verdict)
			b.recordSkipped(job, "halal filter", result.Score, result.Reasoning, verdictJSON)
			b.log.Info().Str("job_id", job.ID).Str("url", redactURL(job.URL)).Msgf("linkedin: halal skip (HARAM) %q @ %s", job.Title, job.Company)
			return result.Score, result.Reasoning, nil, false
		} else if verdict.Verdict == "DOUBTFUL" {
			halalVerdict, _ = json.Marshal(verdict)
			b.log.Info().Str("job_id", job.ID).Str("url", redactURL(job.URL)).Msgf("linkedin: halal DOUBTFUL, letting through %q @ %s", job.Title, job.Company)
		}
	}

	b.resetEvaluationFailures()
	return result.Score, result.Reasoning, halalVerdict, true
}

func isJobifaiQuotaExceeded(err error) bool {
	return err != nil && errors.Is(err, quota.ErrExceeded)
}

func isProviderUsageLimit(err error) bool {
	if err == nil || isJobifaiQuotaExceeded(err) {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "429") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "quota exceeded")
}

// isFormFillFatalLLM is true when form filling must abort (no fallback answers, no Continue/Submit).
func isFormFillFatalLLM(err error) bool {
	return isJobifaiQuotaExceeded(err) || isProviderUsageLimit(err)
}

func llmAbortReason(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case isJobifaiQuotaExceeded(err):
		return "LLM quota exhausted — trial or plan credits are used up. Add credits or upgrade in Settings → Plan, then restart the bot."
	case isProviderUsageLimit(err):
		return "LLM provider usage or rate limit reached — check your provider account, API billing, or model limits in Settings → LLM (this is not Jobifai plan credits)."
	case strings.Contains(msg, "401") || strings.Contains(msg, "Jwt is expired") || strings.Contains(msg, "LOGIN_FAILED"):
		return "LLM authentication failed — the proxy JWT has expired or the API key is invalid. Restart the LLM proxy to refresh credentials."
	case strings.Contains(msg, "connection refused"):
		return "LLM proxy is not running — connection refused. Start the proxy at the configured address."
	case strings.Contains(msg, "502") || strings.Contains(msg, "503"):
		return "LLM service is unavailable (502/503)."
	default:
		return fmt.Sprintf("LLM call failed: %v", err)
	}
}

func (b *Bot) abortOnLLMFailure(err error) {
	reason := llmAbortReason(err)
	if reason == "" {
		reason = fmt.Sprintf("LLM call failed: %v", err)
	}
	b.log.Error().Msgf("bot: aborting — %s", reason)
	b.mu.Lock()
	b.automaticStopReason = reason
	b.state = domain.BotStateError
	b.mu.Unlock()
	b.Stop()
}

func (b *Bot) llmSnapshot() llm.UsageSnapshot {
	if b.cfg.LLMTracker == nil {
		return llm.UsageSnapshot{}
	}
	return b.cfg.LLMTracker.Snapshot()
}

func (b *Bot) logApplied(attemptID, jobID, title, company string, platform domain.Platform, before llm.UsageSnapshot) {
	ev := log.Info().
		Str("event", "applied").
		Str("attempt_id", attemptID).
		Str("job_id", jobID).
		Str("title", title).
		Str("company", company).
		Str("platform", string(platform))
	if b.cfg.LLMTracker != nil {
		after := b.cfg.LLMTracker.Snapshot()
		in := after.InputTokens - before.InputTokens
		out := after.OutputTokens - before.OutputTokens
		ev.Msgf("applied ✓  %s @ %s  [%s]  llm: %d in + %d out tokens", title, company, platform, in, out)
	} else {
		ev.Msgf("applied ✓  %s @ %s  [%s]", title, company, platform)
	}
}

func (b *Bot) loadMarket() *resume.MarketPrompts {
	if b.cfg.MarketDir == "" || b.cfg.Settings.DefaultResumeMarket == "" {
		return nil
	}
	return resume.LoadMarketByName(b.cfg.MarketDir, b.cfg.Settings.DefaultResumeMarket)
}

func (b *Bot) generateDocs(ctx context.Context, job linkedInJob, jobDesc string) (resumePath, coverPath string) {
	market := b.loadMarket()
	profile := b.tailoredProfile(ctx, job.ID, jobDesc, market)
	if b.cfg.Renderer == nil || profile == nil {
		return
	}
	cssOverride := ""
	if market != nil && market.CSSFile != "" {
		cssOverride = market.CSSFile
	}
	var renderOpts *resume.RenderOptions
	if market != nil {
		renderOpts = &resume.RenderOptions{SectionLabels: market.LabelsOrDefault()}
	}
	if pdf, err := b.cfg.Renderer.RenderResume(b.llmCtx(ctx, "render resume", job.ID), profile, "", cssOverride, renderOpts); err == nil {
		resumePath = b.savePDF(pdf, job.Company, job.Title, "resume")
	}
	coverPath = b.generateCoverLetter(ctx, profile, job, jobDesc, market, cssOverride)
	return
}

// currentProfile returns the latest profile, reloading from DB if a ProfileLoader is set.
func (b *Bot) currentProfile() *domain.ResumeProfile {
	if b.cfg.ProfileLoader != nil {
		if p := b.cfg.ProfileLoader(); p != nil {
			return p
		}
	}
	return b.cfg.Profile
}

func (b *Bot) tailoredProfile(ctx context.Context, jobID, jobDesc string, market *resume.MarketPrompts) *domain.ResumeProfile {
	profile := b.currentProfile()
	if b.cfg.Tailor == nil || profile == nil {
		return profile
	}
	promptCtx := jobDesc
	if market != nil && market.TailoredPrompt != "" {
		promptCtx = market.TailoredPrompt + "\n\nJob Description:\n" + jobDesc
	}
	tailored, err := b.cfg.Tailor.TailorProfile(b.llmCtx(ctx, "tailor resume", jobID), profile, promptCtx)
	if err != nil {
		b.log.Warn().Err(err).Msg("linkedin: tailoring failed, using base profile")
		return profile
	}
	return tailored
}

func (b *Bot) generateCoverLetter(ctx context.Context, profile *domain.ResumeProfile, job linkedInJob, jobDesc string, market *resume.MarketPrompts, cssOverride string) string {
	if b.cfg.Tailor == nil || b.cfg.Renderer == nil {
		return ""
	}
	promptCtx := jobDesc
	if market != nil && market.CoverLetterPrompt != "" {
		promptCtx = market.CoverLetterPrompt + "\n\nJob Description:\n" + jobDesc
	}
	body, err := b.cfg.Tailor.WriteCoverLetter(b.llmCtx(ctx, "cover letter", job.ID), profile, promptCtx, b.cfg.CoverLetterTone)
	if err != nil {
		return ""
	}
	pdf, err := b.cfg.Renderer.RenderCoverLetter(b.llmCtx(ctx, "render cover letter", job.ID), body, "", cssOverride)
	if err != nil {
		return ""
	}
	return b.savePDF(pdf, job.Company, job.Title, "cover_letter")
}

func (b *Bot) queueForReview(ctx context.Context, p *domain.PendingReview) {
	b.savePendingReview(ctx, p)
	if p.EasyApply {
		b.log.Info().Msgf("linkedin: queued for review, %q @ %s", p.Role, p.Company)
	} else {
		b.log.Info().Msgf("linkedin: added to Top Matches (manual apply), %q @ %s", p.Role, p.Company)
	}
}

func (b *Bot) submitEasyApply(ctx context.Context, br *rod.Browser, job linkedInJob, lazy *lazyDocGen, score int, reasoning string, halalVerdict []byte, llmBefore llm.UsageSnapshot) bool {
	ac := newAttemptContext(b.cfg.UserID, string(domain.PlatformLinkedIn), job.ID, job.URL, job.Title, job.Company, "automated_linkedin", b.cfg.AutomationRunID)
	beginAttempt(b.cfg.DB, ac)
	release, err := b.beginDocumentWork(ctx)
	if err != nil {
		b.log.Warn().Err(err).Str("attempt_id", ac.AttemptID).Str("job_id", job.ID).Str("url", redactURL(job.URL)).Msg("linkedin: document work guard")
		finishAttempt(b.cfg.DB, ac, OutcomeFailed, "document_work_guard", "begin_work_failed", err.Error())
		return false
	}
	defer release()
	if lazy != nil {
		lazy.attemptID = ac.AttemptID
	}
	jobPage, err := br.Page(proto.TargetCreateTarget{URL: job.URL})
	if err != nil {
		b.log.Error().Str("error", sanitizeErr(err)).Str("attempt_id", ac.AttemptID).Str("job_id", job.ID).Str("url", redactURL(job.URL)).Str("role", job.Title).Msg("linkedin: open job page")
		finishAttempt(b.cfg.DB, ac, classifyApplyOutcome(err), "open_page", "browser_error", err.Error())
		return false
	}
	defer func() { _ = jobPage.Close() }()

	if err := b.easyApply(ctx, jobPage, lazy); err != nil {
		if errors.Is(err, errAlreadyApplied) {
			resume, cover := lazy.get()
			b.recordApplied(job, resume, cover, lazy.resumeVersionID, lazy.coverVersionID, lazy.refsJSON, score, halalVerdict)
			b.log.Info().Str("attempt_id", ac.AttemptID).Str("job_id", job.ID).Str("company", job.Company).Str("role", job.Title).Msg("linkedin: already applied, recorded ✓")
			finishAttempt(b.cfg.DB, ac, OutcomeAlreadyApplied, "already_applied", "already_applied", "")
			return true
		}
		if linkedInNeedsReview(err) {
			_, _, reviewCode := linkedInReviewOutcome(err)
			b.log.Info().
				Str("event", "user_action_required").
				Str("schema_version", "1").
				Str("attempt_id", ac.AttemptID).
				Str("job_id", job.ID).
				Str("platform", string(domain.PlatformLinkedIn)).
				Str("reason_code", reviewCode).
				Msg("user action required: LinkedIn review needed")
			b.preserveLinkedInReview(ctx, job, score, reasoning, err)
			outcome, stage, code := linkedInReviewOutcome(err)
			finishAttempt(b.cfg.DB, ac, outcome, stage, code, err.Error())
			return false
		}
		b.log.Error().Err(err).Str("attempt_id", ac.AttemptID).Str("job_id", job.ID).Str("url", redactURL(job.URL)).Str("role", job.Title).Msg("linkedin: easy apply failed")
		reason := "easy apply: " + err.Error()
		if skipReasonBelongsInTopMatches(reason) {
			b.savePendingReview(ctx, &domain.PendingReview{
				JobID:                job.ID,
				Company:              job.Company,
				Role:                 job.Title,
				Location:             job.Location,
				Platform:             domain.PlatformLinkedIn,
				Link:                 job.URL,
				SuitabilityScore:     score,
				SuitabilityReasoning: reasoning,
				EasyApply:            false,
				HalalVerdict:         unmarshalHalalVerdict(halalVerdict),
				CreatedAt:            time.Now(),
			})
			b.log.Info().Str("attempt_id", ac.AttemptID).Str("job_id", job.ID).Str("url", redactURL(job.URL)).Str("reason", reason).Msgf("linkedin: routed to Top Matches (not automatable): %q @ %s", job.Title, job.Company)
			finishAttempt(b.cfg.DB, ac, OutcomeBlocked, "apply", "not_automatable", reason)
			return false
		}
		b.recordSkipped(job, reason, score, reasoning, nil)
		finishAttempt(b.cfg.DB, ac, classifyApplyOutcome(err), "apply", "apply_error", err.Error())
		return false
	}
	// Post-apply identity check — fail-closed: an unreadable or non-job-view URL after
	// submission is treated as submission-unknown. Uses declared job.URL (not live URL)
	// so URL transforms during navigation cannot mask wrong-job submissions.
	if err := verifyLinkedInCompletion(jobPage, job.URL, job.Company); err != nil {
		b.log.Error().Err(err).
			Str("attempt_id", ac.AttemptID).
			Str("job_id", job.ID).
			Str("intended_url", job.URL).
			Msg("linkedin: post-apply identity check failed — submission needs reconciliation")
		b.recordListingFailure(ctx, domain.PlatformLinkedIn, job, score, reasoning, halalVerdict, fmt.Errorf("%w: %v", errLinkedInSubmissionUnknown, err))
		finishAttempt(b.cfg.DB, ac, OutcomeSubmissionUnknown, "submitted", "job_identity_drift", err.Error())
		return false
	}
	resume, cover := lazy.get()
	b.recordApplied(job, resume, cover, lazy.resumeVersionID, lazy.coverVersionID, lazy.refsJSON, score, halalVerdict)
	b.logApplied(ac.AttemptID, job.ID, job.Title, job.Company, domain.PlatformLinkedIn, llmBefore)
	finishAttempt(b.cfg.DB, ac, OutcomeSubmittedConfirmed, "submitted", "", "")
	return true
}

// ── Easy Apply modal navigation ────────────────────────────────────────────

func (b *Bot) easyApply(ctx context.Context, page *rod.Page, lazy *lazyDocGen) (retErr error) {
	if lazy == nil {
		return fmt.Errorf("%w: missing declared job", errLinkedInIdentity)
	}
	if err := unresolvedSubmission(b.cfg.DB, b.cfg.UserID, lazy.job.ID, lazy.job.URL, lazy.attemptID); err != nil {
		return fmt.Errorf("%w: %v", errLinkedInSubmissionUnknown, err)
	}
	submitClicked := false
	defer func() {
		if submitClicked && retErr != nil {
			retErr = fmt.Errorf("%w: %v", errLinkedInSubmissionUnknown, retErr)
		}
	}()
	// Only wait for load if the page hasn't already fired its load event.
	// Calling WaitLoad() on an already-loaded page blocks forever.
	if ready, err := page.Eval(`() => document.readyState`); err != nil || ready.Value.String() != "complete" {
		if err := page.Timeout(30 * time.Second).WaitLoad(); err != nil {
			return fmt.Errorf("wait load: %w", err)
		}
	}

	if info, err := page.Eval(`() => window.location.href`); err == nil {
		b.log.Info().Str("url", redactURL(info.Value.String())).Msg("easy apply: page URL after load")
	}

	// Entry identity check — before any CTA interaction or preparation scan.
	// This also protects the reviewPrepareOnly branch below.
	expectedJobPath := linkedInJobPath(lazy.job.URL)
	resolution, err := waitLinkedInJob(ctx, page, linkedInExpectedID(lazy.job.URL), lazy.job)
	logLinkedInResolution(resolution, lazy.job, lazy.attemptID, "before_initial_click")
	if err != nil {
		return err
	}
	if resolution.Kind == "already_applied" {
		if err := verifyJobIdentity(page, expectedJobPath); err != nil {
			return err
		}
		return errAlreadyApplied
	}
	if err := resolution.resolutionError(); err != nil {
		return err
	}
	clickCTA := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if b.cfg.BrowserMeter != nil && b.cfg.BrowserMeter.Err() != nil {
			return b.cfg.BrowserMeter.Err()
		}
		r, err := resolveLinkedInJob(page, linkedInExpectedID(lazy.job.URL), true, lazy.job)
		logLinkedInResolution(r, lazy.job, lazy.attemptID, "cta_click")
		if err != nil {
			return err
		}
		if err := r.resolutionError(); err != nil {
			return err
		}
		if r.Action != "clicked" {
			return errLinkedInUnknown
		}
		return nil
	}
	if err := clickCTA(); err != nil {
		return err
	}
	b.log.Info().Str("attempt_id", lazy.attemptID).Str("job_id", lazy.job.ID).Str("url", redactURL(lazy.job.URL)).Msg("easy apply: verified job button clicked")

	// Wait for the Easy Apply modal. LinkedIn SDUI may render it in an iframe /
	// shadow root without classic .jobs-easy-apply-* class names. Detect by
	// apply-specific chrome OR a non-messaging dialog that looks like an apply form.
	const jsModalPresent = `() => {
		function allInDOM(root, selector) {
			const r = [];
			try {
				r.push(...root.querySelectorAll(selector));
				for (const el of root.querySelectorAll('*')) {
					if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
					if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
						try {
							const d = el.contentDocument;
							if (d) r.push(...allInDOM(d, selector));
						} catch (e) {}
					}
				}
			} catch(e) {}
			return r;
		}
		function looksLikeMessaging(el) {
			if (!el) return false;
			try {
				if (el.closest && el.closest('.msg-overlay-conversation-bubble, .msg-overlay-list-bubble, .msg-form, .msg-conversations-container, .msg-overlay, [data-test-messaging]')) return true;
			} catch(e) {}
			const cls = (el.className && el.className.toString && el.className.toString() || '').toLowerCase();
			if (cls.includes('msg-overlay') || cls.includes('msg-form') || cls.includes('msg-conversations')) return true;
			const t = (el.getAttribute('aria-label') || '').toLowerCase();
			return t.includes('conversation with') || t.includes('new message');
		}
		function btnText(b) {
			return ((b.getAttribute('aria-label') || '') + ' ' + (b.textContent || '')).toLowerCase().replace(/\s+/g, ' ').trim();
		}
		function isApplyActionBtn(b) {
			if (looksLikeMessaging(b)) return false;
			const t = btnText(b);
			if (t.includes('conversation') || t.includes('messaging') || t.includes('close your')) return false;
			if (b.hasAttribute('data-live-test-easy-apply-next-button') || b.hasAttribute('data-easy-apply-next-button')) return true;
			if (b.hasAttribute('data-live-test-easy-apply-submit-button') || b.hasAttribute('data-easy-apply-submit-button')) return true;
			if (t === 'next' || t.startsWith('next ')) return true;
			if (t === 'continue' || t.startsWith('continue ') || t.includes('continue to next step')) return true;
			if (t.includes('review your application') || t === 'review' || t.startsWith('review ')) return true;
			if (t.includes('submit application') || t === 'submit' || t.startsWith('submit ')) return true;
			return false;
		}

		// Classic Easy Apply containers (legacy + current).
		const preferred = [
			'[data-test-easy-apply-modal]',
			'.jobs-easy-apply-modal',
			'.jobs-easy-apply-content',
			'[class*="jobs-easy-apply"]',
			'[data-live-test-easy-apply-next-button]',
			'[data-easy-apply-next-button]',
			'[data-live-test-easy-apply-submit-button]',
			'[data-test-form-element]',
		];
		for (const sel of preferred) {
			if (allInDOM(document, sel).some(el => !looksLikeMessaging(el))) return true;
		}

		// Distinctive apply-modal chrome visible in body text (works even when
		// LinkedIn drops jobs-easy-apply-* class names / uses opaque wrappers).
		const pageText = (document.body && document.body.innerText || '').toLowerCase();
		const hasPager = /\b\d+\s*\/\s*\d+\s+pages?\b/.test(pageText);
		const hasContact = pageText.includes('contact info') || pageText.includes('contact information');
		const hasApplyTo = /\bapply to\b/.test(pageText);
		const hasResumeStep = pageText.includes('resume') && (pageText.includes('cover letter') || hasPager);
		const applyBtns = allInDOM(document, 'button, [role="button"]').filter(isApplyActionBtn);
		if (applyBtns.length && (hasPager || hasContact || hasResumeStep || (hasApplyTo && hasContact))) return true;

		// Any non-messaging dialog/modal that contains an apply action button.
		const shells = allInDOM(document, '[role="dialog"], .artdeco-modal, [data-test-modal]');
		for (const shell of shells) {
			if (looksLikeMessaging(shell)) continue;
			if (allInDOM(shell, 'button, [role="button"]').some(isApplyActionBtn)) return true;
			const t = (shell.innerText || '').toLowerCase();
			if (t.includes('contact info') || /\b\d+\s*\/\s*\d+\s+pages?\b/.test(t) || t.includes('apply to')) return true;
		}
		return false;
	}`

	const jsModalDiag = `() => {
		function allInDOM(root, selector) {
			const r = [];
			try {
				r.push(...root.querySelectorAll(selector));
				for (const el of root.querySelectorAll('*')) {
					if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
					if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
						try { const d = el.contentDocument; if (d) r.push(...allInDOM(d, selector)); } catch (e) {}
					}
				}
			} catch(e) {}
			return r;
		}
		const dialogs = allInDOM(document, '[role="dialog"], .artdeco-modal');
		return JSON.stringify({
			url: location.href,
			iframes: allInDOM(document, 'iframe').length,
			dialogs: dialogs.length,
			easyApplyNodes: allInDOM(document, '.jobs-easy-apply-modal, .jobs-easy-apply-content, [data-test-easy-apply-modal], [class*="jobs-easy-apply"]').length,
			dialogSnippets: dialogs.slice(0, 3).map(d => ({
				cls: (d.className && d.className.toString && d.className.toString() || '').slice(0, 80),
				label: (d.getAttribute('aria-label') || '').slice(0, 80),
				text: (d.innerText || '').replace(/\s+/g, ' ').trim().slice(0, 120),
			})),
		});
	}`

	modalDeadline := time.Now().Add(25 * time.Second)
	modalFound := false
	retriedClick := false
	for time.Now().Before(modalDeadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := verifyJobIdentity(page, expectedJobPath); err != nil {
			return err
		}
		if res, err := page.Eval(jsModalPresent); err == nil && res.Value.Bool() {
			modalFound = true
			break
		}
		// Mid-wait: if nothing appeared after ~8s, click Easy Apply again once.
		if !retriedClick && time.Until(modalDeadline) < 17*time.Second {
			b.log.Warn().Msg("easy apply: modal not visible yet, re-clicking Easy Apply")
			if err := clickCTA(); err != nil {
				return err
			}
			retriedClick = true
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !modalFound {
		if diag, err := page.Eval(jsModalDiag); err == nil {
			b.log.Warn().Str("diag", diag.Value.String()).Msg("easy apply: modal did not appear")
		}
		saveApplyDebug(page, "no_modal")
		return fmt.Errorf("easy apply modal did not appear after click (LinkedIn may have changed the apply UI or blocked the session)")
	}

	// Extra settle time after modal appears.
	time.Sleep(1 * time.Second)

	// Post-modal identity check — verifies the CTA click did not navigate to a different job.
	// jsSuccess is checked at the top of the form loop, so terminal success pages are handled there.
	if err := verifyJobIdentity(page, expectedJobPath); err != nil {
		return err
	}

	// Diagnostic: log what form inputs exist in the modal at the start.
	const jsDiagInputs = `() => {
		function allInDOM(root, selector) {
			const r = [];
			try {
				r.push(...root.querySelectorAll(selector));
				for (const el of root.querySelectorAll('*')) {
					if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
					if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
						try {
							const d = el.contentDocument;
							if (d) r.push(...allInDOM(d, selector));
						} catch (e) {}
					}
				}
			} catch(e) {}
			return r;
		}
		const anchor =
			allInDOM(document, '[data-test-form-element]')[0] ||
			allInDOM(document, 'fieldset[data-test-form-builder-radio-button-form-component]')[0] ||
			allInDOM(document, '.jobs-easy-apply-content')[0] ||
			allInDOM(document, '.jobs-easy-apply-modal')[0];
		const form = anchor ? (anchor.closest('form') || anchor.parentElement) : null;
		const root = form || document.body;
		const inputs = [...root.querySelectorAll('input, select, textarea')];
		return JSON.stringify(inputs.map(el => ({
			tag: el.tagName, type: el.type || '', name: el.name.substring(0,40) || '', id: el.id.substring(0,40) || '',
			required: el.required, ariaRequired: el.getAttribute('aria-required'),
			value: el.value ? el.value.substring(0, 20) : '',
			files: el.files ? el.files.length : -1,
		})));
	}`
	if diagRes, err := page.Eval(jsDiagInputs); err == nil {
		b.log.Debug().Str("inputs", diagRes.Value.String()).Msg("easy apply: modal inputs at start")
	}

	// JS to click the correct Easy Apply action button.
	// LinkedIn's apply UI is identified by CTA labels (Next / Continue / Review /
	// Submit), not by fragile .jobs-easy-apply-* shell classes. We deep-walk
	// open shadow roots + same-origin iframes; the Go caller also re-runs this
	// inside each CDP frame when the top document cannot see the modal.
	const jsClickPrimary = `(expectedID, allowedKind) => {
        try {
            const u=new URL(window.top.location.href);
            const id=(u.pathname.match(/^\/jobs\/view\/([^/]+)/)||[])[1] || u.searchParams.get('currentJobId');
            if(id!==expectedID) return {ok:false,label:'job identity changed'};
        } catch(e) { return {ok:false,label:'job identity unreadable'}; }
		function allInDOM(root, sel) {
			const r = [];
			const walk = (node) => {
				if (!node) return;
				try {
					if (node.querySelectorAll) r.push(...node.querySelectorAll(sel));
				} catch (e) {}
				let kids = [];
				try { kids = node.querySelectorAll ? [...node.querySelectorAll('*')] : []; } catch (e) {}
				for (const el of kids) {
					if (el.shadowRoot) walk(el.shadowRoot);
					if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
						try {
							const d = el.contentDocument;
							if (d) walk(d);
						} catch (e) {}
					}
				}
			};
			walk(root);
			return r;
		}

		function isVisible(el) {
			try {
				const rect = el.getBoundingClientRect();
				if (rect.width < 2 && rect.height < 2) return false;
				const doc = el.ownerDocument || document;
				const view = doc.defaultView || window;
				let node = el;
				while (node && node !== doc.documentElement) {
					const s = view.getComputedStyle(node);
					if (s.display === 'none' || s.visibility === 'hidden') return false;
					const parent = node.parentElement;
					if (parent) { node = parent; continue; }
					const root = node.getRootNode && node.getRootNode();
					if (root && root.host) { node = root.host; continue; }
					break;
				}
				return true;
			} catch (e) { return false; }
		}

		function isEnabled(btn) {
			if (btn.disabled) return false;
			if (btn.getAttribute('aria-disabled') === 'true') return false;
			if (btn.classList && btn.classList.contains('artdeco-button--disabled')) return false;
			return true;
		}

		function btnLabel(btn) {
			return ((btn.getAttribute('aria-label') || '') + ' ' + (btn.innerText || btn.textContent || ''))
				.trim().replace(/\s+/g, ' ');
		}

		function isMessagingChrome(el) {
			if (!el) return false;
			try {
				if (el.closest && el.closest(
					'.msg-overlay-conversation-bubble, .msg-overlay-list-bubble, .msg-form, .msg-conversations-container, .msg-overlay, [data-test-messaging]'
				)) return true;
			} catch (e) {}
			const t = btnLabel(el).toLowerCase();
			return t.includes('conversation') || t.includes('messaging') ||
				t.includes('close your conversation') || t.includes('open your conversation') ||
				t.includes('new message');
		}

		function isExcludedRail(el) {
			try {
				return !!el.closest('aside, [class*="similar-job"], [class*="jobs-similar"], [class*="scaffold-layout__aside"], [class*="people-also-viewed"]');
			} catch (e) { return false; }
		}

		function actionKind(btn) {
			const t = btnLabel(btn).toLowerCase();
			if (btn.hasAttribute('data-live-test-easy-apply-submit-button') || btn.hasAttribute('data-easy-apply-submit-button')) return 'submit';
			if (btn.hasAttribute('data-live-test-easy-apply-review-button') || btn.hasAttribute('data-easy-apply-review-btn')) return 'review';
			if (btn.hasAttribute('data-live-test-easy-apply-next-button') || btn.hasAttribute('data-easy-apply-next-button')) return 'next';
			if (t.includes('submit application') || t === 'submit' || t.startsWith('submit ')) return 'submit';
			if (t.includes('review your application') || t === 'review' || t.startsWith('review ')) return 'review';
			if (t.includes('continue to next step') || t === 'continue' || t.startsWith('continue ')) return 'continue';
			if (t === 'next' || t.startsWith('next ') || t === 'done' || t.startsWith('done ')) return 'next';
			return '';
		}

		function trustClick(btn) {
			const label=btnLabel(btn);
            try { btn.scrollIntoView({block:'center'}); btn.focus(); btn.click(); return {ok:true,label}; } catch(e) { return {ok:false,label}; }
		}
		const priority = allowedKind ? [allowedKind] : [];
		const candidates = allInDOM(document, 'button, [role="button"], a[role="button"]')
			.filter(b => b.closest('[role="dialog"], .jobs-easy-apply-content, .jobs-easy-apply-modal, [data-test-easy-apply-modal]') && !isMessagingChrome(b) && !isExcludedRail(b) && actionKind(b));

		for (const kind of priority) {
			const btn = candidates.find(b => actionKind(b) === kind && isVisible(b) && isEnabled(b));
			if (btn) return trustClick(btn);
		}

		const primary = allInDOM(document, 'button.artdeco-button--primary, button[class*="primary"]')
			.find(b => {
				if (!b.closest('[role="dialog"], .jobs-easy-apply-content, .jobs-easy-apply-modal, [data-test-easy-apply-modal]') || !isVisible(b) || !isEnabled(b) || isMessagingChrome(b) || isExcludedRail(b)) return false;
				const t = btnLabel(b).toLowerCase();
				if (t.includes('dismiss') || t.includes('cancel') || t.includes('close') || t.includes('back') || t.includes('save')) return false;
				if (t.includes('easy apply') || t === 'apply' || t.startsWith('apply ')) return false;
				return actionKind(b) !== '';
			});
		if (primary && allowedKind && actionKind(primary)===allowedKind) return trustClick(primary);

		const disabledNext = candidates.find(b => actionKind(b) && isVisible(b) && !isEnabled(b));
		if (disabledNext) {
			return { ok: false, label: 'apply CTA disabled (validation): ' + btnLabel(disabledNext).slice(0, 60) };
		}

		const seen = candidates.slice(0, 12).map(b => btnLabel(b).slice(0, 40)).filter(Boolean);
		const iframeCount = allInDOM(document, 'iframe').length;
		const dialogCount = allInDOM(document, '[role="dialog"], .artdeco-modal').length;
		return {
			ok: false,
			label: 'no apply CTA (iframes=' + iframeCount + ', dialogs=' + dialogCount + ', seen=[' + seen.join(' | ') + '])'
		};
	}`

	// clickEasyApplyPrimary runs the CTA clicker on the top page, then inside each
	// CDP iframe frame. LinkedIn often mounts the apply UI in a frame whose
	// contentDocument is opaque to page JS but reachable via rod's Frame().
	clickEasyApplyPrimary := func(allowedKind string) (label string, ok bool, err error) {
		tryEval := func(p *rod.Page) (string, bool, error) {
			res, evalErr := p.Timeout(10*time.Second).Eval(jsClickPrimary, linkedInExpectedID(lazy.job.URL), allowedKind)
			if evalErr != nil {
				return "", false, evalErr
			}
			return res.Value.Get("label").String(), res.Value.Get("ok").Bool(), nil
		}

		label, ok, err = tryEval(page)
		if err != nil || ok {
			return label, ok, err
		}

		frames, frameErr := page.Elements("iframe, frame")
		if frameErr != nil {
			return label, false, nil
		}
		for _, fr := range frames {
			fp, ferr := fr.Frame()
			if ferr != nil || fp == nil {
				continue
			}
			flabel, fok, ferr := tryEval(fp)
			if ferr != nil {
				continue
			}
			if fok {
				return flabel, true, nil
			}
			if flabel != "" && (strings.Contains(flabel, "disabled") || strings.Contains(flabel, "seen=[")) {
				label = flabel
			}
		}
		return label, false, nil
	}

	// jsStepHash identifies the current Easy Apply step using visible question text
	// and the section heading inside the modal. Progress % alone is unreliable because
	// LinkedIn only updates it at major milestones, not on every individual step.
	const jsStepHash = `() => {
		function allInDOM(root, selector) {
			const r = [];
			try {
				r.push(...root.querySelectorAll(selector));
				for (const el of root.querySelectorAll('*')) {
					if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, selector));
					if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
						try {
							const d = el.contentDocument;
							if (d) r.push(...allInDOM(d, selector));
						} catch (e) {}
					}
				}
			} catch(e) {}
			return r;
		}
		function isVisible(el) {
			try {
				const rect = el.getBoundingClientRect();
				if (rect.width === 0 && rect.height === 0) return false;
				if (rect.right <= 0 || rect.left >= window.innerWidth) return false;
				if (rect.bottom <= 0 || rect.top >= window.innerHeight) return false;
				let n = el;
				while (n && n !== document.documentElement) {
					const s = window.getComputedStyle(n);
					if (s.display === 'none' || s.visibility === 'hidden') return false;
					n = n.parentElement;
				}
				return true;
			} catch(e) { return false; }
		}
		const prog = allInDOM(document, 'progress[aria-valuenow]')[0]
			|| document.querySelector('progress[aria-valuenow]');
		const pct = prog ? prog.getAttribute('aria-valuenow') : '';
		const texts = allInDOM(document,
			'[role="dialog"] h3, [role="dialog"] h4, .artdeco-modal h3, .artdeco-modal h4, ' +
			'.jobs-easy-apply-content h3, .jobs-easy-apply-modal h3, ' +
			'legend, legend span[aria-hidden="true"], ' +
			'[data-test-form-element] label:not(.visually-hidden), ' +
			'.artdeco-text-input--label, ' +
			'[role="group"] label, fieldset label:first-of-type'
		)
			.filter(isVisible)
			.map(e => e.textContent.trim().replace(/\s+/g, ' ').substring(0, 60))
			.filter(Boolean)
			.slice(0, 4)
			.join('|');
		return pct + '~' + texts;
	}`

	// Stuck detection: count consecutive button-click failures.
	okFailCount := 0
	// consecutiveUnfillable counts steps where fields were found but none could be filled.
	consecutiveUnfillable := 0
	// consecutiveBlind counts advancing steps (ok=true) where nothing was filled — safety net for unsupported field types.
	consecutiveBlind := 0
	// noAdvanceCount tracks when the step hash doesn't change after a click.
	noAdvanceCount := 0
	prevStepHash := ""

	if lazy != nil && lazy.reviewPrepareOnly {
		return b.scanApplyStepsForPrepare(ctx, page, lazy)
	}

	for i := 0; i < 40; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if b.cfg.BrowserMeter != nil {
			if !DestructiveActionAllowed(b.cfg.BrowserMeter) {
				return fmt.Errorf("easy apply: %w", b.cfg.BrowserMeter.Err())
			}
		}

		b.interactionPause()

		if submitClicked && linkedInSubmissionConfirmed(page, linkedInExpectedID(lazy.job.URL), lazy.job.Company) {
			b.log.Info().Msg("easy apply: success detected")
			return nil
		}
		if err := verifyJobIdentity(page, expectedJobPath); err != nil {
			return err
		}

		// Check whether the modal actually advanced since the previous click.
		if hashRes, err := page.Timeout(10 * time.Second).Eval(jsStepHash); err == nil {
			h := hashRes.Value.String()
			b.log.Debug().Msgf("easy apply: step %d hash=%q", i, h)
			// Only treat as "same step" when we got a meaningful hash.
			// "~" means jsStepHash found no labels, treat as unknown rather than stuck.
			if h != "" && h != "~" && h == prevStepHash {
				noAdvanceCount++
				b.log.Warn().Int("count", noAdvanceCount).Msg("easy apply: modal did not advance")
			} else {
				noAdvanceCount = 0
				prevStepHash = h
			}
		} else if isCDPFatal(err) {
			return fmt.Errorf("easy apply: browser closed or context canceled: %w", err)
		}

		// Per-step diagnostic: log what form inputs exist right now.
		if diagRes, err := page.Timeout(10 * time.Second).Eval(jsDiagInputs); err == nil {
			b.log.Debug().Str("inputs", diagRes.Value.String()).Msgf("easy apply: step %d inputs", i)
		} else if isCDPFatal(err) {
			return fmt.Errorf("easy apply: browser closed or context canceled: %w", err)
		}

		// Fill any unanswered fields on the current step before clicking the action button.
		filled, hasFields, fillErr := b.fillFormStep(ctx, page, lazy)
		if fillErr != nil {
			if isFormFillFatalLLM(fillErr) {
				b.abortOnLLMFailure(fillErr)
			}
			return fmt.Errorf("easy apply: form fill aborted: %w", fillErr)
		}
		if hasFields && !filled {
			consecutiveUnfillable++
			b.log.Warn().Int("count", consecutiveUnfillable).Msg("easy apply: fields present but none filled")
			// Bail out if we cannot fill anything for many consecutive steps, this
			// catches cases where the step hash is "~" (unreadable) and noAdvanceCount
			// never increments, e.g. a required file-upload step with no generated PDF.
			if consecutiveUnfillable >= 6 {
				saveApplyDebug(page, "unfillable")
				return fmt.Errorf("easy apply: stuck, %d consecutive unfillable steps", consecutiveUnfillable)
			}
		} else {
			consecutiveUnfillable = 0
		}
		if filled {
			b.shortPause() // let React settle after fills
		}

		// Always uncheck the "Follow company" checkbox on the review page before submitting.
		if _, err := page.Timeout(10 * time.Second).Eval(`() => {
			const cb = document.getElementById('follow-company-checkbox');
			if (cb && cb.checked) {
				cb.checked = false;
				cb.dispatchEvent(new Event('change', { bubbles: true }));
			}
		}`); isCDPFatal(err) {
			return fmt.Errorf("easy apply: browser closed or context canceled: %w", err)
		}

		// Final pre-click guard: reject if quota expired during form-fill phase.
		if b.cfg.BrowserMeter != nil {
			if !DestructiveActionAllowed(b.cfg.BrowserMeter) {
				return fmt.Errorf("easy apply: %w", b.cfg.BrowserMeter.Err())
			}
		}

		// Identity check immediately before advancing — fail-closed: any unverifiable identity blocks the click.
		// jsSuccess is checked at the top of this loop, so terminal success pages exit before reaching here.
		if err := verifyJobIdentity(page, expectedJobPath); err != nil {
			return err
		}

		_, kind, found, enabled, peekErr := b.peekEasyApplyPrimary(page)
		if peekErr != nil {
			return fmt.Errorf("read application action: %w", peekErr)
		}
		if kind == "submit" && found && enabled {
			if err := markAttemptSubmitDispatch(b.cfg.DB, lazy.attemptID, b.cfg.UserID); err != nil {
				return err
			}
			submitClicked = true
		}
		label, ok, err := clickEasyApplyPrimary(kind)
		if err != nil {
			if submitClicked {
				return err
			}
			if isCDPFatal(err) {
				return fmt.Errorf("easy apply: browser closed or context canceled: %w", err)
			}
			b.log.Warn().Err(err).Int("step", i).Msg("easy apply: eval error")
			continue
		}
		b.log.Info().Msgf("easy apply: step %d, btn=%q ok=%v filled=%v", i, label, ok, filled)
		if submitClicked {
			if !ok {
				return errLinkedInSubmissionUnknown
			}
			return waitLinkedInConfirmation(ctx, page, linkedInExpectedID(lazy.job.URL), lazy.job.Company)
		}

		// Bail when the modal is stuck: button click may succeed (ok=true) while
		// LinkedIn validation blocks advance (e.g. essay fields left empty). Do NOT
		// require !filled — false "filled" from mismatched selects used to loop forever.
		stuckThreshold := 4
		if ok && filled {
			stuckThreshold = 5
		} else if ok {
			stuckThreshold = 6
		}
		if noAdvanceCount >= stuckThreshold {
			if errs := pageValidationErrors(page); len(errs) > 0 {
				b.log.Warn().Strs("errors", errs).Msg("easy apply: validation errors on page, re-filling step")
				refilled, _, refillErr := b.fillFormStep(ctx, page, lazy)
				if refillErr != nil {
					if isFormFillFatalLLM(refillErr) {
						b.abortOnLLMFailure(refillErr)
					}
					return fmt.Errorf("easy apply: form fill aborted: %w", refillErr)
				}
				if refilled {
					noAdvanceCount = 0
					consecutiveUnfillable = 0
					continue
				}
			}
			saveApplyDebug(page, "modal_stuck")
			return fmt.Errorf("easy apply: stuck, modal did not advance after %d iterations (ok=%v, filled=%v) — likely unanswered/invalid fields", noAdvanceCount, ok, filled)
		}

		if !ok {
			saveApplyDebug(page, "no_primary_button")
			okFailCount++
			if okFailCount >= 5 {
				b.log.Error().Str("btn", label).Msg("easy apply: stuck, no clickable primary button")
				return fmt.Errorf("stuck on step %d: no clickable primary button (label=%q)", i, label)
			}
			time.Sleep(2 * time.Second)
			continue
		}
		okFailCount = 0
		consecutiveUnfillable = 0 // click succeeded → step advanced, reset counter
		if !filled {
			consecutiveBlind++
			if consecutiveBlind >= 8 {
				return fmt.Errorf("easy apply: form did not accept input for %d consecutive advancing steps — possible unsupported field type", consecutiveBlind)
			}
		} else {
			consecutiveBlind = 0
		}

	}

	saveApplyDebug(page, "modal_incomplete")
	return fmt.Errorf("could not complete easy apply modal")
}

// ── Approved queue ─────────────────────────────────────────────────────────

// platformApply dispatches to the correct apply implementation for the bot's platform.
func (b *Bot) platformApply(ctx context.Context, page *rod.Page, lazy *lazyDocGen) error {
	switch b.cfg.Platform {
	case domain.PlatformLinkedIn:
		return b.easyApply(ctx, page, lazy)
	case domain.PlatformSeek:
		return b.seekApply(ctx, page, lazy)
	default:
		return fmt.Errorf("no apply handler for platform %s", b.cfg.Platform)
	}
}

type approvedJob struct {
	JobID           string
	Platform        string
	Company         string
	Role            string
	Location        string
	Link            string
	ResumePath      string
	CoverLetterPath string
	NeedsEvaluation bool
}

func (b *Bot) processApprovedQueue(ctx context.Context, br *rod.Browser, remaining int) int {
	if b.cfg.DB == nil {
		return 0
	}
	rows, err := b.cfg.DB.QueryContext(ctx,
		`SELECT job_id,platform,company,role,location,link,COALESCE(resume_path,''),COALESCE(cover_letter_path,''),needs_evaluation
		 FROM jobs_approved_queue WHERE user_id = ? ORDER BY approved_at ASC`, b.cfg.UserID)
	if err != nil {
		b.log.Error().Err(err).Msg("approved queue: load")
		return 0
	}
	var jobs []approvedJob
	for rows.Next() {
		var j approvedJob
		if err := rows.Scan(&j.JobID, &j.Platform, &j.Company, &j.Role, &j.Location, &j.Link, &j.ResumePath, &j.CoverLetterPath, &j.NeedsEvaluation); err == nil {
			jobs = append(jobs, j)
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		b.log.Error().Err(err).Str("user_id", b.cfg.UserID).Msg("processApprovedQueue row iteration error")
		return 0
	}

	platform := string(b.cfg.Platform)
	applied := 0
	for _, j := range jobs {
		if b.stopped(ctx) || applied >= remaining {
			return applied
		}
		// Respect billing quota: stop if a mid-run bucket charge failed.
		if b.cfg.BrowserMeter != nil {
			if !DestructiveActionAllowed(b.cfg.BrowserMeter) {
				b.log.Warn().Err(b.cfg.BrowserMeter.Err()).Msg("approved queue: browser quota exceeded, stopping")
				return applied
			}
		}
		jobPlatform := j.Platform
		if jobPlatform == "" {
			jobPlatform = platform
		}
		if jobPlatform != platform {
			continue // wait for the matching platform bot run
		}
		if j.NeedsEvaluation {
			if b.processEvaluationRetry(ctx, br, j) {
				applied++
			}
			continue
		}
		b.log.Info().Str("job_id", j.JobID).Str("company", j.Company).Str("title", j.Role).Msg("approved queue: submitting")
		b.humanPause()

		ac := newAttemptContext(b.cfg.UserID, platform, j.JobID, j.Link, j.Role, j.Company, "automated_queue", b.cfg.AutomationRunID)
		beginAttempt(b.cfg.DB, ac)

		jobPage, err := br.Page(proto.TargetCreateTarget{URL: j.Link})
		if err != nil {
			b.log.Error().Str("error", sanitizeErr(err)).Str("url", redactURL(j.Link)).Str("attempt_id", ac.AttemptID).Str("job_id", j.JobID).Str("role", j.Role).Msg("approved queue: open job page")
			finishAttempt(b.cfg.DB, ac, classifyApplyOutcome(err), "open_page", "browser_error", err.Error())
			continue
		}
		queueDetails := scraper.JobDetails{Description: j.Role + " at " + j.Company}
		if jobPlatform != string(domain.PlatformLinkedIn) {
			queueDetails = b.fetchJob(ctx, linkedInJob{ID: j.JobID, URL: j.Link, Company: j.Company, Title: j.Role})
		}
		queueLazy := &lazyDocGen{
			b:       b,
			ctx:     ctx,
			job:     linkedInJob{ID: j.JobID, URL: j.Link, Company: j.Company, Title: j.Role},
			jobDesc: queueDetails.Description,
		}
		queueLazy.attemptID = ac.AttemptID
		if err := b.platformApply(ctx, jobPage, queueLazy); err != nil {
			_ = jobPage.Close()
			if errors.Is(err, errAlreadyApplied) {
				b.log.Info().Str("attempt_id", ac.AttemptID).Str("job_id", j.JobID).
					Str("company", j.Company).Str("role", j.Role).Msg("approved queue: already applied, skipping ✓")
				_, _ = appdb.ExecWithRetry(b.cfg.DB,
					`DELETE FROM jobs_approved_queue WHERE job_id = ? AND user_id = ?`, j.JobID, b.cfg.UserID)
				finishAttempt(b.cfg.DB, ac, OutcomeAlreadyApplied, "already_applied", "already_applied", "")
				applied++
				continue
			}
			b.log.Error().Err(err).Str("attempt_id", ac.AttemptID).Str("job_id", j.JobID).Str("role", j.Role).Msg("approved queue: apply failed")
			var score int
			_ = b.cfg.DB.QueryRow(
				`SELECT COALESCE(suitability_score,0) FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
				j.JobID, b.cfg.UserID,
			).Scan(&score)
			b.recordApplyFailure(j, score, err)
			_, _ = appdb.ExecWithRetry(b.cfg.DB,
				`DELETE FROM jobs_approved_queue WHERE job_id = ? AND user_id = ?`, j.JobID, b.cfg.UserID)
			outcome, stage, reasonCode := queueApplyOutcome(err)
			finishAttempt(b.cfg.DB, ac, outcome, stage, reasonCode, err.Error())
			continue
		}
		// Post-apply identity check for LinkedIn — fail-closed before recording as applied.
		if b.cfg.Platform == domain.PlatformLinkedIn {
			if err := verifyLinkedInCompletion(jobPage, j.Link, j.Company); err != nil {
				_ = jobPage.Close()
				b.log.Error().Err(err).Str("attempt_id", ac.AttemptID).Str("job_id", j.JobID).
					Str("company", j.Company).Str("role", j.Role).Msg("approved queue: post-apply identity check failed")
				_, _ = appdb.ExecWithRetry(b.cfg.DB, `DELETE FROM jobs_approved_queue WHERE job_id = ? AND user_id = ?`, j.JobID, b.cfg.UserID)
				b.recordApplyFailure(j, 0, fmt.Errorf("%w: %v", errLinkedInSubmissionUnknown, err))
				finishAttempt(b.cfg.DB, ac, OutcomeSubmissionUnknown, "submitted", "job_identity_drift", err.Error())
				continue
			}
		}
		_ = jobPage.Close()

		// Record as applied and remove from queue.
		// Fetch score from pending_review (may already be deleted, falls back to 0).
		var score int
		_ = b.cfg.DB.QueryRow(
			`SELECT COALESCE(suitability_score,0) FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
			j.JobID, b.cfg.UserID,
		).Scan(&score)
		if _, err := appdb.ExecWithRetry(b.cfg.DB,
			`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,suitability_score,applied_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			j.JobID, b.cfg.UserID, platform, j.Company, j.Role,
			j.Location, j.Link, j.ResumePath, j.CoverLetterPath, score,
			time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			b.log.Error().Err(err).Str("job_id", j.JobID).Str("company", j.Company).Str("role", j.Role).Msg("approved queue: failed to record applied job")
		}
		if _, err := appdb.ExecWithRetry(b.cfg.DB, `DELETE FROM jobs_approved_queue WHERE job_id = ? AND user_id = ?`, j.JobID, b.cfg.UserID); err != nil {
			b.log.Error().Err(err).Str("job_id", j.JobID).Msg("approved queue: failed to delete from queue")
		}
		b.log.Info().Str("attempt_id", ac.AttemptID).Str("job_id", j.JobID).Str("company", j.Company).Str("role", j.Role).Msg("approved queue: submitted ✓")
		finishAttempt(b.cfg.DB, ac, OutcomeSubmittedConfirmed, "submitted", "", "")
		applied++
	}
	return applied
}

func (b *Bot) recordApplyFailure(j approvedJob, score int, err error) {
	if err == nil {
		return
	}
	plat := j.Platform
	if plat == "" {
		plat = string(b.cfg.Platform)
	}
	label := "easy apply"
	if plat == string(domain.PlatformSeek) {
		label = "quick apply"
	}
	if plat == string(domain.PlatformLinkedIn) && linkedInNeedsReview(err) {
		_, _, reviewCode := linkedInReviewOutcome(err)
		b.log.Info().
			Str("event", "user_action_required").
			Str("schema_version", "1").
			Str("job_id", j.JobID).
			Str("platform", plat).
			Str("reason_code", reviewCode).
			Msg("user action required: LinkedIn review needed")
		b.preserveLinkedInReview(context.Background(), linkedInJob{ID: j.JobID, URL: j.Link, Title: j.Role, Company: j.Company, Location: j.Location}, score, "", err)
		return
	}
	reason := label + ": " + err.Error()
	if plat == string(domain.PlatformSeek) {
		b.recordSeekSkipped(seekJob{
			ID: j.JobID, Company: j.Company, Title: j.Role, Location: j.Location, URL: j.Link,
		}, reason, score, "", nil)
		return
	}
	b.recordSkipped(linkedInJob{
		ID: j.JobID, Company: j.Company, Title: j.Role, Location: j.Location, URL: j.Link,
	}, reason, score, "", nil)
}

// ── DB helpers ────────────────────────────────────────────────────────────

// alreadyAppliedReason returns a non-empty reason string if the job has
// already been seen, or "" if it is new.
func (b *Bot) alreadyAppliedReason(jobID string) string {
	if jobID == "" {
		return ""
	}
	if b.seenCache != nil {
		if r := b.seenCache.reason(jobID); r != "" {
			return r
		}
	}
	if b.cfg.DB == nil {
		return ""
	}
	var id string
	if b.cfg.DB.QueryRow("SELECT id FROM jobs_applied WHERE id = ? AND user_id = ?", jobID, b.cfg.UserID).Scan(&id) == nil {
		return "already applied"
	}
	if b.cfg.DB.QueryRow("SELECT id FROM jobs_skipped WHERE id = ? AND user_id = ?", jobID, b.cfg.UserID).Scan(&id) == nil {
		return "in skipped list"
	}
	if b.cfg.DB.QueryRow("SELECT job_id FROM jobs_pending_review WHERE job_id = ? AND user_id = ?", jobID, b.cfg.UserID).Scan(&id) == nil {
		return "in Top Matches"
	}
	if b.cfg.DB.QueryRow("SELECT job_id FROM jobs_approved_queue WHERE job_id = ? AND user_id = ?", jobID, b.cfg.UserID).Scan(&id) == nil {
		return "in approved queue"
	}
	return ""
}

// alreadyQueued returns true when a job with the same company+title is already in
// jobs_pending_review. LinkedIn occasionally shows the same position with different
// job IDs (sponsored vs organic), so ID-based dedup alone isn't enough.
func (b *Bot) alreadyQueued(ctx context.Context, company, title string) bool {
	if b.cfg.DB == nil {
		return false
	}
	var n int
	_ = b.cfg.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs_pending_review WHERE user_id = ? AND company = ? AND role = ?`,
		b.cfg.UserID, company, title,
	).Scan(&n)
	return n > 0
}

func (b *Bot) countAppliedToday() int {
	if b.cfg.DB == nil {
		return 0
	}
	var n int
	_ = b.cfg.DB.QueryRow("SELECT COUNT(*) FROM jobs_applied WHERE user_id = ? AND date(applied_at) = date('now')", b.cfg.UserID).Scan(&n)
	return n
}

func (b *Bot) recordApplied(job linkedInJob, resumePath, coverPath, resumeVer, coverVer, refsJSON string, score int, halalVerdict []byte) {
	if b.cfg.DB == nil {
		return
	}
	if _, err := appdb.ExecWithRetry(b.cfg.DB,
		`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,resume_content_version_id,cover_letter_content_version_id,document_refs_json,suitability_score,halal_verdict,applied_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, b.cfg.UserID, string(b.cfg.Platform), job.Company, job.Title,
		job.Location, job.URL, resumePath, coverPath, resumeVer, coverVer, refsJSON, score, halalVerdict,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		b.log.Error().Err(err).Str("job_id", job.ID).Msg("failed to record applied job")
	} else if b.cfg.OnApplied != nil {
		b.cfg.OnApplied()
	}
	if b.seenCache != nil {
		b.seenCache.mark(job.ID, seenApplied)
	}
}

func (b *Bot) recordSkipped(job linkedInJob, reason string, score int, reasoning string, halalVerdict []byte) {
	if b.cfg.DB == nil {
		return
	}
	if _, err := appdb.ExecWithRetry(b.cfg.DB,
		`INSERT OR IGNORE INTO jobs_skipped(id,user_id,platform,company,role,location,link,skip_reason,suitability_score,suitability_reasoning,halal_verdict,viewed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, b.cfg.UserID, string(b.cfg.Platform), job.Company, job.Title,
		job.Location, job.URL, reason, score, reasoning, halalVerdict,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		b.log.Error().Err(err).Str("job_id", job.ID).Msg("failed to record skipped job")
	}
	if b.seenCache != nil {
		b.seenCache.mark(job.ID, seenSkipped)
	}
}

func (b *Bot) savePendingReview(ctx context.Context, p *domain.PendingReview) {
	if b.cfg.DB == nil {
		return
	}
	halalJSON, _ := json.Marshal(p.HalalVerdict)
	if string(halalJSON) == "null" {
		halalJSON = nil
	}
	if _, err := appdb.ExecContextWithRetry(ctx, b.cfg.DB,
		`INSERT OR REPLACE INTO jobs_pending_review(job_id,user_id,company,role,location,platform,link,resume_path,cover_letter_path,resume_content_version_id,cover_letter_content_version_id,document_refs_json,suitability_score,suitability_reasoning,due_date,posted_date,easy_apply,halal_verdict,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.JobID, b.cfg.UserID, p.Company, p.Role, p.Location, string(p.Platform), p.Link,
		p.ResumePath, p.CoverLetterPath, p.ResumeContentVersionID, p.CoverLetterContentVersionID, p.DocumentRefsJSON,
		p.SuitabilityScore, p.SuitabilityReasoning, p.DueDate, p.PostedDate, p.EasyApply, halalJSON,
		p.CreatedAt.UTC().Format(time.RFC3339),
	); err != nil {
		b.log.Error().Err(err).Str("job_id", p.JobID).Msg("failed to save pending review")
	}
	if b.seenCache != nil {
		b.seenCache.mark(p.JobID, seenPendingReview)
	}
}

// unmarshalHalalVerdict decodes a JSON halal verdict from the bot's checkScore result.
// Returns nil if the input is empty or invalid.
func unmarshalHalalVerdict(data []byte) *domain.HalalVerdict {
	if len(data) == 0 {
		return nil
	}
	var v domain.HalalVerdict
	if err := json.Unmarshal(data, &v); err != nil {
		return nil
	}
	return &v
}

// ── Misc helpers ──────────────────────────────────────────────────────────

// humanPause sleeps a random duration between PauseBetweenJobsMin and Max.
// Used between whole jobs to mimic human browsing pace.
func (b *Bot) humanPause() {
	min := b.cfg.Settings.HumanBehavior.PauseBetweenJobsMin
	if min == 0 {
		min = 3
	}
	max := b.cfg.Settings.HumanBehavior.PauseBetweenJobsMax
	if max <= min {
		max = min + 5
	}
	d := time.Duration(min+rand.IntN(max-min+1)) * time.Second
	time.Sleep(d)
}

// interactionPause sleeps a short random duration between InteractionPauseMin and Max.
// Used between form-step interactions to mimic human reading/thinking time.
func (b *Bot) interactionPause() {
	minF := b.cfg.Settings.HumanBehavior.InteractionPauseMin
	maxF := b.cfg.Settings.HumanBehavior.InteractionPauseMax
	if minF <= 0 {
		minF = 0.8
	}
	if maxF <= minF {
		maxF = minF + 1.5
	}
	rangeMs := int64((maxF - minF) * 1000)
	jitter := rand.Int64N(rangeMs)
	d := time.Duration(int64(minF*1000)+jitter) * time.Millisecond
	time.Sleep(d)
}

// shortPause sleeps a brief random duration (half of interactionPause).
// Used after filling individual fields to let the UI react.
func (b *Bot) shortPause() {
	minF := b.cfg.Settings.HumanBehavior.InteractionPauseMin / 2
	maxF := b.cfg.Settings.HumanBehavior.InteractionPauseMax / 2
	if minF <= 0 {
		minF = 0.3
	}
	if maxF <= minF {
		maxF = minF + 0.5
	}
	rangeMs := int64((maxF - minF) * 1000)
	jitter := rand.Int64N(rangeMs)
	d := time.Duration(int64(minF*1000)+jitter) * time.Millisecond
	time.Sleep(d)
}

func (b *Bot) isBlacklisted(job linkedInJob) bool {
	if domain.LocationMatchesBlacklist(job.Location, b.cfg.Preferences.LocationBlacklist) {
		return true
	}
	for _, c := range b.cfg.Preferences.CompanyBlacklist {
		if strings.EqualFold(job.Company, c) || strings.Contains(strings.ToLower(job.Company), strings.ToLower(c)) {
			return true
		}
	}
	for _, t := range b.cfg.Preferences.TitleBlacklist {
		if strings.Contains(strings.ToLower(job.Title), strings.ToLower(t)) {
			return true
		}
	}
	return false
}

func (b *Bot) buildLinkedInSearchURL(keyword string, target domain.SearchTarget) string {
	params := url.Values{}
	params.Set("keywords", keyword)
	if loc := domain.FormatSearchLocation(target.Location, domain.PlatformLinkedIn); loc != "" {
		params.Set("location", loc)
	}

	var workTypes []string
	if target.Onsite {
		workTypes = append(workTypes, "1")
	}
	if target.Remote {
		workTypes = append(workTypes, "2")
	}
	if target.Hybrid {
		workTypes = append(workTypes, "3")
	}
	// Only apply when a subset is selected — omitting f_WT returns all work types.
	if len(workTypes) > 0 && len(workTypes) < 3 {
		params.Set("f_WT", strings.Join(workTypes, ","))
	}

	prefs := b.cfg.Preferences
	var jobTypes []string
	if prefs.JobTypes.FullTime {
		jobTypes = append(jobTypes, "F")
	}
	if prefs.JobTypes.PartTime {
		jobTypes = append(jobTypes, "P")
	}
	if prefs.JobTypes.Contract {
		jobTypes = append(jobTypes, "C")
	}
	if prefs.JobTypes.Temporary {
		jobTypes = append(jobTypes, "T")
	}
	if prefs.JobTypes.Internship {
		jobTypes = append(jobTypes, "I")
	}
	if prefs.JobTypes.Volunteer {
		jobTypes = append(jobTypes, "V")
	}
	if prefs.JobTypes.Other {
		jobTypes = append(jobTypes, "O")
	}
	if len(jobTypes) > 0 {
		params.Set("f_JT", strings.Join(jobTypes, ","))
	}

	if codes := domain.LinkedInExperienceCodes(prefs.ExperienceLevel); len(codes) > 0 && len(codes) < 6 {
		params.Set("f_E", strings.Join(codes, ","))
	}

	switch {
	case prefs.Date.Hours24:
		params.Set("f_TPR", "r86400")
	case prefs.Date.Week:
		params.Set("f_TPR", "r604800")
	case prefs.Date.Month:
		params.Set("f_TPR", "r2592000")
		// AllTime: omit f_TPR
	}

	return "https://www.linkedin.com/jobs/search/?" + params.Encode()
}

func (b *Bot) savePDF(data []byte, company, title, kind string) string {
	safe := func(s string) string {
		var sb strings.Builder
		for _, c := range s {
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
				sb.WriteRune(c)
			} else {
				sb.WriteByte('_')
			}
		}
		return sb.String()
	}
	dir := fmt.Sprintf("job_applications/%s_%s", safe(company), safe(title))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return ""
	}
	path := fmt.Sprintf("%s/%s.pdf", dir, kind)
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return ""
	}
	return path
}

// linkedinEnsureLoggedIn returns nil when LinkedIn looks authenticated. If not,
// and stored credentials exist, it runs linkedinAutoLogin once and re-checks.
func (b *Bot) linkedinEnsureLoggedIn(page *rod.Page) error {
	state, err := browser.PageLoginState(page, "linkedin")
	if err == nil && state == browser.LoginStateYes {
		return nil
	}
	if info, ierr := page.Info(); ierr == nil {
		u := strings.ToLower(info.URL)
		onLogin := strings.Contains(u, "/login") || strings.Contains(u, "/checkpoint") || strings.Contains(u, "/authwall")
		if !onLogin && state != browser.LoginStateNo {
			return nil
		}
	}

	if b.cfg.LinkedInEmail == "" || b.cfg.LinkedInPassword == "" {
		return fmt.Errorf("not logged in and no LinkedIn credentials saved")
	}

	if info, ierr := page.Info(); ierr == nil {
		u := strings.ToLower(info.URL)
		if !strings.Contains(u, "/login") && !strings.Contains(u, "linkedin.com/uas") {
			_ = page.Navigate("https://www.linkedin.com/login")
			_ = page.Timeout(30 * time.Second).WaitLoad()
			_ = page.Timeout(5 * time.Second).WaitStable(1 * time.Second)
		}
	}

	if autoErr := b.linkedinAutoLogin(page); autoErr != nil {
		return autoErr
	}

	_ = page.Navigate("https://www.linkedin.com/feed/")
	_ = page.Timeout(30 * time.Second).WaitLoad()
	_ = page.Timeout(8 * time.Second).WaitStable(2 * time.Second)

	state, err = browser.PageLoginState(page, "linkedin")
	if err == nil && state == browser.LoginStateYes {
		b.log.Info().Msg("linkedin: session recovered via stored credentials")
		return nil
	}
	if info, ierr := page.Info(); ierr == nil {
		u := strings.ToLower(info.URL)
		if strings.Contains(u, "/login") || strings.Contains(u, "/checkpoint") || strings.Contains(u, "/authwall") {
			return fmt.Errorf("auto-login completed but still on login/checkpoint page")
		}
	}
	if state == browser.LoginStateNo {
		return fmt.Errorf("auto-login completed but still logged out")
	}
	b.log.Info().Msg("linkedin: session recovered via stored credentials")
	return nil
}

// linkedinAutoLogin fills the LinkedIn login form using stored credentials.
func (b *Bot) linkedinAutoLogin(page *rod.Page) error {
	b.log.Info().Msg("linkedin: auto-login with stored credentials")

	emailInput, err := page.Timeout(10 * time.Second).Element(
		"input#username, input[name='session_key'], input[type='email']",
	)
	if err != nil {
		return fmt.Errorf("login form: email input not found: %w", err)
	}
	_ = emailInput.SelectAllText()
	if err := emailInput.Input(b.cfg.LinkedInEmail); err != nil {
		return fmt.Errorf("fill email: %w", err)
	}
	b.humanPause()

	pwdInput, err := page.Timeout(5 * time.Second).Element("input#password, input[name='session_password'], input[type='password']")
	if err != nil {
		return fmt.Errorf("login form: password input not found: %w", err)
	}
	_ = pwdInput.SelectAllText()
	if err := pwdInput.Input(b.cfg.LinkedInPassword); err != nil {
		return fmt.Errorf("fill password: %w", err)
	}
	b.humanPause()

	if btn, e := page.Element("button[type='submit'], button[data-litms-control-urn='login-submit']"); e == nil {
		_ = btn.Click(proto.InputMouseButtonLeft, 1)
	}

	_ = page.Timeout(30 * time.Second).WaitLoad()
	_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)
	return nil
}
