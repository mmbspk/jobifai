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
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/browser"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/resume"
	"github.com/user/jobifai/internal/scraper"
)

// lazyDocGen generates resume + cover letter on first demand and caches the
// result. Generation only happens when the toggle is on and a file upload
// field is actually encountered during form filling.
type lazyDocGen struct {
	b              *Bot
	ctx            context.Context
	job            linkedInJob
	jobDesc        string
	once           sync.Once
	resume         string
	cover          string
	formOnce       sync.Once
	formJSON       []byte
	resumeOverride string // pre-generated resume path from a prior attempt; skips LLM if set
	coverOverride  string // pre-generated cover letter path from a prior attempt; skips LLM if set
}

// get returns the generated resume and cover letter paths, generating them on
// the first call. If override paths from a prior attempt are set, they are
// returned directly without calling the LLM again.
func (l *lazyDocGen) get() (resume, cover string) {
	if l.resumeOverride != "" || l.coverOverride != "" {
		return l.resumeOverride, l.coverOverride
	}
	if !l.b.cfg.Settings.GenerateNewResumeDocs {
		return "", ""
	}
	l.once.Do(func() {
		l.resume, l.cover = l.b.generateDocs(l.ctx, l.job, l.jobDesc)
	})
	return l.resume, l.cover
}

// peek returns already-generated paths without triggering new generation.
// Returns override paths if set, otherwise whatever get() has already cached.
// Returns empty strings if generation never ran.
func (l *lazyDocGen) peek() (resume, cover string) {
	if l.resumeOverride != "" || l.coverOverride != "" {
		return l.resumeOverride, l.coverOverride
	}
	return l.resume, l.cover
}

