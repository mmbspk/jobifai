package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/billing"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/llmreuse"
	"github.com/user/jobifai/internal/llmpolicy"
	"github.com/user/jobifai/internal/retention"
	"github.com/user/jobifai/internal/pricing"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/usage"
	"github.com/user/jobifai/internal/resume"
	jobws "github.com/user/jobifai/internal/ws"
	_ "github.com/user/jobifai/internal/db" // imported for IncrementUsage via alias below
)

func main() {
	addr := flag.String("addr", ":8081", "HTTP listen address")
	dbPath := flag.String("db", "data/jobifai.db", "SQLite database path")
	flag.Parse()

	// Lifetime context cancelled on graceful shutdown; passed to long-running goroutines.
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()

	// ── Logger + WebSocket broadcaster ──────────────────────────────────
	logBroadcaster := jobws.NewBroadcaster()
	log.Logger = log.Output(zerolog.MultiLevelWriter(
		zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339},
		logBroadcaster,
	))

	// ── Database ────────────────────────────────────────────────────────
	if err := os.MkdirAll("data", 0o750); err != nil {
		log.Fatal().Err(err).Msg("create data dir")
	}
	database, err := db.Open(*dbPath)
	if err != nil {
		log.Fatal().Err(err).Msg("open database")
	}
	defer func() { _ = database.Close() }()
	log.Info().Str("path", *dbPath).Msg("database ready")
	if err := billing.ValidateProductionStripeConfig(); err != nil {
		log.Fatal().Err(err).Msg("stripe configuration")
	}

	// ── Config + Secrets ────────────────────────────────────────────────
	machineKey, err := config.MachineKey(database)
	if err != nil {
		log.Fatal().Err(err).Msg("machine key")
	}
	cfgStore := config.NewStore(database)
	secretsStore := config.NewSecretsStore(database, machineKey)

	// ── Auth ────────────────────────────────────────────────────────────
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		// Auto-generate and persist so restarts don't invalidate existing tokens.
		if stored, err := secretsStore.Get("__system__", "jwt_secret"); err == nil && stored != "" {
			jwtSecret = stored
		} else {
			raw, genErr := auth.GenerateRefreshToken() // re-use random-bytes generator
			if genErr != nil {
				log.Fatal().Err(genErr).Msg("generate jwt secret")
			}
			jwtSecret = raw
			_ = secretsStore.Set("__system__", "jwt_secret", jwtSecret)
		}
	}
	tokenManager := auth.NewTokenManager(jwtSecret)
	userStore := auth.NewUserStore(database)
	logBroadcaster.SetStreamPolicy(jobws.StreamPolicy{
		Production: billing.IsProduction(),
		Verbose:    userStore.VerboseLogsEnabled,
	})
	quotaSvc := quota.NewService(database, cfgStore, userStore)

	// ── Google OAuth (optional, requires env vars) ──────────────────────
	var googleHandler handler.GoogleOAuthHandler
	googleClientID := os.Getenv("GOOGLE_CLIENT_ID")
	googleClientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	googleRedirectURL := os.Getenv("GOOGLE_REDIRECT_URL")
	if googleClientID != "" && googleClientSecret != "" && googleRedirectURL != "" {
		googleHandler = auth.NewGoogleHandler(
			auth.GoogleConfig{
				ClientID:     googleClientID,
				ClientSecret: googleClientSecret,
				RedirectURL:  googleRedirectURL,
			},
			database,
			tokenManager,
			func(ctx context.Context, googleID, email, name, avatar string) (string, error) {
				u, err := userStore.UpsertGoogle(googleID, email, name, avatar)
				if err != nil {
					return "", err
				}
				_ = quotaSvc.InitTrial(ctx, u.ID)
				return u.ID, nil
			},
		)
		log.Info().Msg("Google OAuth enabled")
	} else {
		log.Info().Msg("Google OAuth disabled (set GOOGLE_CLIENT_ID/SECRET/REDIRECT_URL to enable)")
	}

	// ── Browser + Session store ─────────────────────────────────────────
	if err := browser.InitVirtualDisplay(); err != nil {
		log.Warn().Err(err).Msg("virtual display not available — Connect browser needs Docker/Xvfb on Linux servers")
	}
	browserMgr := browser.NewManager()
	sessionStore := browser.NewSessionStore(database, secretsStore)

	catalog := pricing.DefaultCatalog()
	policyStore := &llmpolicy.Store{DB: database}
	usageLedger := &usage.Ledger{
		DB:      database,
		Catalog: catalog,
		Quota:   quotaSvc,
		Defaults: func() domain.QuotaDefaults {
			return quotaSvc.LoadDefaults()
		},
	}
	usageStore := llm.NewUserUsageStore()
	quotaSvc.SetPricingCatalog(catalog)
	llmReuseStore := &llmreuse.Store{DB: database}

	// ── LLM deps (nil if no API key stored yet) ─────────────────────────
	extractor, tailor, renderer, llmClient := buildLLMDeps("__default__", cfgStore, secretsStore, usageStore.For("__default__"), quotaSvc, usageLedger, policyStore, catalog, llmReuseStore)

	// ── Bot manager ──────────────────────────────────────────────────────
	var botTailor bot.ResumeTailor
	var botRenderer bot.ResumeRenderer
	var botScorer bot.JobScorer
	var botHalalChecker bot.JobHalalChecker
	if tailor != nil {
		botTailor = tailor.(bot.ResumeTailor)
	}
	botRenderer = renderer.(bot.ResumeRenderer)
	if llmClient != nil {
		var gs domain.GeneralSettings
		_ = cfgStore.Get("__default__", "general_settings", &gs)
		if scoreC, err := taskApply(llmClient, gs, policyStore, catalog, "scoring"); err == nil {
			botScorer = resume.NewScorer(scoreC)
		}
		if halalC, err := taskApply(llmClient, gs, policyStore, catalog, "halal"); err == nil {
			botHalalChecker = resume.NewHalalChecker(halalC)
		}
	}
	botMgr := bot.NewManager(shutdownCtx, database, cfgStore, secretsStore, sessionStore, botTailor, botScorer, botHalalChecker, botRenderer, "resume_markets")

	botMgr.SetSessionQuota(quotaSvc)
	botMgr.SetLLMQuota(quotaSvc)
	botMgr.SetLLMBilling(policyStore, catalog, usageLedger)

	const marketDir = "resume_markets"
	if n := countYAMLFiles(marketDir); n == 0 {
		log.Warn().Str("dir", marketDir).Msg("resume markets missing — Default market and Generate presets will be empty; ship resume_markets/ with the binary or rebuild the Docker image")
	} else {
		log.Info().Str("dir", marketDir).Int("markets", n).Msg("resume markets loaded from disk")
	}

	docStorageRoot := os.Getenv("JOBIFAI_DOCUMENTS_STORAGE")
	if docStorageRoot == "" {
		docStorageRoot = filepath.Join("data", "user_documents")
	}
	docBlobs, err := documents.NewLocalBlobStore(docStorageRoot)
	if err != nil {
		log.Fatal().Err(err).Str("root", docStorageRoot).Msg("document blob storage")
	}
	docStore := documents.NewStore(database)
	docSvc := &documents.Service{
		Store:     docStore,
		Blobs:     docBlobs,
		Renderer:  renderer,
		MarketDir: marketDir,
		StylesDir: documents.StylesDirRelative,
		LoadProfile: func(userID string) (*domain.ResumeProfile, error) {
			var p domain.ResumeProfile
			if err := cfgStore.Get(userID, "resume_profile", &p); errors.Is(err, domain.ErrNotFound) {
				return nil, nil
			} else if err != nil {
				return nil, err
			}
			return &p, nil
		},
		DefaultsMeta: func(userID string) (documents.DefaultsMeta, error) {
			var m documents.DefaultsMeta
			if err := cfgStore.Get(userID, "document_defaults_meta", &m); errors.Is(err, domain.ErrNotFound) {
				return documents.DefaultsMeta{}, nil
			} else if err != nil {
				return documents.DefaultsMeta{}, err
			}
			return documents.CoalesceDefaultsMeta(m), nil
		},
		SaveDefaultsMeta: func(userID string, m documents.DefaultsMeta) error {
			return cfgStore.Set(userID, "document_defaults_meta", m)
		},
	}
	log.Info().Str("root", docStorageRoot).Msg("document storage (local; transitional — plan durable object storage for production scale)")
	retentionCoord := &retention.ActivityCoordinator{}
	docSvc.WorkGuard = retentionCoord
	docSvc.Metrics = &documents.ServiceMetrics{}
	botMgr.SetDocuments(docSvc)
	botMgr.SetSubmitWorkGuard(retentionCoord)
	retentionSvc := &retention.Service{
		DB:       database,
		Config:   cfgStore,
		Root:     ".",
		Blobs:    docBlobs,
		Activity: retentionCoord,
	}
	botMgr.SetRetention(retentionSvc)
	botMgr.SetLLMReuse(llmReuseStore)

	// ── Router ──────────────────────────────────────────────────────────
	svc := &handler.Services{
		StartedAt:    time.Now(),
		DB:           database,
		Config:       cfgStore,
		Secrets:      secretsStore,
		Extractor:    extractor,
		Tailor:       tailor,
		Renderer:     renderer,
		BrowserMgr:   browserMgr,
		SessionStore: sessionStore,
		Logs:         logBroadcaster,
		Bot:          botMgr,
		MarketDir:    marketDir,
		StylesDir:    documents.StylesDirRelative,
		FileToText:   resume.TextFromReader,
		FetchJobPage: resume.FetchJobPage,
		MarketLoader: func(path, yamlFile string) (*domain.ResumeMarket, error) {
			m, err := resume.LoadMarket(path)
			if err != nil {
				return nil, err
			}
			return &domain.ResumeMarket{
				Name: m.Name, YAMLFile: yamlFile, HasCSS: m.CSSFile != "",
				Locale: m.Locale, DocumentLanguage: m.DocumentLanguage,
				RegionGroup: m.RegionGroup, PageSize: m.PageSize,
			}, nil
		},
		MarketPrefixLookup: func(marketDir, name, section string) string {
			m := resume.LoadMarketByName(marketDir, name)
			if m == nil {
				return ""
			}
			switch section {
			case "resume":
				return m.ResumePrompt
			case "tailored":
				return m.TailoredPrompt
			case "cover":
				return m.CoverLetterPrompt
			}
			return ""
		},
		MarketCSSFileLookup: func(marketDir, name string) string {
			m := resume.LoadMarketByName(marketDir, name)
			if m == nil {
				return ""
			}
			return m.CSSFile
		},
		Users:        userStore,
		TokenManager: tokenManager,
		Google:       googleHandler,
		UsageStore:   &usageStoreAdapter{s: usageStore},
		Quota:        quotaSvc,
		HTTPClient:   &http.Client{Timeout: 5 * time.Second},
		LLMFactory: func(userID string) (handler.ResumeExtractor, handler.ResumeTailor) {
			e, t, _, _ := buildLLMDeps(userID, cfgStore, secretsStore, usageStore.For(userID), quotaSvc, usageLedger, policyStore, catalog, llmReuseStore)
			return e, t
		},
		EvaluatorFactory: func(userID string) handler.JobEvaluator {
			_, _, _, client := buildLLMDeps(userID, cfgStore, secretsStore, usageStore.For(userID), quotaSvc, usageLedger, policyStore, catalog, llmReuseStore)
			if client == nil {
				return nil
			}
			gs := config.ResolveOperationalSettings(cfgStore, userID)
			scoreC, err := taskApply(client, gs, policyStore, catalog, "scoring")
			if err != nil {
				return nil
			}
			return resume.NewScorer(scoreC)
		},
		HalalCheckerFactory: func(userID string) handler.JobHalalChecker {
			_, _, _, client := buildLLMDeps(userID, cfgStore, secretsStore, usageStore.For(userID), quotaSvc, usageLedger, policyStore, catalog, llmReuseStore)
			if client == nil {
				return nil
			}
			gs := config.ResolveOperationalSettings(cfgStore, userID)
			halalC, err := taskApply(client, gs, policyStore, catalog, "halal")
			if err != nil {
				return nil
			}
			return resume.NewHalalChecker(halalC)
		},
		Documents: docSvc,
		Retention: retentionSvc,
		LLMReuseMetrics: llmReuseStore,
		QuestionAnswererFactory: func(userID string) handler.JobQuestionAnswerer {
			_, _, _, client := buildLLMDeps(userID, cfgStore, secretsStore, usageStore.For(userID), quotaSvc, usageLedger, policyStore, catalog, llmReuseStore)
			if client == nil {
				return nil
			}
			gs := config.ResolveOperationalSettings(cfgStore, userID)
			qC, err := taskApply(client, gs, policyStore, catalog, "questions")
			if err != nil {
				return nil
			}
			return resume.NewQuestionAnswerer(qC)
		},
	}
	router := handler.NewRouter(svc)

	// ── HTTP server ─────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         *addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Info().Str("addr", *addr).Msg("jobifai server starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	// ── Graceful shutdown ───────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("")
	log.Info().Msg("shutting down...")
	shutdownCancel() // signal background goroutines (e.g. SubmitNow) to stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown failed")
	}
	log.Info().Msg("bye")
}

