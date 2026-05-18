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
	SeekEmail     string            // stored credentials for auto-login on session expiry
	SeekPassword  string
	Sessions      *browser.SessionStore // if set, updated with fresh cookies after each auth
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
	return &Bot{cfg: cfg, state: domain.BotStateIdle, stopCh: make(chan struct{})}
}

func (b *Bot) State() domain.BotState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
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

	// Verify session is still valid after browser launch.
	if info, e := page.Info(); e == nil {
		u := info.URL
		if strings.Contains(u, "/login") || strings.Contains(u, "/checkpoint") || strings.Contains(u, "/authwall") {
			log.Error().Msg("linkedin: session expired, re-login via Settings → Secrets")
			b.mu.Lock()
			b.state = domain.BotStateError
			b.mu.Unlock()
			return
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
		n := b.processKeyword(ctx, br, page, keyword, limit-appliedToday)
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
			appliedToday += b.processKeyword(ctx, br, page, keyword, limit-appliedToday)
		}
	}
	b.SetKeyword("")
	log.Info().Int("applied_today", appliedToday).Msg("linkedin: stopped, all keywords processed, no more jobs found")
}

// isCDPDead returns true when the browser's CDP connection is no longer usable
// (e.g. after a VPN reset drops the underlying TCP connection).
// String match is intentional: rod does not expose a typed sentinel for CDP
// disconnection. Validated against go-rod v0.116+ — update this comment if the message changes.
func isCDPDead(page *rod.Page) bool {
	_, err := page.Eval(`() => true`)
	return err != nil && strings.Contains(err.Error(), "closed network connection")
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

	if b.cfg.Settings.Browser.UseChromeProfile && b.cfg.Settings.Browser.ChromeProfilePath != "" {
		l = l.UserDataDir(b.cfg.Settings.Browser.ChromeProfilePath)
	}

	u, err := l.Launch()
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

	// After auth0 completes silent re-auth, the browser's cookie jar contains
	// with valid tokens instead of the original (now-rotated/invalid) ones.
	if b.cfg.Sessions != nil && b.cfg.Platform == domain.PlatformSeek {
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
				log.Debug().Msg("browser: session cookies refreshed after auth0 re-auth")
			}
		}
	}

	return br, page, nil
}

