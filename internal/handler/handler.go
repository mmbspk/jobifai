// Package handler contains all HTTP handlers for the jobifai API.
package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/ws"
)

// ResumeExtractor is the interface the upload handler uses to parse resume files.
type ResumeExtractor interface {
	ExtractFromText(ctx context.Context, text string) (*domain.ResumeProfile, error)
}

// JobEvaluator scores how well a candidate profile matches a job description.
type JobEvaluator interface {
	EvaluateJob(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (domain.JobScore, error)
}

// Services is the dependency bag threaded into each handler group.
type Services struct {
	DB           *sql.DB
	Config       ConfigStore
	Secrets      SecretsStore
	Extractor    ResumeExtractor
	Tailor       ResumeTailor
	Renderer     ResumeRenderer
	BrowserMgr   *browser.Manager
	SessionStore *browser.SessionStore
	Logs         *ws.Broadcaster
	Bot          BotController
	MarketDir    string // directory containing market_*.yaml files
	Users        *auth.UserStore
	TokenManager *auth.TokenManager
	Google       GoogleOAuthHandler
	// LLMFactory builds a fresh Extractor+Tailor for the given user from the
	// current stored config/secrets. Called per-request so changes take effect
	// immediately without a server restart.
	LLMFactory func(userID string) (ResumeExtractor, ResumeTailor)
	// EvaluatorFactory builds a fresh JobEvaluator for the given user.
	// Returns nil if no API key is configured.
	EvaluatorFactory func(userID string) JobEvaluator
	// HalalCheckerFactory builds a fresh HalalChecker for the given user.
	// Returns nil if no API key is configured.
	HalalCheckerFactory func(userID string) JobHalalChecker
	// UsageStore accumulates per-user LLM token usage for the lifetime of the process.
	UsageStore *llm.UserUsageStore
}

// GoogleOAuthHandler handles the Google OAuth2 redirect + callback.
type GoogleOAuthHandler interface {
	Redirect(w http.ResponseWriter, r *http.Request)
	Callback(w http.ResponseWriter, r *http.Request)
}

// BotController is the interface bot handlers use to start/stop/query the bot.
type BotController interface {
	Start(ctx context.Context, userID string, platform domain.Platform) error
	Stop(userID string)
	Status(userID string) domain.BotStatus
	SubmitNow(userID string, req bot.SubmitRequest)
}

// ResumeTailor rewrites a profile for a job and writes cover letters.
type ResumeTailor interface {
	TailorProfile(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (*domain.ResumeProfile, error)
	WriteCoverLetter(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (string, error)
}

// JobHalalChecker evaluates whether a job is permissible under Islamic employment ethics.
type JobHalalChecker interface {
	CheckHalal(ctx context.Context, title, company, description string) (domain.HalalVerdict, error)
}

// ResumeRenderer turns a profile into a PDF byte slice.
type ResumeRenderer interface {
	RenderResume(ctx context.Context, profile *domain.ResumeProfile, styleName, cssOverride string) ([]byte, error)
	RenderCoverLetter(ctx context.Context, body string, styleName, cssOverride string) ([]byte, error)
}

// ConfigStore is the minimal interface handlers need for settings persistence.
type ConfigStore interface {
	Get(userID, key string, dst any) error
	Set(userID, key string, src any) error
}

// SecretsStore is the minimal interface handlers need for secret management.
type SecretsStore interface {
	Set(userID, key, value string) error
	Get(userID, key string) (string, error)
	Has(userID, key string) bool
	Delete(userID, key string) error
}

// ─── helpers ───────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func okMsg(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusOK, map[string]string{"message": msg})
}

func notImpl(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"message": "not implemented"})
}

func notFound(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusNotFound, map[string]string{"message": msg})
}

func conflict(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusConflict, map[string]string{"message": msg})
}

func unprocessable(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": msg})
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}