// buildLLMDeps returns an Extractor, Tailor, PDFRenderer, and raw LLM client from stored config.
// userID scopes the config/secrets lookup; pass "__default__" for startup bootstrapping.
// Extractor/Tailor/Client are nil if no API key is saved yet.
// tracker is optional; if non-nil the returned client will accumulate token usage into it.
func buildLLMDeps(userID string, cfgStore *config.Store, secrets *config.SecretsStore, tracker *llm.UsageTracker, quotaGuard quota.LLMGuard, ledger *usage.Ledger, policyStore *llmpolicy.Store, catalog *pricing.Catalog, reuseStore *llmreuse.Store) (handler.ResumeExtractor, handler.ResumeTailor, handler.ResumeRenderer, *llm.Client) {
	renderer := resume.NewPDFRenderer("resume_style")

	gs := config.ResolveOperationalSettings(cfgStore, userID)
	apiKey, err := config.ResolveLLMAPIKey(secrets, userID, gs.LLM.UseProxy)
	if err != nil || apiKey == "" {
		return nil, nil, renderer, nil
	}

	client := llm.New(gs.LLM, apiKey).WithUserID(userID)
	if tracker != nil {
		client = client.WithTracker(tracker)
	}
	if quotaGuard != nil && userID != "" && userID != "__default__" {
		client = client.WithQuota(quotaGuard, userID)
	}
	if ledger != nil {
		client = client.WithBilling(llm.BillingHooks{Ledger: ledger})
	}
	if reuseStore != nil {
		client = client.WithReuse(&llm.ReuseCoordinator{Store: reuseStore})
	}
	extractC := taskApplyWithFallback(client, gs, policyStore, catalog, domain.TaskResumeExtract, userID)
	tailorC := taskApplyWithFallback(client, gs, policyStore, catalog, "tailoring", userID)
	coverC := taskApplyWithFallback(client, gs, policyStore, catalog, "cover_letter", userID)
	formAnswerC := taskApplyWithFallback(client, gs, policyStore, catalog, domain.TaskFormAnswer, userID)
	formVisionC := taskApplyWithFallback(client, gs, policyStore, catalog, domain.TaskFormVision, userID)
	tailor := resume.NewTailor(tailorC, coverC, formAnswerC, formVisionC)
	return resume.NewExtractor(extractC), tailor, renderer, client
}

