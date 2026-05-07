package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	seekBrowsers map[string]*rod.Browser // persistent Seek browser per user
	browserMu    sync.Mutex              // protects seekBrowsers
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
		bots:         make(map[string]*botEntry),
		seekBrowsers: make(map[string]*rod.Browser),
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

	cfg, err := m.buildConfig(ctx, userID, platform)
	if err != nil {
		return err
	}

	botCtx, cancel := context.WithCancel(m.ctx)
	e.cancel = cancel
	e.bot = New(*cfg)
	now := time.Now()
	location := ""
	if len(cfg.Preferences.Locations) > 0 {
		location = cfg.Preferences.Locations[0]
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
		_ = m.db.QueryRow(
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
	go func() {
		ctx := m.ctx
		var gs domain.GeneralSettings
		if err := m.cfgStore.Get(userID, "general_settings", &gs); err != nil {
			log.Warn().Err(err).Str("user_id", userID).Msg("SubmitNow: failed to load general settings")
		}

		var profile domain.ResumeProfile
		if err := m.cfgStore.Get(userID, "resume_profile", &profile); err != nil {
			log.Warn().Err(err).Str("user_id", userID).Msg("SubmitNow: failed to load resume profile")
		}

		cookies, err := m.sessions.Load(userID, req.Platform)
		if err != nil {
			if req.Platform != "seek" {
				// For non-Seek platforms a saved session is required.
				log.Error().Err(err).Str("job", req.Role).Msg("approve: no session for platform")
				return
			}
			// For Seek: getSeekBrowser handles session loading; proceed with nil cookies.
			cookies = nil
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
				Platform:     domain.Platform(req.Platform),
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

		if req.Platform == "seek" {
			if raw, err := m.secrets.Get(userID, "cred:seek"); err == nil {
				var cred struct {
					Email    string `json:"email"`
					Password string `json:"password"`
				}
				if json.Unmarshal([]byte(raw), &cred) == nil {
					b.cfg.SeekEmail = cred.Email
					b.cfg.SeekPassword = cred.Password
				}
			}
		}

		// Seek: reuse a persistent browser per user so auth0 session state (including
		// the post-PKCE cookies from each Quick Apply) is preserved naturally between
		// consecutive approvals — no token-rotation issues.
		// LinkedIn (and remote-debug mode): new browser per job, same as before.
		var br *rod.Browser
		var jobPage *rod.Page
		ownsBr := false
		if req.Platform == "seek" && gs.Browser.RemoteDebugPort == 0 {
			seekBr, brErr := m.getSeekBrowser(ctx, userID, gs)
			if brErr != nil {
				log.Error().Err(brErr).Str("job", req.Role).Msg("approve: seek browser")
				return
			}
			p, pageErr := seekBr.Page(proto.TargetCreateTarget{URL: req.Link})
			if pageErr != nil {
				m.InvalidateSeekBrowser(userID)
				log.Error().Err(pageErr).Str("job", req.Role).Msg("approve: open seek job tab")
				return
			}
			br, jobPage = seekBr, p
		} else {
			newBr, newPage, launchErr := b.launchBrowser(ctx)
			if launchErr != nil {
				log.Error().Err(launchErr).Str("job", req.Role).Msg("approve: launch browser")
				return
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
				return
			}
			br, jobPage = newBr, newPage
		}
		defer jobPage.Close()
		if ownsBr {
			defer br.Close()
		}

		var score int
		if err := m.db.QueryRow(
			`SELECT COALESCE(suitability_score,0) FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
			req.JobID, userID,
		).Scan(&score); err != nil {
			log.Warn().Err(err).Str("job_id", req.JobID).Msg("SubmitNow: failed to fetch score from pending review")
		}

		var managerLazy *lazyDocGen
		if req.Platform == "seek" {
			// Read description directly from the already-open jobPage tab — avoids
			// opening a second tab at the same URL just to extract text.
			_ = jobPage.WaitLoad()
			_ = jobPage.WaitStable(500 * time.Millisecond)
			jobDesc := req.Role + " at " + req.Company
			if rawHTML, err := jobPage.HTML(); err == nil {
				if d := scraper.ParseHTML(rawHTML); len(strings.TrimSpace(d.Description)) >= 100 {
					jobDesc = d.Description
				}
			}
			managerLazy = &lazyDocGen{b: b, ctx: ctx, job: linkedInJob{Company: req.Company, Title: req.Role}, jobDesc: jobDesc}
		} else {
			managerDetails := b.fetchJob(ctx, linkedInJob{URL: req.Link, Company: req.Company, Title: req.Role})
			managerLazy = &lazyDocGen{b: b, ctx: ctx, job: linkedInJob{Company: req.Company, Title: req.Role}, jobDesc: managerDetails.Description}
		}

		var applyErr error
		if req.Platform == "seek" {
			applyErr = b.seekApply(ctx, jobPage, managerLazy)
		} else {
			applyErr = b.easyApply(ctx, jobPage, managerLazy)
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
					req.JobID, userID, req.Platform, req.Company, req.Role, req.Location, req.Link, applyLabel+": "+applyErr.Error(), score, "",
				); err != nil {
					log.Error().Err(err).Str("job_id", req.JobID).Msg("SubmitNow: failed to record skipped job")
				}
				return
			}
		}

		if _, err := m.db.Exec(
			`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,applied_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?)`,
			req.JobID, userID, req.Platform, req.Company, req.Role, req.Location, req.Link, req.ResumePath, req.CoverPath,
			time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			log.Error().Err(err).Str("job_id", req.JobID).Msg("SubmitNow: failed to record applied job")
		}
		if _, err := m.db.Exec(`DELETE FROM jobs_approved_queue WHERE job_id = ? AND user_id = ?`, req.JobID, userID); err != nil {
			log.Error().Err(err).Str("job_id", req.JobID).Msg("SubmitNow: failed to delete from approved queue")
		}
		log.Info().Str("company", req.Company).Str("job", req.Role).Msg("approve: submitted ✓")
	}()
}

// getSeekBrowser returns the persistent Seek browser for userID, creating it if needed.
// The browser is kept alive across consecutive SubmitNow calls so auth0 session state
// (including post-PKCE cookies) is preserved naturally — no token rotation issues.
func (m *Manager) getSeekBrowser(ctx context.Context, userID string, gs domain.GeneralSettings) (*rod.Browser, error) {
	m.browserMu.Lock()
	defer m.browserMu.Unlock()

	if br, ok := m.seekBrowsers[userID]; ok {
		if _, err := br.Pages(); err == nil {
			return br, nil // still alive
		}
		delete(m.seekBrowsers, userID)
		log.Info().Str("user_id", userID).Msg("seek: persistent browser was dead, recreating")
	}

	cookies, err := m.sessions.Load(userID, "seek")
	if err != nil {
		return nil, errors.New("no saved Seek session, log in via Settings → Secrets first")
	}

	l := launcher.New().Headless(!gs.Browser.ShowBrowser)
	if gs.Browser.UseChromeProfile && gs.Browser.ChromeProfilePath != "" {
		l = l.UserDataDir(gs.Browser.ChromeProfilePath)
	}
	wsURL, launchErr := l.Launch()
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
	_ = warmPage.WaitLoad()
	_ = warmPage.WaitStable(2 * time.Second)

	// Persist warm-up rotated tokens so the next server restart loads a valid
	// (unconsumed) refresh token — getSeekBrowser rotates on navigate but unlike
	// launchBrowser it must save explicitly here.
	if m.sessions != nil {
		warmCookies, cookieErr := proto.NetworkGetAllCookies{}.Call(warmPage)
		if cookieErr == nil {
			var fresh []browser.Cookie
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
			if saveErr := m.sessions.Save(userID, "seek", "session", fresh); saveErr != nil {
				log.Warn().Err(saveErr).Msg("seek: warm-up cookie save failed")
			} else {
				log.Debug().Str("user_id", userID).Msg("seek: session cookies saved after warm-up")
			}
		}
	}

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

func (m *Manager) buildConfig(ctx context.Context, userID string, platform domain.Platform) (*Config, error) {
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

	var profile domain.ResumeProfile
	if err := m.cfgStore.Get(userID, "resume_profile", &profile); err != nil {
		return nil, errors.New("no resume profile saved, add one at /api/settings/resume first")
	}

	resolved := platform
	isSupported := false
	for _, p := range domain.SupportedPlatforms {
		if p == platform {
			isSupported = true
			break
		}
	}
	if !isSupported {
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
