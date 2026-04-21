package bot

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

type botEntry struct {
	bot    *Bot
	cancel context.CancelFunc
	status domain.BotStatus
}

// Manager is the long-lived bot controller exposed to HTTP handlers.
// It supports concurrent bots, one per user, and satisfies handler.BotController.
type Manager struct {
	mu        sync.Mutex
	bots      map[string]*botEntry // key = userID
	db        *sql.DB
	cfgStore  *config.Store
	secrets   *config.SecretsStore
	sessions  *browser.SessionStore
	tailor    ResumeTailor
	scorer    JobScorer
	halal     JobHalalChecker
	renderer  ResumeRenderer
	marketDir string
}

func NewManager(
	db *sql.DB,
	cfgStore *config.Store,
	secrets *config.SecretsStore,
	sessions *browser.SessionStore,
	tailor ResumeTailor,
	scorer JobScorer,
	halal JobHalalChecker,
	renderer ResumeRenderer,
	marketDir string,
) *Manager {
	return &Manager{
		db:        db,
		cfgStore:  cfgStore,
		secrets:   secrets,
		sessions:  sessions,
		tailor:    tailor,
		scorer:    scorer,
		halal:     halal,
		renderer:  renderer,
		marketDir: marketDir,
		bots:      make(map[string]*botEntry),
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

	botCtx, cancel := context.WithCancel(context.Background())
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
	if e.bot != nil {
		e.status.Keyword = e.bot.Keyword()
	}
	return e.status
}

// SubmitNow immediately submits a job from the approved queue for userID.
func (m *Manager) SubmitNow(userID string, req SubmitRequest) {
	go func() {
		ctx := context.Background()
		var gs domain.GeneralSettings
		_ = m.cfgStore.Get(userID, "general_settings", &gs)

		var profile domain.ResumeProfile
		_ = m.cfgStore.Get(userID, "resume_profile", &profile)

		cookies, err := m.sessions.Load(userID, req.Platform)
		if err != nil {
			log.Error().Err(err).Str("job", req.Role).Msg("approve: no session for platform")
			return
		}

		b := &Bot{
			cfg: Config{
				Platform:     domain.Platform(req.Platform),
				Settings:     gs,
				Cookies:      cookies,
				DB:           m.db,
				UserID:       userID,
				Tailor:       m.tailor,
				Scorer:       m.scorer,
				HalalChecker: m.halalCheckerFor(gs),
				Renderer:     m.renderer,
				Profile:      &profile,
				MarketDir:    m.marketDir,
			},
			state:  domain.BotStateIdle,
			stopCh: make(chan struct{}),
		}

		br, jobPage, err := b.launchBrowser(ctx)
		if err != nil {
			log.Error().Err(err).Str("job", req.Role).Msg("approve: launch browser")
			return
		}
		if gs.Browser.RemoteDebugPort == 0 {
			defer br.Close()
		}

		if err := jobPage.Navigate(req.Link); err != nil {
			log.Error().Err(err).Str("job", req.Role).Msg("approve: navigate to job")
			jobPage.Close()
			return
		}
		defer jobPage.Close()

		var score int
		_ = m.db.QueryRow(
			`SELECT COALESCE(suitability_score,0) FROM jobs_pending_review WHERE job_id = ? AND user_id = ?`,
			req.JobID, userID,
		).Scan(&score)

		if err := b.easyApply(ctx, jobPage, req.ResumePath, req.CoverPath); err != nil {
			if errors.Is(err, errAlreadyApplied) {
				log.Info().Str("company", req.Company).Str("job", req.Role).Msg("approve: already applied, recording ✓")
			} else {
				log.Error().Err(err).Str("company", req.Company).Str("job", req.Role).Msg("approve: easy apply failed")
				_, _ = m.db.Exec(
					`INSERT OR IGNORE INTO jobs_skipped(id,user_id,platform,company,role,location,link,skip_reason,suitability_score,suitability_reasoning,viewed_at)
					 VALUES(?,?,?,?,?,?,?,'easy apply: '||?,?,?,datetime('now'))`,
					req.JobID, userID, req.Platform, req.Company, req.Role, req.Location, req.Link, err.Error(), score, "",
				)
				return
			}
		}

		_, _ = m.db.Exec(
			`INSERT OR IGNORE INTO jobs_applied(id,user_id,platform,company,role,location,link,resume_path,cover_letter_path,applied_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?)`,
			req.JobID, userID, req.Platform, req.Company, req.Role, req.Location, req.Link, req.ResumePath, req.CoverPath,
			time.Now().UTC().Format(time.RFC3339),
		)
		_, _ = m.db.Exec(`DELETE FROM jobs_approved_queue WHERE job_id = ? AND user_id = ?`, req.JobID, userID)
		log.Info().Str("company", req.Company).Str("job", req.Role).Msg("approve: submitted ✓")
	}()
}

func (m *Manager) buildConfig(ctx context.Context, userID string, platform domain.Platform) (*Config, error) {
	var gs domain.GeneralSettings
	_ = m.cfgStore.Get(userID, "general_settings", &gs)
	if gs.HumanBehavior.DailyApplicationLimit == 0 {
		gs.HumanBehavior.DailyApplicationLimit = 40
	}

	var prefs domain.WorkPreferences
	_ = m.cfgStore.Get(userID, "work_preferences", &prefs)

	var profile domain.ResumeProfile
	if err := m.cfgStore.Get(userID, "resume_profile", &profile); err != nil {
		return nil, errors.New("no resume profile saved — add one at /api/settings/resume first")
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
		return nil, errors.New("no saved session for " + string(resolved) + " — log in via Settings → Secrets first")
	}

	return &Config{
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
		Tailor:        m.tailor,
		Scorer:        m.scorer,
		HalalChecker:  m.halalCheckerFor(gs),
		Renderer:      m.renderer,
		DB:            m.db,
		UserID:        userID,
		RequireReview: gs.RequireReview,
		MarketDir:     m.marketDir,
	}, nil
}

// halalCheckerFor returns the halal checker when the setting is enabled, or nil.
func (m *Manager) halalCheckerFor(gs domain.GeneralSettings) JobHalalChecker {
	if gs.HalalJobFilter {
		return m.halal
	}
	return nil
}