// taskApplyWithFallback uses the base client for legacy/user misconfiguration only.
// Invalid approved system policies fail loudly (HTTP handlers may still fall back to base).
func taskApplyWithFallback(base *llm.Client, gs domain.GeneralSettings, policyStore *llmpolicy.Store, catalog *pricing.Catalog, task, userID string) *llm.Client {
	c, err := taskApply(base, gs, policyStore, catalog, task)
	if err != nil {
		if errors.Is(err, llmpolicy.ErrApprovedPolicy) {
			log.Error().Err(err).Str("event", "approved_task_policy_invalid").Str("user_id", userID).Str("task", task).
				Msg("approved task policy invalid; refusing task override (using base client for this request)")
		} else {
			log.Error().Err(err).Str("user_id", userID).Str("task", task).Msg("task model resolution failed; using base LLM client")
		}
		return base
	}
	if c == nil {
		return base
	}
	return c
}

func taskApply(base *llm.Client, gs domain.GeneralSettings, policyStore *llmpolicy.Store, catalog *pricing.Catalog, task string) (*llm.Client, error) {
	var pol *domain.TaskModelPolicyRow
	if policyStore != nil {
		p, err := policyStore.ApprovedPolicy(task)
		if err != nil {
			return nil, err
		}
		pol = p
	}
	return llmpolicy.ApplyTask(base, gs.LLM, gs.LLM.TaskModels, pol, catalog, task)
}

// usageStoreAdapter adapts *llm.UserUsageStore to the handler.UsageStore interface.
type usageStoreAdapter struct{ s *llm.UserUsageStore }

func (a *usageStoreAdapter) Session(userID string) domain.SessionUsage {
	snap := a.s.For(userID).Snapshot()
	return domain.SessionUsage{
		InputTokens:  snap.InputTokens,
		OutputTokens: snap.OutputTokens,
		Calls:        snap.Calls,
	}
}

func countYAMLFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".yaml" {
			n++
		}
	}
	return n
}