// preload kicks off doc generation in the background so the LLM calls run
// concurrently with form loading rather than sequentially after it.
func (l *lazyDocGen) preload() {
	go l.get()
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
	WriteCoverLetter(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (string, error)
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
	RenderResume(ctx context.Context, profile *domain.ResumeProfile, styleName, cssOverride string) ([]byte, error)
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
	Platform      domain.Platform
	Settings      domain.GeneralSettings
	Preferences   domain.WorkPreferences
	Profile       *domain.ResumeProfile
	ProfileLoader func() *domain.ResumeProfile // if set, called per-job to get the latest profile
	Cookies       []browser.Cookie
	Tailor        ResumeTailor    // nil = no LLM tailoring
	Scorer        JobScorer       // nil = let all jobs through
	HalalChecker  JobHalalChecker // nil = halal filter disabled
	Renderer      ResumeRenderer
	DB            *sql.DB
	UserID        string // owner of this bot session
	RequireReview bool
	MarketDir     string // path to resume_markets/ directory
	LLMTracker    *llm.UsageTracker // optional; tracks per-job token usage for success log
	SeekEmail        string // stored credentials for auto-login on session expiry
	SeekPassword     string
	LinkedInEmail    string
	LinkedInPassword string
	Sessions         *browser.SessionStore // if set, updated with fresh cookies after each auth
}

// SubmitRequest bundles the fields needed to submit a single approved job.
type SubmitRequest struct {
	JobID                string
	Company              string
	Role                 string
	Location             string
	Platform             string
	Link                 string
	ResumePath           string
	CoverPath            string
	SuitabilityReasoning string
}

// Bot runs the Easy Apply automation loop for a single platform session.
type Bot struct {
	cfg            Config
	mu             sync.Mutex
	state          domain.BotState
	currentKeyword string
	stopCh         chan struct{}
	pauseMu        sync.Mutex
	pauseCh        chan struct{} // non-nil and open when paused; closed on resume

	seekSessionExpired bool // set during runSeek when a job page reveals the Seek session is no longer valid

	seenCache *jobSeenCache
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
	return &Bot{cfg: cfg, state: domain.BotStateIdle, stopCh: make(chan struct{}), seenCache: newJobSeenCache()}
}

// warmSeenCache loads applied/skipped/top-matches/approved job ids into memory.
func (b *Bot) warmSeenCache() {
	if b.seenCache == nil {
		b.seenCache = newJobSeenCache()
	}
	n, err := b.seenCache.load(b.cfg.DB, b.cfg.UserID)
	if err != nil {
		log.Warn().Err(err).Msg("bot: job seen cache load failed, falling back to per-job DB lookups")
		return
	}
	log.Info().Int("known_jobs", n).Msg("bot: job seen cache loaded")
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
	log.Error().Msg("seek: stopping run — Seek session expired, re-add your Seek session in Settings → Secrets")
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
func (b *Bot) waitIfPaused(ctx context.Context) {
	b.pauseMu.Lock()
	ch := b.pauseCh
	b.pauseMu.Unlock()
	if ch == nil {
		return
	}
	log.Info().Msg("bot: paused — waiting for resume")
	select {
	case <-ch:
	case <-b.stopCh:
	case <-ctx.Done():
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
		b.state = domain.BotStateStopped
		b.mu.Unlock()
		log.Info().Str("platform", string(b.cfg.Platform)).Msg("bot stopped")
	}()

	log.Info().Str("platform", string(b.cfg.Platform)).Msg("bot starting")

	runner, ok := runnerRegistry[b.cfg.Platform]
	if !ok {
		log.Warn().Str("platform", string(b.cfg.Platform)).Msg("platform not yet supported")
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
		log.Error().Err(err).Msg("linkedin: launch browser failed")
		return
	}
	defer br.Close()

	b.warmSeenCache()

	// Verify session is still valid after browser launch; try stored credentials once.
	if info, e := page.Info(); e == nil {
		u := info.URL
		if strings.Contains(u, "/login") || strings.Contains(u, "/checkpoint") || strings.Contains(u, "/authwall") {
			if recoverErr := b.linkedinEnsureLoggedIn(page); recoverErr != nil {
				log.Error().Err(recoverErr).Msg("linkedin: session expired, re-login via Settings → Secrets")
				b.mu.Lock()
				b.state = domain.BotStateError
				b.mu.Unlock()
				return
			}
		}
	}

	limit := b.cfg.Settings.HumanBehavior.DailyApplicationLimit
	if limit == 0 {
		limit = 40
	}
	appliedToday := b.countAppliedToday()
	log.Info().Int("applied_today", appliedToday).Int("limit", limit).Msg("linkedin: starting, daily progress")

	// Process previously approved jobs first.
	if appliedToday < limit {
		appliedToday += b.processApprovedQueue(ctx, br, limit-appliedToday)
	}

	targets := b.cfg.Preferences.EffectiveSearchTargets()
	for _, target := range targets {
		arrangement := searchArrangementLabel(target)
		if arrangement != "" || strings.TrimSpace(target.Location) != "" {
			log.Info().Str("location", target.Location).Str("arrangement", arrangement).
				Msg("linkedin: searching location target")
		}

		for _, keyword := range b.cfg.Preferences.Positions {
			b.waitIfPaused(ctx)
			if reason := b.stopReason(ctx); reason != "" {
				log.Info().Str("keyword", keyword).Msgf("linkedin: stopped, %s", reason)
				return
			}
			if appliedToday >= limit {
				log.Info().Int("limit", limit).Msg("linkedin: stopped, daily application limit reached")
				return
			}
			b.SetKeyword(keyword)
			n := b.processKeyword(ctx, br, page, keyword, limit-appliedToday, target)
			appliedToday += n

			// If no jobs were found, check whether the browser connection was lost
			// (e.g. VPN reset) and attempt a reconnect before continuing.
			if n == 0 && isCDPDead(page) {
				log.Warn().Msg("linkedin: browser connection lost, attempting reconnect")
				br.Close()
				newBr, newPage, err := b.launchBrowser(ctx)
				if err != nil {
					log.Error().Err(err).Msg("linkedin: reconnect failed, stopping")
					return
				}
				br, page = newBr, newPage
				log.Info().Msg("linkedin: browser reconnected, retrying keyword")
				appliedToday += b.processKeyword(ctx, br, page, keyword, limit-appliedToday, target)
			}
		}
	}
	b.SetKeyword("")
	log.Info().Int("applied_today", appliedToday).Msg("linkedin: stopped, all location targets and keywords processed")
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
			log.Warn().Err(err).Int("port", port).Msg("remote debug: resolve URL failed, falling back to new browser")
		} else {
			br := rod.New().ControlURL(u)
			if err := br.Connect(); err != nil {
				log.Warn().Err(err).Int("port", port).Msg("remote debug: connect failed, falling back to new browser")
			} else {
				log.Info().Int("port", port).Msg("browser: connected to existing Chrome via remote debug")
				page, err := br.Page(proto.TargetCreateTarget{URL: "about:blank"})
				if err != nil {
					br.Close()
					return nil, nil, fmt.Errorf("open page: %w", err)
				}
				return br, page, nil
			}
		}
	}

	l := launcher.New().
		Headless(!b.cfg.Settings.Browser.ShowBrowser)

	// Always use a persistent profile for job platforms so Auth0/localStorage
	// from Connect browser is reused. Path can be overridden in General settings.
	dir := browser.ProfileDir(b.cfg.UserID, string(b.cfg.Platform), b.cfg.Settings.Browser.ChromeProfilePath)
	browser.PrepareChromeProfileDir(dir)
	l = l.UserDataDir(dir)

	u, err := l.Launch()
	if err != nil && strings.Contains(err.Error(), "SingletonLock") {
		log.Warn().Err(err).Str("dir", dir).Msg("browser: profile locked, force-unlocking and retrying")
		browser.ForceUnlockChromeProfile(dir)
		u, err = launcher.New().Headless(!b.cfg.Settings.Browser.ShowBrowser).UserDataDir(dir).Launch()
	}
	if err != nil {
		return nil, nil, fmt.Errorf("launch chrome: %w", err)
	}
	br := rod.New().ControlURL(u)
	if err := br.Connect(); err != nil {
		return nil, nil, fmt.Errorf("connect chrome: %w", err)
	}
	// Set cookies on a blank page BEFORE navigating to LinkedIn,
	// so the session cookies are present on the very first request.
	page, err := br.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		br.Close()
		return nil, nil, fmt.Errorf("open blank page: %w", err)
	}
	if len(b.cfg.Cookies) > 0 {
		if err := page.SetCookies(browser.ToCookieParams(b.cfg.Cookies)); err != nil {
			log.Warn().Err(err).Msg("browser: set cookies")
		}
	}
	homeURL := "https://www.linkedin.com"
	if b.cfg.Platform == domain.PlatformSeek {
		homeURL = "https://au.seek.com/jobs"
	}
	if err := page.Navigate(homeURL); err != nil {
		br.Close()
		return nil, nil, fmt.Errorf("navigate %s: %w", b.cfg.Platform, err)
	}
	// Wait for the page to fully load and for Auth0 silent re-auth to complete.
	// Seek uses Auth0 SPA which refreshes the access token via a hidden iframe on
	// first load — navigating away before this finishes leaves the session without
	// an access token and Quick Apply redirects to login.
	_ = page.Timeout(30 * time.Second).WaitLoad()
	_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)

	// Recover a dead session with stored credentials before giving up.
	if b.cfg.Platform == domain.PlatformSeek {
		if err := b.seekEnsureLoggedIn(page); err != nil {
			log.Warn().Err(err).Msg("browser: seek session not authenticated after warm-up")
		}
	} else if b.cfg.Platform == domain.PlatformLinkedIn {
		if err := b.linkedinEnsureLoggedIn(page); err != nil {
			log.Warn().Err(err).Msg("browser: linkedin session not authenticated after warm-up")
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
					log.Warn().Err(marshalErr).Msg("browser: failed to marshal refreshed session cookies")
				} else if err := b.cfg.Sessions.Save(b.cfg.UserID, string(b.cfg.Platform), "session", raw); err != nil {
					log.Warn().Err(err).Msg("browser: failed to refresh session cookies")
				} else {
					log.Debug().Msg("browser: session cookies refreshed after auth warm-up")
				}
			}
		} else {
			log.Warn().Str("state", string(state)).Msg("browser: skipping cookie refresh — not confirmed logged in")
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
			log.Info().Msgf("linkedin: skip (%s): %q @ %s", reason, job.Title, job.Company)
			continue
		}
		fresh = append(fresh, job)
	}
	if skipped > 0 {
		log.Info().Int("skipped_known", skipped).Int("to_process", len(fresh)).Msg("linkedin: filtered known jobs from search results")
	}
	return fresh
}

