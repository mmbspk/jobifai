package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/resume"
	jobws "github.com/user/jobifai/internal/ws"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", "data/jobifai.db", "SQLite database path")
	flag.Parse()

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
	defer database.Close()
	log.Info().Str("path", *dbPath).Msg("database ready")

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

	// ── Google OAuth (optional — requires env vars) ──────────────────────
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
			func(_ context.Context, googleID, email, name, avatar string) (string, error) {
				u, err := userStore.UpsertGoogle(googleID, email, name, avatar)
				if err != nil {
					return "", err
				}
				return u.ID, nil
			},
		)
		log.Info().Msg("Google OAuth enabled")
	} else {
		log.Info().Msg("Google OAuth disabled (set GOOGLE_CLIENT_ID/SECRET/REDIRECT_URL to enable)")
	}

	// ── Browser + Session store ─────────────────────────────────────────
	browserMgr := browser.NewManager()
	sessionStore := browser.NewSessionStore(database, secretsStore)

	// ── LLM deps (nil if no API key stored yet) ─────────────────────────
	extractor, tailor, renderer, llmClient := buildLLMDeps("__default__", cfgStore, secretsStore, nil)

	// ── Bot manager ──────────────────────────────────────────────────────
	var botTailor bot.ResumeTailor
	var botRenderer bot.ResumeRenderer
	var botScorer bot.JobScorer
	var botHalalChecker bot.JobHalalChecker
	if tailor != nil {
		botTailor = tailor.(bot.ResumeTailor)
	}
	if renderer != nil {
		botRenderer = renderer.(bot.ResumeRenderer)
	}
	if llmClient != nil {
		botScorer = resume.NewScorer(llmClient)
		botHalalChecker = resume.NewHalalChecker(llmClient)
	}
	botMgr := bot.NewManager(database, cfgStore, secretsStore, sessionStore, botTailor, botScorer, botHalalChecker, botRenderer, "resume_markets")

	// ── Usage tracking ───────────────────────────────────────────────────
	usageStore := llm.NewUserUsageStore()

	// ── Router ──────────────────────────────────────────────────────────
	svc := &handler.Services{
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
		MarketDir:    "resume_markets",
		Users:        userStore,
		TokenManager: tokenManager,
		Google:       googleHandler,
		UsageStore:   usageStore,
		LLMFactory: func(userID string) (handler.ResumeExtractor, handler.ResumeTailor) {
			e, t, _, _ := buildLLMDeps(userID, cfgStore, secretsStore, usageStore.For(userID))
			return e, t
		},
		EvaluatorFactory: func(userID string) handler.JobEvaluator {
			_, _, _, client := buildLLMDeps(userID, cfgStore, secretsStore, usageStore.For(userID))
			if client == nil {
				return nil
			}
			return resume.NewScorer(client)
		},
		HalalCheckerFactory: func(userID string) handler.JobHalalChecker {
			_, _, _, client := buildLLMDeps(userID, cfgStore, secretsStore, usageStore.For(userID))
			if client == nil {
				return nil
			}
			return resume.NewHalalChecker(client)
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

	fmt.Println()
	log.Info().Msg("shutting down...")
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
func buildLLMDeps(userID string, cfgStore *config.Store, secrets *config.SecretsStore, tracker *llm.UsageTracker) (handler.ResumeExtractor, handler.ResumeTailor, handler.ResumeRenderer, *llm.Client) {
	renderer := resume.NewPDFRenderer("resume_style")

	var gs domain.GeneralSettings
	if err := cfgStore.Get(userID, "general_settings", &gs); err != nil {
		gs = domain.GeneralSettings{
			LLM: domain.LLMConfig{Provider: "claude", Model: "claude-sonnet-4-6"},
		}
	}

	// When proxy is enabled, prefer proxy_key (e.g. HAI proxy key) over llm_api_key.
	var apiKey string
	if gs.LLM.UseProxy {
		if pk, err := secrets.Get(userID, "proxy_key"); err == nil && pk != "" {
			apiKey = pk
		}
	}
	if apiKey == "" {
		var err error
		apiKey, err = secrets.Get(userID, "llm_api_key")
		if err != nil {
			return nil, nil, renderer, nil
		}
	}

	client := llm.New(gs.LLM, apiKey)
	if tracker != nil {
		client = client.WithTracker(tracker)
	}
	return resume.NewExtractor(client), resume.NewTailor(client), renderer, client
}