func (b *Bot) processKeyword(ctx context.Context, br *rod.Browser, page *rod.Page, keyword string, remaining int) int {
	jobs, err := b.scrapeLinkedInJobs(ctx, page, keyword)
	if err != nil {
		log.Error().Err(err).Str("keyword", keyword).Msg("linkedin: scrape jobs failed")
		return 0
	}
	if len(jobs) == 0 {
		log.Info().Str("keyword", keyword).Msg("linkedin: no new jobs found for keyword")
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

func (b *Bot) scrapeLinkedInJobs(ctx context.Context, page *rod.Page, keyword string) ([]linkedInJob, error) {
	b.navigateLinkedInSearch(page, keyword)

	cap := b.cfg.Settings.MaxJobsPerKeyword
	if cap <= 0 {
		cap = 25
	}
	fallbackLocation := ""
	if len(b.cfg.Preferences.Locations) > 0 {
		fallbackLocation = b.cfg.Preferences.Locations[0]
	}
	jobs := b.collectLinkedInCards(page, cap, fallbackLocation)
	log.Info().Msgf("linkedin: search returned %d jobs for %q", len(jobs), keyword)
	return jobs, nil
}

func (b *Bot) navigateLinkedInSearch(page *rod.Page, keyword string) {
	searchURL := b.buildLinkedInSearchURL(keyword)
	log.Info().Msgf("linkedin: searching %q", keyword)
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
	// Detect Easy Apply badge on listing card.
	if _, err := el.Element("[aria-label*='Easy Apply'], .jobs-apply-button--top-card"); err == nil {
		job.EasyApply = true
	} else if txt, err := el.Text(); err == nil && strings.Contains(strings.ToLower(txt), "easy apply") {
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

	details := b.fetchJob(ctx, job)
	if details.PostedDate == "" {
		details.PostedDate = job.PostedDate
	}
	llmBefore := b.llmSnapshot()
	score, reasoning, halalVerdict, ok := b.checkScore(ctx, job, details.Description)
	if !ok {
		return false
	}

	// Docs are generated lazily at the file-upload step, only if the toggle is on
	// and a file field is actually encountered during form filling.
	lazy := &lazyDocGen{b: b, ctx: ctx, job: job, jobDesc: details.Description}

	// Detect Easy Apply on the job detail page (authoritative).
	easyApply := job.EasyApply // card-level fallback
	if jobPage, err := br.Page(proto.TargetCreateTarget{URL: job.URL}); err == nil {
		_ = jobPage.Timeout(30 * time.Second).WaitLoad()
		easyApply = detectLinkedInEasyApply(jobPage)
		_ = jobPage.Close()
	}

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
		// Not an Easy Apply job, queue for manual application via Top Matches.
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

// detectLinkedInEasyApply returns true when the job detail page has a clickable
// Easy Apply button. Read-only, does not click anything.
// Polls for up to 10s because LinkedIn's SPA renders the button after the load event.
func detectLinkedInEasyApply(page *rod.Page) bool {
	const jsDetect = `() => {
		const candidates = [
			...document.querySelectorAll('button'),
			...document.querySelectorAll('a'),
			...document.querySelectorAll('[role="button"]'),
		];
		return candidates.some(b => {
			const label = (b.getAttribute('aria-label') || '').toLowerCase();
			const text  = b.textContent.toLowerCase().trim();
			return label.includes('easy apply') || text === 'easy apply';
		});
	}`
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := page.Eval(jsDetect)
		if err == nil && res.Value.Bool() {
			return true
		}
		time.Sleep(1 * time.Second)
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

	// Poll for up to 30s for either an Easy Apply button OR an "already applied" state.
	// LinkedIn's SPA renders both dynamically after the initial load event.
	const jsPoll = `() => {
		// Already applied?
		const t = document.body.innerText.toLowerCase();
		if (t.includes('application submitted') || t.includes('applied  ') ||
		    document.querySelector('[class*="application-status"]') ||
		    document.querySelector('[data-test-job-save-button]') === null && t.includes('applied')) {
			const hasAppStatus = document.querySelector('[class*="application-status"], [class*="ApplicationStatus"]');
			if (hasAppStatus || t.includes('application submitted')) return 'already_applied';
		}
		// Easy Apply button present?
		const candidates = [
			...document.querySelectorAll('button'),
			...document.querySelectorAll('a'),
			...document.querySelectorAll('[role="button"]'),
		];
		const btn = candidates.find(b => {
			const label = (b.getAttribute('aria-label') || '').toLowerCase();
			const text  = b.textContent.toLowerCase().trim();
			return label.includes('easy apply') || text === 'easy apply';
		});
		if (btn) {
			btn.scrollIntoView({ block: 'center' });
			btn.click();
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
	default:
		// Neither found, save diagnostics.
		const jsDiag = `() => [
			...document.querySelectorAll('button'),
			...document.querySelectorAll('a[href]'),
			...document.querySelectorAll('[role="button"]'),
		].map(b => (b.getAttribute('aria-label') || b.textContent || '').trim().substring(0, 60))
		 .filter(t => t).slice(0, 30)`
		if diag, err := page.Eval(jsDiag); err == nil {
			log.Warn().Str("buttons", diag.Value.String()).Msg("easy apply: buttons found on page")
		}
		if os.Getenv("DEBUG_BOT") != "" {
			if shot, err := page.Screenshot(false, nil); err == nil {
				_ = os.WriteFile("debug_easy_apply.png", shot, 0o644)
				log.Warn().Msg("easy apply: screenshot saved to debug_easy_apply.png")
			}
		}
		return fmt.Errorf("easy apply button not found after 30s")
	}

	// Wait for the Easy Apply modal to appear before starting the form loop.
	// LinkedIn's SPA can take several seconds to render the dialog after the button click.
	const jsModalPresent = `() => !!document.querySelector(
		'[data-test-modal][role="dialog"], .artdeco-modal[role="dialog"], [data-test-easy-apply-modal]'
	)`
	modalDeadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(modalDeadline) {
		if res, err := page.Eval(jsModalPresent); err == nil && res.Value.Bool() {
			break
		}
		time.Sleep(500 * time.Millisecond)
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
				}
			} catch(e) {}
			return r;
		}
		const anchor =
			allInDOM(document, '[data-test-form-element]')[0] ||
			allInDOM(document, 'fieldset[data-test-form-builder-radio-button-form-component]')[0] ||
			allInDOM(document, '.jobs-easy-apply-content')[0];
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

	// JS to click the correct Easy Apply modal action button.
	// LinkedIn's Artdeco modal renders buttons inside shadow DOM, so we must
	// recursively walk all shadow roots to find them.
	const jsClickPrimary = `() => {
		function allInDOM(root, sel) {
			const r = [];
			try {
				r.push(...root.querySelectorAll(sel));
				for (const el of root.querySelectorAll('*'))
					if (el.shadowRoot) r.push(...allInDOM(el.shadowRoot, sel));
			} catch(e) {}
			return r;
		}

		// Visibility check scoped to the modal, avoids nav-bar false positives.
		// Does NOT enforce viewport bounds (footer buttons sit at the screen edge).
		function isVisibleInModal(el) {
			try {
				const rect = el.getBoundingClientRect();
				if (rect.width === 0 && rect.height === 0) return false;
				let node = el;
				while (node && node !== document.documentElement) {
					const s = window.getComputedStyle(node);
					if (s.display === 'none' || s.visibility === 'hidden') return false;
					node = node.parentElement;
				}
				return true;
			} catch(e) { return false; }
		}

		function clickBtn(btn) {
			const label = (btn.getAttribute('aria-label') || btn.textContent || '').trim();
			btn.scrollIntoView({block: 'nearest'});
			btn.click();
			return {ok: true, label: label};
		}

		// 1. LinkedIn data-attribute selectors, unique to apply action buttons,
		//    no visibility check needed (they could be at the very edge of the viewport).
		const dataSelectors = [
			'[data-live-test-easy-apply-submit-button]',
			'[data-easy-apply-submit-button]',
			'[data-live-test-easy-apply-review-button]',
			'[data-easy-apply-review-btn]',
			'[data-live-test-easy-apply-next-button]',
			'[data-easy-apply-next-button]',
		];
		for (const sel of dataSelectors) {
			const btn = allInDOM(document, sel)[0];
			if (btn) return clickBtn(btn);
		}

		// 2. Text / aria-label matching, restrict to inside the modal dialog
		//    so we never accidentally click nav-bar buttons.
		const modal = document.querySelector('[data-test-modal][role="dialog"], .artdeco-modal[role="dialog"]')
		              || document;
		const priority = [
			'submit application',
			'submit',
			'review your application',
			'continue to next step',
			'next',
		];
		const modalBtns = allInDOM(modal, 'button, [role="button"]').filter(isVisibleInModal);
		for (const lbl of priority) {
			const btn = modalBtns.find(b => {
				const t = (b.getAttribute('aria-label') || b.textContent || '').toLowerCase().trim();
				return t === lbl || t.startsWith(lbl);
			});
			if (btn) return clickBtn(btn);
		}

		// 3. Fallback: any primary button in the modal footer.
		const footerPrimary = allInDOM(modal,
			'footer button.artdeco-button--primary, [role="dialog"] button.artdeco-button--primary')
			.find(isVisibleInModal);
		if (footerPrimary) return clickBtn(footerPrimary);

		const labels = modalBtns
			.map(b => (b.getAttribute('aria-label') || b.textContent || '').trim().substring(0, 50))
			.filter(t => t).slice(0, 30);
		return {ok: false, label: labels.join(' | ')};
	}`

	const jsSuccess = `() => {
		const t = document.body.innerText.toLowerCase();
		return t.includes('application was sent') ||
		       t.includes('application submitted') ||
		       (t.includes('your application') && t.includes('sent'));
	}`

	// jsStepHash identifies the current Easy Apply step using visible question text
	// and the section heading inside the modal. Progress % alone is unreliable because
	// LinkedIn only updates it at major milestones, not on every individual step.
	const jsStepHash = `() => {
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
		const prog = document.querySelector('progress[aria-valuenow]');
		const pct = prog ? prog.getAttribute('aria-valuenow') : '';
		// Collect visible question labels and section headings, these change every step.
		const texts = [
			...document.querySelectorAll(
				'[role="dialog"] h3, [role="dialog"] h4, .artdeco-modal h3, .artdeco-modal h4, ' +
				'legend, legend span[aria-hidden="true"], ' +
				'[data-test-form-element] label:not(.visually-hidden), ' +
				'.artdeco-text-input--label, ' +
				'[role="group"] label, fieldset label:first-of-type'
			)
		]
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

		if res, err := page.Eval(jsSuccess); err == nil && res.Value.Bool() {
			log.Info().Msg("easy apply: success detected")
			return nil
		}

		// Check whether the modal actually advanced since the previous click.
		if hashRes, err := page.Eval(jsStepHash); err == nil {
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
		}

		// Per-step diagnostic: log what form inputs exist right now.
		if diagRes, err := page.Eval(jsDiagInputs); err == nil {
			log.Debug().Str("inputs", diagRes.Value.String()).Msgf("easy apply: step %d inputs", i)
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
		_, _ = page.Eval(`() => {
			const cb = document.getElementById('follow-company-checkbox');
			if (cb && cb.checked) {
				cb.checked = false;
				cb.dispatchEvent(new Event('change', { bubbles: true }));
			}
		}`)

		res, err := page.Eval(jsClickPrimary)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("easy apply: browser closed or context canceled: %w", err)
			}
			log.Warn().Err(err).Int("step", i).Msg("easy apply: eval error")
			continue
		}

		label := res.Value.Get("label").String()
		ok := res.Value.Get("ok").Bool()
		log.Info().Msgf("easy apply: step %d, btn=%q ok=%v filled=%v", i, label, ok, filled)

		// Bail when the modal is stuck: either the button click failed repeatedly,
		// or the button click succeeds (ok=true) but the step hash never changes and
		// nothing new is filled, e.g. a typeahead field that requires dropdown
		// selection but didn't get one, leaving the step frozen.
		stuckThreshold := 4
		if ok {
			stuckThreshold = 8 // be more lenient when clicks succeed
		}
		if noAdvanceCount >= stuckThreshold && !filled {
			if os.Getenv("DEBUG_BOT") != "" {
			if shot, err := page.Screenshot(false, nil); err == nil {
				_ = os.WriteFile("debug_modal_stuck.png", shot, 0o644)
			}
		}
			return fmt.Errorf("easy apply: stuck, modal did not advance after %d iterations (ok=%v, no fill)", noAdvanceCount, ok)
		}

		if !ok {
			if os.Getenv("DEBUG_BOT") != "" {
				if shot, err := page.Screenshot(false, nil); err == nil {
					_ = os.WriteFile("debug_modal.png", shot, 0o644)
				}
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
			b.interactionPause() // pause before checking submission success
			if res, err := page.Eval(jsSuccess); err == nil && res.Value.Bool() {
				log.Info().Msg("easy apply: submitted successfully")
				return nil
			}
			// If the page no longer has an Easy Apply modal, assume success.
			if res, err := page.Eval(`() => !document.querySelector('[class*="easy-apply"], [class*="artdeco-modal"]')`); err == nil && res.Value.Bool() {
				log.Info().Msg("easy apply: modal closed after submit, assuming success")
				return nil
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
		_, _ = b.cfg.DB.Exec(
			`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,suitability_score,applied_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			j.JobID, b.cfg.UserID, platform, j.Company, j.Role,
			j.Location, j.Link, j.ResumePath, j.CoverLetterPath, score,
			time.Now().UTC().Format(time.RFC3339),
		)
		_, _ = b.cfg.DB.Exec(`DELETE FROM jobs_approved_queue WHERE job_id = ? AND user_id = ?`, j.JobID, b.cfg.UserID)
		log.Info().Str("company", j.Company).Str("title", j.Role).Msg("approved queue: submitted ✓")
		applied++
	}
	return applied
}

// ── DB helpers ────────────────────────────────────────────────────────────

// alreadyAppliedReason returns a non-empty reason string if the job has
// already been seen, or "" if it is new.
func (b *Bot) alreadyAppliedReason(jobID string) string {
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
	if _, err := b.cfg.DB.Exec(
		`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,suitability_score,halal_verdict,applied_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, b.cfg.UserID, string(domain.PlatformLinkedIn), job.Company, job.Title,
		job.Location, job.URL, resumePath, coverPath, score, halalVerdict,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		log.Error().Err(err).Str("job_id", job.ID).Msg("failed to record applied job")
	}
}

func (b *Bot) recordSkipped(job linkedInJob, reason string, score int, reasoning string, halalVerdict []byte) {
	if b.cfg.DB == nil {
		return
	}
	if _, err := b.cfg.DB.Exec(
		`INSERT OR IGNORE INTO jobs_skipped(id,user_id,platform,company,role,location,link,skip_reason,suitability_score,suitability_reasoning,halal_verdict,viewed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, b.cfg.UserID, string(domain.PlatformLinkedIn), job.Company, job.Title,
		job.Location, job.URL, reason, score, reasoning, halalVerdict,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		log.Error().Err(err).Str("job_id", job.ID).Msg("failed to record skipped job")
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
	if _, err := b.cfg.DB.ExecContext(ctx,
		`INSERT OR REPLACE INTO jobs_pending_review(job_id,user_id,company,role,location,platform,link,resume_path,cover_letter_path,suitability_score,suitability_reasoning,due_date,posted_date,easy_apply,halal_verdict,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.JobID, b.cfg.UserID, p.Company, p.Role, p.Location, string(p.Platform), p.Link,
		p.ResumePath, p.CoverLetterPath, p.SuitabilityScore, p.SuitabilityReasoning, p.DueDate, p.PostedDate, p.EasyApply, halalJSON,
		p.CreatedAt.UTC().Format(time.RFC3339),
	); err != nil {
		log.Error().Err(err).Str("job_id", p.JobID).Msg("failed to save pending review")
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

func (b *Bot) buildLinkedInSearchURL(keyword string) string {
	params := url.Values{}
	params.Set("keywords", keyword)
	if len(b.cfg.Preferences.Locations) > 0 {
		params.Set("location", b.cfg.Preferences.Locations[0])
	}

	prefs := b.cfg.Preferences
	var workTypes []string
	if prefs.Onsite {
		workTypes = append(workTypes, "1")
	}
	if prefs.Remote {
		workTypes = append(workTypes, "2")
	}
	if prefs.Hybrid {
		workTypes = append(workTypes, "3")
	}
	// Only apply the filter when a subset is selected, omitting it returns all work types.
	if len(workTypes) > 0 && len(workTypes) < 3 {
		params.Set("f_WT", strings.Join(workTypes, ","))
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