func (b *Bot) processKeyword(ctx context.Context, br *rod.Browser, page *rod.Page, keyword string, remaining int, target domain.SearchTarget) int {
	jobs, err := b.scrapeLinkedInJobs(ctx, page, keyword, target)
	if err != nil {
		log.Error().Err(err).Str("keyword", keyword).Msg("linkedin: scrape jobs failed")
		return 0
	}
	if len(jobs) == 0 {
		log.Info().Str("keyword", keyword).Msg("linkedin: no new jobs found for keyword")
		return 0
	}
	jobs = b.filterLinkedInJobs(jobs)
	if len(jobs) == 0 {
		log.Info().Str("keyword", keyword).Msg("linkedin: all search results already known, nothing to process")
		return 0
	}
	log.Info().Msgf("linkedin: found %d jobs for %q, processing", len(jobs), keyword)
	applied := 0
	for _, job := range jobs {
		b.waitIfPaused(ctx)
		if reason := b.stopReason(ctx); reason != "" {
			log.Info().Str("keyword", keyword).Msgf("linkedin: stopped mid-keyword, %s", reason)
			return applied
		}
		if applied >= remaining {
			log.Info().Str("keyword", keyword).Int("remaining", remaining).Msg("linkedin: stopped mid-keyword, daily limit reached")
			return applied
		}
		b.humanPause()
		if b.processJob(ctx, br, job) {
			applied++
		}
	}
	log.Info().Str("keyword", keyword).Int("applied", applied).Msg("linkedin: keyword done")
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
func (b *Bot) stopReason(ctx context.Context) string {
	select {
	case <-b.stopCh:
		return "manually stopped"
	case <-ctx.Done():
		return "context cancelled: " + ctx.Err().Error()
	default:
		return ""
	}
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
	log.Info().Msgf("linkedin: search returned %d jobs for %q", len(jobs), keyword)
	return jobs, nil
}

func (b *Bot) navigateLinkedInSearch(page *rod.Page, keyword string, target domain.SearchTarget) {
	searchURL := b.buildLinkedInSearchURL(keyword, target)
	arrangement := searchArrangementLabel(target)
	log.Info().
		Str("keyword", keyword).
		Str("location", target.Location).
		Str("arrangement", arrangement).
		Msg("linkedin: searching")
	log.Debug().Str("url", searchURL).Msg("linkedin: navigating to search URL")
	if err := page.Timeout(30 * time.Second).Navigate(searchURL); err != nil {
		log.Warn().Err(err).Msg("linkedin: navigate timed out, proceeding anyway")
	}
	log.Info().Msg("linkedin: navigate done, waiting for load")
	if err := page.Timeout(30 * time.Second).WaitLoad(); err != nil {
		log.Warn().Err(err).Msg("linkedin: WaitLoad timed out, proceeding anyway")
	}
	log.Info().Msg("linkedin: page ready, scraping cards")
}

func (b *Bot) collectLinkedInCards(page *rod.Page, cap int, fallbackLocation string) []linkedInJob {
	seen := map[string]bool{}
	var jobs []linkedInJob

	const maxPages = 10
	for pageNum := 0; len(jobs) < cap && pageNum < maxPages; pageNum++ {
		if pageNum > 0 {
			log.Info().Msgf("linkedin: navigating to page %d", pageNum+1)
			if !clickNextPage(page) {
				log.Info().Msg("linkedin: no next page, stopping")
				break
			}
			time.Sleep(3 * time.Second)
		}
		prev := len(jobs)
		jobs = scrollPageCards(page, seen, jobs, cap, fallbackLocation)
		log.Info().Msgf("linkedin: page %d yielded %d new jobs", pageNum+1, len(jobs)-prev)
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
	log.Info().Msgf("linkedin: processing, %q @ %s", job.Title, job.Company)

	if reason := b.alreadyAppliedReason(job.ID); reason != "" {
		log.Info().Msgf("linkedin: skip (%s): %q @ %s", reason, job.Title, job.Company)
		return false
	}
	if b.alreadyQueued(ctx, job.Company, job.Title) {
		log.Info().Msgf("linkedin: skip, duplicate listing already queued: %q @ %s", job.Title, job.Company)
		return false
	}
	// Card-level applied indicator (LinkedIn shows "Applied" badge on already-applied cards).
	// Use recordSkipped rather than recordApplied: the bot did not submit an application here.
	// This also avoids false positives from browser crashes leaving phantom "Applied" badges.
	if job.AlreadyApplied {
		b.recordSkipped(job, "already_applied_indicator", 0, "", nil)
		log.Info().Str("company", job.Company).Str("title", job.Title).Msg("linkedin: card shows applied badge, recording to skipped")
		return false
	}
	if b.isBlacklisted(job) {
		b.recordSkipped(job, "blacklisted", 0, "", nil)
		log.Info().Msgf("linkedin: skip, blacklisted: %q @ %s", job.Title, job.Company)
		return false
	}
	if !b.cfg.Preferences.ExperienceLevel.MatchesExperienceTitle(job.Title) {
		b.recordSkipped(job, "experience_level", 0, "", nil)
		log.Info().Msgf("linkedin: skip, experience level: %q @ %s", job.Title, job.Company)
		return false
	}

	// Open the job page once — authoritative Easy Apply detection before scoring/apply.
	easyApply := job.EasyApply
	details := b.fetchJob(ctx, job)
	if jobPage, err := br.Page(proto.TargetCreateTarget{URL: job.URL}); err == nil {
		_ = jobPage.Timeout(30 * time.Second).WaitLoad()
		easyApply = detectLinkedInEasyApply(jobPage)
		if pageDetails, err := jobPage.HTML(); err == nil {
			if parsed := scraper.ParseHTML(pageDetails); len(strings.TrimSpace(parsed.Description)) >= 100 {
				details = parsed
			}
		}
		_ = jobPage.Close()
	}
	if details.PostedDate == "" {
		details.PostedDate = job.PostedDate
	}
	if easyApply {
		log.Info().Msgf("linkedin: Easy Apply detected: %q @ %s", job.Title, job.Company)
	} else {
		log.Info().Msgf("linkedin: not Easy Apply, will route to Top Matches: %q @ %s", job.Title, job.Company)
	}

	llmBefore := b.llmSnapshot()
	score, reasoning, halalVerdict, ok := b.checkScore(ctx, job, details.Description)
	if !ok {
		return false
	}

	// Docs are generated lazily at the file-upload step, only if the toggle is on
	// and a file field is actually encountered during form filling.
	lazy := &lazyDocGen{b: b, ctx: ctx, job: job, jobDesc: details.Description}

	if b.cfg.RequireReview {
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
		log.Info().Msgf("linkedin: routed to Top Matches (not Easy Apply): %q @ %s", job.Title, job.Company)
		return false
	}

	return b.submitEasyApply(ctx, br, job, lazy, score, reasoning, halalVerdict, llmBefore)
}

// linkedInPageApplied returns true when the open job detail page shows an "Applied" indicator,
// meaning the user has already applied to this job manually or in a prior bot run.
// Uses a single JS eval round-trip instead of per-selector Element() calls (which each block
// for 3 s on a miss) — eliminates an 18-second delay on fresh unapplied jobs in the new UI.
func (b *Bot) linkedInPageApplied(page *rod.Page) bool {
	res, err := page.Eval(`() => {
		const t = document.body.innerText.toLowerCase();
		// Text signals visible after application submission
		if (t.includes('application submitted')) return true;
		if (t.includes('application was sent'))  return true;
		// aria-label="Applied" (exact) or "Applied <space>..." on the apply button
		if (document.querySelector('[aria-label="Applied"], button[aria-label*="Applied "]')) return true;
		// Stable data attribute set by LinkedIn when the apply button is in applied state
		if (document.querySelector('[data-test-job-apply-button-applied]')) return true;
		// "Application status" heading replaces the Easy Apply button after applying
		for (const h of document.querySelectorAll('h1,h2,h3')) {
			if (h.textContent && h.textContent.includes('Application status')) return true;
		}
		return false;
	}`)
	return err == nil && res.Value.Bool()
}

// detectLinkedInEasyApply returns true when the job details pane shows LinkedIn's
// Easy Apply CTA. This follows LinkedIn's UI contract — not job-specific heuristics:
//
//	Easy Apply  → accessible name contains "Easy Apply" (or "LinkedIn Apply …")
//	External    → primary CTA is plain "Apply" / "Apply on company website"
//
// Only the main job pane is considered (similar-jobs / aside rails are ignored).
func detectLinkedInEasyApply(page *rod.Page) bool {
	const jsDetect = `() => {
		function deepAll(root, selector) {
			const out = [];
			const walk = (node) => {
				if (!node || !node.querySelectorAll) return;
				try { out.push(...node.querySelectorAll(selector)); } catch (e) {}
				try {
					for (const el of node.querySelectorAll('*')) {
						if (el.shadowRoot) walk(el.shadowRoot);
					}
				} catch (e) {}
			};
			walk(root);
			return out;
		}
		function labelOf(el) {
			return ((el.getAttribute('aria-label') || '') + ' ' + (el.innerText || el.textContent || ''))
				.toLowerCase().replace(/\s+/g, ' ').trim();
		}
		function isExcluded(el) {
			return !!el.closest([
				'aside',
				'[class*="similar-job"]',
				'[class*="jobs-similar"]',
				'[class*="people-also-viewed"]',
				'[class*="scaffold-layout__aside"]',
				'[data-test-similar-jobs]',
				'.jobs-discovery',
			].join(','));
		}
		function isVisible(el) {
			const r = el.getBoundingClientRect();
			return r.width > 0 && r.height > 0;
		}
		function isEasyApplyLabel(t) {
			return t.includes('easy apply') || t.includes('linkedin apply');
		}

		// 1) Visible Easy Apply control in the main job pane (LinkedIn's standard CTA).
		const controls = [
			...deepAll(document, 'button'),
			...deepAll(document, 'a'),
			...deepAll(document, '[role="button"]'),
		];
		for (const el of controls) {
			if (isExcluded(el)) continue;
			if (!isVisible(el)) continue;
			if (isEasyApplyLabel(labelOf(el))) return true;
			if (el.getAttribute('data-live-test-easy-apply') != null) return true;
		}

		// 2) LinkedIn SDUI Easy Apply flag on an in-pane control/link.
		for (const el of controls) {
			if (isExcluded(el)) continue;
			const href = (el.getAttribute('href') || '').toLowerCase();
			if (href.includes('opensduiapplyflow=true') || href.includes('opensduiapplyflow')) return true;
		}

		return false;
	}`
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		res, err := page.Eval(jsDetect)
		if err == nil && res.Value.Bool() {
			return true
		}
		time.Sleep(400 * time.Millisecond)
	}

	// Diagnostics: what apply CTAs did we actually see?
	const jsDiag = `() => {
		function labelOf(el) {
			return ((el.getAttribute('aria-label') || '') + ' ' + (el.innerText || el.textContent || ''))
				.toLowerCase().replace(/\s+/g, ' ').trim().slice(0, 80);
		}
		return [...document.querySelectorAll('button, a, [role="button"]')]
			.map(el => labelOf(el))
			.filter(t => t.includes('apply') || t.includes('save'))
			.slice(0, 20);
	}`
	if diag, err := page.Eval(jsDiag); err == nil {
		log.Warn().Str("apply_ctas", diag.Value.String()).Msg("linkedin: Easy Apply CTA not found on job page")
	}
	return false
}

func (b *Bot) fetchJob(ctx context.Context, job linkedInJob) scraper.JobDetails {
	details, err := scraper.FetchJob(ctx, job.URL)
	if err != nil {
		log.Warn().Err(err).Msg("linkedin: fetch job desc")
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
		return minScore, "", nil, true
	}
	result, err := b.cfg.Scorer.EvaluateJob(ctx, b.currentProfile(), jobDesc)
	if err != nil {
		b.abortOnLLMFailure(err)
		return 0, "", nil, false
	}
	if result.Score < minScore {
		b.recordSkipped(job, fmt.Sprintf("score %d < %d", result.Score, minScore), result.Score, result.Reasoning, nil)
		log.Info().Msgf("linkedin: skip, score %d < %d for %q @ %s", result.Score, minScore, job.Title, job.Company)
		return result.Score, result.Reasoning, nil, false
	}
	log.Info().Msgf("linkedin: score %d/%d, %q @ %s", result.Score, 10, job.Title, job.Company)

	// Halal check, only runs after score passes to avoid wasted LLM calls.
	// HARAM → skip; DOUBTFUL → let through but carry verdict for storage.
	if b.cfg.HalalChecker != nil {
		verdict, err := b.cfg.HalalChecker.CheckHalal(ctx, job.Title, job.Company, jobDesc)
		if err != nil {
			log.Warn().Err(err).Msg("halal check failed, letting job through")
		} else if verdict.Verdict == "HARAM" {
			verdictJSON, _ := json.Marshal(verdict)
			b.recordSkipped(job, "halal filter", result.Score, result.Reasoning, verdictJSON)
			log.Info().Msgf("linkedin: halal skip (HARAM) %q @ %s", job.Title, job.Company)
			return result.Score, result.Reasoning, nil, false
		} else if verdict.Verdict == "DOUBTFUL" {
			halalVerdict, _ = json.Marshal(verdict)
			log.Info().Msgf("linkedin: halal DOUBTFUL, letting through %q @ %s", job.Title, job.Company)
		}
	}

	return result.Score, result.Reasoning, halalVerdict, true
}

func (b *Bot) abortOnLLMFailure(err error) {
	msg := err.Error()
	var reason string
	switch {
	case strings.Contains(msg, "401") || strings.Contains(msg, "Jwt is expired") || strings.Contains(msg, "LOGIN_FAILED"):
		reason = "LLM authentication failed — the proxy JWT has expired or the API key is invalid. Restart the LLM proxy to refresh credentials."
	case strings.Contains(msg, "connection refused"):
		reason = "LLM proxy is not running — connection refused. Start the proxy at the configured address."
	case strings.Contains(msg, "502") || strings.Contains(msg, "503"):
		reason = "LLM service is unavailable (502/503) — a network issue persisted after 3 retry attempts."
	default:
		reason = fmt.Sprintf("LLM call failed after 3 attempts: %v", err)
	}
	log.Error().Msgf("bot: aborting — %s", reason)
	b.Stop()
}

func (b *Bot) llmSnapshot() llm.UsageSnapshot {
	if b.cfg.LLMTracker == nil {
		return llm.UsageSnapshot{}
	}
	return b.cfg.LLMTracker.Snapshot()
}

func (b *Bot) logApplied(title, company string, platform domain.Platform, before llm.UsageSnapshot) {
	ev := log.Info().
		Str("event", "applied").
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
	profile := b.tailoredProfile(ctx, jobDesc, market)
	if b.cfg.Renderer == nil || profile == nil {
		return
	}
	cssOverride := ""
	if market != nil && market.CSSFile != "" {
		cssOverride = market.CSSFile
	}
	if pdf, err := b.cfg.Renderer.RenderResume(ctx, profile, "", cssOverride); err == nil {
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

func (b *Bot) tailoredProfile(ctx context.Context, jobDesc string, market *resume.MarketPrompts) *domain.ResumeProfile {
	profile := b.currentProfile()
	if b.cfg.Tailor == nil || profile == nil {
		return profile
	}
	promptCtx := jobDesc
	if market != nil && market.TailoredPrompt != "" {
		promptCtx = market.TailoredPrompt + "\n" + jobDesc
	}
	tailored, err := b.cfg.Tailor.TailorProfile(ctx, profile, promptCtx)
	if err != nil {
		log.Warn().Err(err).Msg("linkedin: tailoring failed, using base profile")
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
		promptCtx = market.CoverLetterPrompt + "\n" + jobDesc
	}
	body, err := b.cfg.Tailor.WriteCoverLetter(ctx, profile, promptCtx)
	if err != nil {
		return ""
	}
	pdf, err := b.cfg.Renderer.RenderCoverLetter(ctx, body, "", cssOverride)
	if err != nil {
		return ""
	}
	return b.savePDF(pdf, job.Company, job.Title, "cover_letter")
}

func (b *Bot) queueForReview(ctx context.Context, p *domain.PendingReview) {
	b.savePendingReview(ctx, p)
	if p.EasyApply {
		log.Info().Msgf("linkedin: queued for review, %q @ %s", p.Role, p.Company)
	} else {
		log.Info().Msgf("linkedin: added to Top Matches (manual apply), %q @ %s", p.Role, p.Company)
	}
}

func (b *Bot) submitEasyApply(ctx context.Context, br *rod.Browser, job linkedInJob, lazy *lazyDocGen, score int, reasoning string, halalVerdict []byte, llmBefore llm.UsageSnapshot) bool {
	jobPage, err := br.Page(proto.TargetCreateTarget{URL: job.URL})
	if err != nil {
		log.Error().Err(err).Msg("linkedin: open job page")
		return false
	}
	defer jobPage.Close()

	if err := b.easyApply(ctx, jobPage, lazy); err != nil {
		if errors.Is(err, errAlreadyApplied) {
			resume, cover := lazy.get()
			b.recordApplied(job, resume, cover, score, halalVerdict)
			log.Info().Str("company", job.Company).Str("title", job.Title).Msg("linkedin: already applied, recorded ✓")
			return true
		}
		log.Error().Err(err).Str("job", job.Title).Msg("linkedin: easy apply failed")
		b.recordSkipped(job, "easy apply: "+err.Error(), score, reasoning, nil)
		return false
	}
	resume, cover := lazy.get()
	b.recordApplied(job, resume, cover, score, halalVerdict)
	b.logApplied(job.Title, job.Company, domain.PlatformLinkedIn, llmBefore)
	return true
}

// ── Easy Apply modal navigation ────────────────────────────────────────────

func (b *Bot) easyApply(ctx context.Context, page *rod.Page, lazy *lazyDocGen) error {
	// Only wait for load if the page hasn't already fired its load event.
	// Calling WaitLoad() on an already-loaded page blocks forever.
	if ready, err := page.Eval(`() => document.readyState`); err != nil || ready.Value.String() != "complete" {
		if err := page.Timeout(30 * time.Second).WaitLoad(); err != nil {
			return fmt.Errorf("wait load: %w", err)
		}
	}

	if info, err := page.Eval(`() => window.location.href`); err == nil {
		log.Info().Str("url", info.Value.String()).Msg("easy apply: page URL after load")
	}

	// Kick off resume/cover generation in parallel with the form so uploads are ready
	// before the review/submit step (otherwise LLM finishes after "submitted").
	if lazy != nil {
		lazy.preload()
	}

	// Poll for up to 30s for either an Easy Apply button OR an "already applied" state.
	// LinkedIn's SPA renders both dynamically after the initial load event.
	// SDUI apply buttons ignore plain element.click() — use a full pointer lifecycle.
	const jsPoll = `() => {
		function trustClick(btn) {
			try { btn.scrollIntoView({ block: 'center' }); } catch(e) {}
			try { btn.focus(); } catch(e) {}
			const view = (btn.ownerDocument && btn.ownerDocument.defaultView) || window;
			const opts = { bubbles: true, cancelable: true, view: view };
			for (const type of ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click']) {
				try {
					if (type.startsWith('pointer')) {
						btn.dispatchEvent(new PointerEvent(type, Object.assign({pointerId: 1, pointerType: 'mouse'}, opts)));
					} else {
						btn.dispatchEvent(new MouseEvent(type, opts));
					}
				} catch(e) {
					try { btn.dispatchEvent(new MouseEvent(type.replace('pointer', 'mouse'), opts)); } catch(e2) {}
				}
			}
			try { btn.click(); } catch(e) {}
		}
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
		const t = document.body.innerText.toLowerCase();
		if (t.includes('application submitted') || t.includes('application was sent')) {
			const hasAppStatus = document.querySelector('[class*="application-status"], [class*="ApplicationStatus"]');
			if (hasAppStatus || t.includes('application submitted') || t.includes('application was sent')) return 'already_applied';
		}
		const candidates = (() => {
			return allInDOM(document, 'button, a, [role="button"]').filter(b => {
				return !b.closest('aside, [class*="similar-job"], [class*="jobs-similar"], [class*="scaffold-layout__aside"], [class*="people-also-viewed"]');
			});
		})();
		const btn = candidates.find(b => {
			const label = ((b.getAttribute('aria-label') || '') + ' ' + (b.innerText || b.textContent || ''))
				.toLowerCase().replace(/\s+/g, ' ').trim();
			if (label.includes('easy apply') || label.includes('linkedin apply')) return true;
			if (b.getAttribute('data-live-test-easy-apply') != null) return true;
			const href = (b.getAttribute('href') || '').toLowerCase();
			if (href.includes('opensduiapplyflow')) return true;
			return false;
		});
		if (btn) {
			trustClick(btn);
			return 'clicked';
		}
		return '';
	}`

	deadline := time.Now().Add(30 * time.Second)
	pollResult := ""
	for pollResult == "" && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if res, err := page.Eval(jsPoll); err == nil {
			pollResult = res.Value.String()
		}
		if pollResult == "" {
			time.Sleep(1 * time.Second)
		}
	}

	switch pollResult {
	case "already_applied":
		log.Info().Msg("easy apply: already applied")
		return errAlreadyApplied
	case "clicked":
		log.Info().Msg("easy apply: button clicked")
		// Collapse LinkedIn messaging overlays — they are also role="dialog".
		_, _ = page.Eval(`() => {
			const closeBtns = [...document.querySelectorAll(
				'.msg-overlay-bubble-header__control, .msg-overlay-conversation-bubble__control, button[aria-label*="Close your conversation"], button[aria-label*="Minimize your conversation"]'
			)];
			for (const b of closeBtns) {
				try { b.click(); } catch(e) {}
			}
		}`)
	default:
		const jsDiag = `() => [
			...document.querySelectorAll('button'),
			...document.querySelectorAll('a[href]'),
			...document.querySelectorAll('[role="button"]'),
		].map(b => (b.getAttribute('aria-label') || b.textContent || '').trim().substring(0, 60))
		 .filter(t => t).slice(0, 30)`
		if diag, err := page.Eval(jsDiag); err == nil {
			log.Warn().Str("buttons", diag.Value.String()).Msg("easy apply: buttons found on page")
		}
		if shot, err := page.Screenshot(false, nil); err == nil {
			_ = os.WriteFile("debug_easy_apply.png", shot, 0o644)
			log.Warn().Msg("easy apply: screenshot saved to debug_easy_apply.png")
		}
		return fmt.Errorf("easy apply button not found after 30s")
	}

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

	clickEasyApplyAgain := func() {
		_, _ = page.Eval(jsPoll)
	}

	modalDeadline := time.Now().Add(25 * time.Second)
	modalFound := false
	retriedClick := false
	for time.Now().Before(modalDeadline) {
		if res, err := page.Eval(jsModalPresent); err == nil && res.Value.Bool() {
			modalFound = true
			break
		}
		// Mid-wait: if nothing appeared after ~8s, click Easy Apply again once.
		if !retriedClick && time.Until(modalDeadline) < 17*time.Second {
			log.Warn().Msg("easy apply: modal not visible yet, re-clicking Easy Apply")
			clickEasyApplyAgain()
			retriedClick = true
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !modalFound {
		// Fallback: LinkedIn SDUI sometimes ignores synthetic clicks on the apply
		// TriggerButton. Opening the job URL with openSDUIApplyFlow=true forces the flow.
		curURL := ""
		if info, err := page.Info(); err == nil && info != nil {
			curURL = info.URL
		}
		if curURL != "" && !strings.Contains(curURL, "openSDUIApplyFlow=true") {
			sep := "?"
			if strings.Contains(curURL, "?") {
				sep = "&"
			}
			forceURL := curURL + sep + "openSDUIApplyFlow=true"
			log.Warn().Str("url", forceURL).Msg("easy apply: trying openSDUIApplyFlow URL fallback")
			_ = page.Navigate(forceURL)
			_ = page.Timeout(30 * time.Second).WaitLoad()
			_ = page.Timeout(5 * time.Second).WaitStable(2 * time.Second)
			time.Sleep(2 * time.Second)
			// Also click Easy Apply once more if the forced URL alone is not enough.
			_, _ = page.Eval(jsPoll)
			forceDeadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(forceDeadline) {
				if res, err := page.Eval(jsModalPresent); err == nil && res.Value.Bool() {
					modalFound = true
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
		}
	}
	if !modalFound {
		if diag, err := page.Eval(jsModalDiag); err == nil {
			log.Warn().Str("diag", diag.Value.String()).Msg("easy apply: modal did not appear")
		}
		if shot, err := page.Screenshot(false, nil); err == nil {
			_ = os.WriteFile("debug_easy_apply_no_modal.png", shot, 0o644)
			log.Warn().Msg("easy apply: screenshot saved to debug_easy_apply_no_modal.png")
		}
		return fmt.Errorf("easy apply modal did not appear after click (LinkedIn may have changed the apply UI or blocked the session)")
	}

	// Extra settle time after modal appears.
	time.Sleep(1 * time.Second)

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
		log.Debug().Str("inputs", diagRes.Value.String()).Msg("easy apply: modal inputs at start")
	}

	// JS to click the correct Easy Apply action button.
	// LinkedIn's apply UI is identified by CTA labels (Next / Continue / Review /
	// Submit), not by fragile .jobs-easy-apply-* shell classes. We deep-walk
	// open shadow roots + same-origin iframes; the Go caller also re-runs this
	// inside each CDP frame when the top document cannot see the modal.
	const jsClickPrimary = `() => {
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
			const label = btnLabel(btn);
			try { btn.scrollIntoView({ block: 'nearest', inline: 'nearest' }); } catch (e) {}
			try { btn.focus(); } catch (e) {}
			const view = (btn.ownerDocument && btn.ownerDocument.defaultView) || window;
			const opts = { bubbles: true, cancelable: true, view: view };
			for (const type of ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click']) {
				try {
					if (type.startsWith('pointer')) {
						btn.dispatchEvent(new PointerEvent(type, Object.assign({ pointerId: 1, pointerType: 'mouse' }, opts)));
					} else {
						btn.dispatchEvent(new MouseEvent(type, opts));
					}
				} catch (e) {
					try { btn.dispatchEvent(new MouseEvent(type.replace('pointer', 'mouse'), opts)); } catch (e2) {}
				}
			}
			try { btn.click(); } catch (e) {}
			return { ok: true, label: label };
		}

		const priority = ['submit', 'review', 'continue', 'next'];
		const candidates = allInDOM(document, 'button, [role="button"], a[role="button"]')
			.filter(b => !isMessagingChrome(b) && !isExcludedRail(b) && actionKind(b));

		for (const kind of priority) {
			const btn = candidates.find(b => actionKind(b) === kind && isVisible(b) && isEnabled(b));
			if (btn) return trustClick(btn);
		}

		const primary = allInDOM(document, 'button.artdeco-button--primary, button[class*="primary"]')
			.find(b => {
				if (!isVisible(b) || !isEnabled(b) || isMessagingChrome(b) || isExcludedRail(b)) return false;
				const t = btnLabel(b).toLowerCase();
				if (t.includes('dismiss') || t.includes('cancel') || t.includes('close') || t.includes('back') || t.includes('save')) return false;
				if (t.includes('easy apply') || t === 'apply' || t.startsWith('apply ')) return false;
				return actionKind(b) !== '';
			});
		if (primary) return trustClick(primary);

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
	clickEasyApplyPrimary := func() (label string, ok bool, err error) {
		tryEval := func(p *rod.Page) (string, bool, error) {
			res, evalErr := p.Timeout(10 * time.Second).Eval(jsClickPrimary)
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

	const jsSuccess = `() => {
		const t = (document.body && document.body.innerText || '').toLowerCase();
		// Must be a post-submit confirmation — never match "Review your application".
		if (t.includes('review your application')) return false;
		if (t.includes('application was sent')) return true;
		if (t.includes('your application was sent')) return true;
		if (t.includes('application submitted')) return true;
		if (t.includes('applied successfully')) return true;
		return false;
	}`

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

	for i := 0; i < 40; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		b.interactionPause()

		if res, err := page.Timeout(10 * time.Second).Eval(jsSuccess); err == nil && res.Value.Bool() {
			log.Info().Msg("easy apply: success detected")
			return nil
		} else if isCDPFatal(err) {
			return fmt.Errorf("easy apply: browser closed or context canceled: %w", err)
		}

		// Check whether the modal actually advanced since the previous click.
		if hashRes, err := page.Timeout(10 * time.Second).Eval(jsStepHash); err == nil {
			h := hashRes.Value.String()
			log.Debug().Msgf("easy apply: step %d hash=%q", i, h)
			// Only treat as "same step" when we got a meaningful hash.
			// "~" means jsStepHash found no labels, treat as unknown rather than stuck.
			if h != "" && h != "~" && h == prevStepHash {
				noAdvanceCount++
				log.Warn().Int("count", noAdvanceCount).Msg("easy apply: modal did not advance")
			} else {
				noAdvanceCount = 0
				prevStepHash = h
			}
		} else if isCDPFatal(err) {
			return fmt.Errorf("easy apply: browser closed or context canceled: %w", err)
		}

		// Per-step diagnostic: log what form inputs exist right now.
		if diagRes, err := page.Timeout(10 * time.Second).Eval(jsDiagInputs); err == nil {
			log.Debug().Str("inputs", diagRes.Value.String()).Msgf("easy apply: step %d inputs", i)
		} else if isCDPFatal(err) {
			return fmt.Errorf("easy apply: browser closed or context canceled: %w", err)
		}

		// Fill any unanswered fields on the current step before clicking the action button.
		filled, hasFields := b.fillFormStep(ctx, page, lazy)
		if hasFields && !filled {
			consecutiveUnfillable++
			log.Warn().Int("count", consecutiveUnfillable).Msg("easy apply: fields present but none filled")
			// Bail out if we cannot fill anything for many consecutive steps, this
			// catches cases where the step hash is "~" (unreadable) and noAdvanceCount
			// never increments, e.g. a required file-upload step with no generated PDF.
			if consecutiveUnfillable >= 6 {
				if os.Getenv("DEBUG_BOT") != "" {
					if shot, err := page.Screenshot(false, nil); err == nil {
						_ = os.WriteFile("debug_unfillable.png", shot, 0o644)
					}
				}
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

		label, ok, err := clickEasyApplyPrimary()
		if err != nil {
			if isCDPFatal(err) {
				return fmt.Errorf("easy apply: browser closed or context canceled: %w", err)
			}
			log.Warn().Err(err).Int("step", i).Msg("easy apply: eval error")
			continue
		}
		log.Info().Msgf("easy apply: step %d, btn=%q ok=%v filled=%v", i, label, ok, filled)

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
			if shot, err := page.Screenshot(false, nil); err == nil {
				_ = os.WriteFile("debug_modal_stuck.png", shot, 0o644)
			}
			return fmt.Errorf("easy apply: stuck, modal did not advance after %d iterations (ok=%v, filled=%v) — likely unanswered/invalid fields", noAdvanceCount, ok, filled)
		}

		if !ok {
			if shot, err := page.Screenshot(false, nil); err == nil {
				_ = os.WriteFile("debug_modal.png", shot, 0o644)
			}
			okFailCount++
			if okFailCount >= 5 {
				log.Error().Str("btn", label).Msg("easy apply: stuck, no clickable primary button")
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

		lowerLabel := strings.ToLower(label)
		if strings.Contains(lowerLabel, "submit") {
			b.interactionPause()
			_ = page.Timeout(10 * time.Second).WaitStable(500 * time.Millisecond)
			if res, err := page.Eval(jsSuccess); err == nil && res.Value.Bool() {
				log.Info().Msg("easy apply: submitted successfully")
				return nil
			}
			// Modal gone and not on review → success. Still on review → keep looping.
			stillReview := false
			if res, err := page.Eval(`() => (document.body.innerText || '').toLowerCase().includes('review your application')`); err == nil {
				stillReview = res.Value.Bool()
			}
			if !stillReview {
				if res, err := page.Eval(`() => {
					function allInDOM(root, sel) {
						const r = [];
						try {
							r.push(...root.querySelectorAll(sel));
							for (const el of root.querySelectorAll('*')) {
								if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, sel));
								if (el.tagName === 'IFRAME' || el.tagName === 'FRAME') {
									try { const d = el.contentDocument; if (d) r.push(...allInDOM(d, sel)); } catch(e) {}
								}
							}
						} catch(e) {}
						return r;
					}
					const easy = allInDOM(document, '.jobs-easy-apply-modal, .jobs-easy-apply-content, [data-test-easy-apply-modal]');
					return easy.length === 0;
				}`); err == nil && res.Value.Bool() {
					log.Info().Msg("easy apply: apply modal closed after submit, assuming success")
					return nil
				}
			} else {
				log.Warn().Msg("easy apply: still on review page after Submit click — will retry")
				// Scroll modal footer into view so Submit is clickable next iteration.
				_, _ = page.Eval(`() => {
					const dialog = document.querySelector('[role="dialog"], .artdeco-modal, .jobs-easy-apply-content');
					if (dialog) dialog.scrollTop = dialog.scrollHeight;
					const btn = [...document.querySelectorAll('button')].find(b =>
						/submit application/i.test(b.getAttribute('aria-label') || b.textContent || ''));
					if (btn) btn.scrollIntoView({block: 'center'});
				}`)
			}
		}
	}

	if os.Getenv("DEBUG_BOT") != "" {
		if shot, err := page.Screenshot(false, nil); err == nil {
			_ = os.WriteFile("debug_modal.png", shot, 0o644)
		}
	}
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
	Company         string
	Role            string
	Location        string
	Link            string
	ResumePath      string
	CoverLetterPath string
}

func (b *Bot) processApprovedQueue(ctx context.Context, br *rod.Browser, remaining int) int {
	if b.cfg.DB == nil {
		return 0
	}
	rows, err := b.cfg.DB.QueryContext(ctx,
		`SELECT job_id,company,role,location,link,COALESCE(resume_path,''),COALESCE(cover_letter_path,'')
		 FROM jobs_approved_queue WHERE user_id = ? ORDER BY approved_at ASC`, b.cfg.UserID)
	if err != nil {
		log.Error().Err(err).Msg("approved queue: load")
		return 0
	}
	var jobs []approvedJob
	for rows.Next() {
		var j approvedJob
		if err := rows.Scan(&j.JobID, &j.Company, &j.Role, &j.Location, &j.Link, &j.ResumePath, &j.CoverLetterPath); err == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Error().Err(err).Str("user_id", b.cfg.UserID).Msg("processApprovedQueue row iteration error")
		return 0
	}

	platform := string(b.cfg.Platform)
	applied := 0
	for _, j := range jobs {
		if b.stopped(ctx) || applied >= remaining {
			return applied
		}
		log.Info().Str("company", j.Company).Str("title", j.Role).Msg("approved queue: submitting")
		b.humanPause()

		jobPage, err := br.Page(proto.TargetCreateTarget{URL: j.Link})
		if err != nil {
			log.Error().Err(err).Str("job", j.Role).Msg("approved queue: open job page")
			continue
		}
		queueDetails := b.fetchJob(ctx, linkedInJob{URL: j.Link, Company: j.Company, Title: j.Role})
		queueLazy := &lazyDocGen{b: b, ctx: ctx, job: linkedInJob{Company: j.Company, Title: j.Role}, jobDesc: queueDetails.Description}
		if err := b.platformApply(ctx, jobPage, queueLazy); err != nil {
			jobPage.Close()
			log.Error().Err(err).Str("job", j.Role).Msg("approved queue: apply failed")
			continue
		}
		jobPage.Close()

		// Record as applied and remove from queue.
		// Fetch score from pending_review (may already be deleted, falls back to 0).
		var score int
		_ = b.cfg.DB.QueryRow(
			`SELECT COALESCE(suitability_score,0) FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
			j.JobID, b.cfg.UserID,
		).Scan(&score)
		_, _ = appdb.ExecWithRetry(b.cfg.DB,
			`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,suitability_score,applied_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			j.JobID, b.cfg.UserID, platform, j.Company, j.Role,
			j.Location, j.Link, j.ResumePath, j.CoverLetterPath, score,
			time.Now().UTC().Format(time.RFC3339),
		)
		_, _ = appdb.ExecWithRetry(b.cfg.DB, `DELETE FROM jobs_approved_queue WHERE job_id = ? AND user_id = ?`, j.JobID, b.cfg.UserID)
		log.Info().Str("company", j.Company).Str("title", j.Role).Msg("approved queue: submitted ✓")
		applied++
	}
	return applied
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

func (b *Bot) alreadyApplied(jobID string) bool {
	return b.alreadyAppliedReason(jobID) != ""
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

func (b *Bot) recordApplied(job linkedInJob, resumePath, coverPath string, score int, halalVerdict []byte) {
	if b.cfg.DB == nil {
		return
	}
	if _, err := appdb.ExecWithRetry(b.cfg.DB,
		`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,suitability_score,halal_verdict,applied_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, b.cfg.UserID, string(domain.PlatformLinkedIn), job.Company, job.Title,
		job.Location, job.URL, resumePath, coverPath, score, halalVerdict,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		log.Error().Err(err).Str("job_id", job.ID).Msg("failed to record applied job")
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
		job.ID, b.cfg.UserID, string(domain.PlatformLinkedIn), job.Company, job.Title,
		job.Location, job.URL, reason, score, reasoning, halalVerdict,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		log.Error().Err(err).Str("job_id", job.ID).Msg("failed to record skipped job")
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
		`INSERT OR REPLACE INTO jobs_pending_review(job_id,user_id,company,role,location,platform,link,resume_path,cover_letter_path,suitability_score,suitability_reasoning,due_date,posted_date,easy_apply,halal_verdict,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.JobID, b.cfg.UserID, p.Company, p.Role, p.Location, string(p.Platform), p.Link,
		p.ResumePath, p.CoverLetterPath, p.SuitabilityScore, p.SuitabilityReasoning, p.DueDate, p.PostedDate, p.EasyApply, halalJSON,
		p.CreatedAt.UTC().Format(time.RFC3339),
	); err != nil {
		log.Error().Err(err).Str("job_id", p.JobID).Msg("failed to save pending review")
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
		log.Info().Msg("linkedin: session recovered via stored credentials")
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
	log.Info().Msg("linkedin: session recovered via stored credentials")
	return nil
}

// linkedinAutoLogin fills the LinkedIn login form using stored credentials.
func (b *Bot) linkedinAutoLogin(page *rod.Page) error {
	log.Info().Msg("linkedin: auto-login with stored credentials")

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
