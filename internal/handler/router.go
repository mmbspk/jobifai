package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/user/jobifai/internal/auth"
)

// NewRouter constructs and returns the fully-wired chi router.
func NewRouter(svc *Services) *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.CleanPath)

	authH := NewAuthHandlers(svc)
	botH := NewBotHandlers(svc)
	jobs := NewJobHandlers(svc)
	resume := NewResumeHandlers(svc)
	settings := NewSettingsHandlers(svc)
	ws := NewWSHandlers(svc)
	users := NewUserHandlers(svc, svc.Users, svc.TokenManager, svc.DB)
	usage := NewUsageHandlers(svc)

	// ── Public: user accounts + OAuth ────────────────────────────────────
	r.Route("/auth", func(r chi.Router) {
		r.Post("/register", users.Register)
		r.Post("/login", users.Login)
		r.Post("/refresh", users.Refresh)
		r.Post("/logout", users.Logout)
		if svc.Google != nil {
			r.Get("/google", svc.Google.Redirect)
			r.Get("/google/callback", svc.Google.Callback)
		}
	})

	// ── WebSocket (public — auth is handled inside the handler) ──────────
	r.Get("/ws/logs", ws.Logs)

	// ── noVNC: websockify proxy (JWT validated inside) + static files ────
	vncH := NewVNCHandlers(svc)
	r.Get("/novnc/websockify", vncH.Websockify)
	if fi, err := os.Stat("/usr/share/novnc"); err == nil && fi.IsDir() {
		noVNCFS := http.StripPrefix("/novnc/", http.FileServer(http.Dir("/usr/share/novnc")))
		r.Get("/novnc/*", func(w http.ResponseWriter, r *http.Request) {
			noVNCFS.ServeHTTP(w, r)
		})
	}

	// ── All /api/* routes require a valid JWT ────────────────────────────
	r.Group(func(r chi.Router) {
		if svc.TokenManager != nil {
			r.Use(auth.RequireAuth(svc.TokenManager))
		}

		// ── Me ───────────────────────────────────────────────────────────
		r.Get("/api/me", users.Me)
		r.Put("/api/me", users.UpdateMe)

		// ── Platform auth (browser + session) ───────────────────────────
		r.Route("/api/auth", func(r chi.Router) {
			r.Post("/launch-browser", authH.LaunchBrowser)
			r.Post("/save-session", authH.SaveSession)
			r.Get("/{platform}/status", authH.PlatformStatus)
			r.Delete("/{platform}/session", authH.DeleteSession)
		})

		// ── Bot ──────────────────────────────────────────────────────────
		r.Route("/api/bot", func(r chi.Router) {
			r.Post("/start", botH.Start)
			r.Post("/stop", botH.Stop)
			r.Get("/status", botH.Status)
			r.Get("/review/pending", botH.ReviewListPending)
			r.Post("/review/{job_id}/approve", botH.ReviewApprove)
			r.Post("/review/{job_id}/reject", botH.ReviewReject)
		})

		// ── Jobs ─────────────────────────────────────────────────────────
		r.Route("/api/jobs", func(r chi.Router) {
			r.Get("/applied", jobs.Applied)
			r.Delete("/applied/{job_id}", jobs.DeleteApplied)
			r.Get("/skipped", jobs.Skipped)
			r.Delete("/skipped/{job_id}", jobs.DeleteSkipped)
			r.Get("/cannot-apply", jobs.CannotApply)
			r.Post("/cannot-apply/{job_id}/requeue", jobs.RequeueCannotApply)
			r.Get("/top-matches", jobs.TopMatches)
			r.Delete("/pending-review/{job_id}", jobs.DeletePendingReview)
			r.Post("/pending-review/{job_id}/mark-applied", jobs.MarkApplied)
			r.Get("/stats", jobs.Stats)
			r.Get("/{job_id}", jobs.GetJob)
		})

		// ── Resume generation ─────────────────────────────────────────
		r.Route("/api/resume", func(r chi.Router) {
			r.Post("/generate", resume.Generate)
			r.Post("/generate-tailored", resume.GenerateTailored)
			r.Post("/generate-cover-letter", resume.GenerateCoverLetter)
			r.Post("/evaluate", resume.EvaluateJob)
			r.Post("/check-halal", resume.CheckHalal)
		})

		// ── Settings ─────────────────────────────────────────────────
		r.Route("/api/settings", func(r chi.Router) {
			r.Get("/resume", settings.ResumeGet)
			r.Post("/resume", settings.ResumeSet)
			r.Post("/resume/upload", settings.ResumeUpload)
			r.Get("/resume/download", settings.ResumeDownload)
			r.Get("/general", settings.GeneralGet)
			r.Post("/general", settings.GeneralSet)
			r.Get("/preferences", settings.PreferencesGet)
			r.Post("/preferences", settings.PreferencesSet)
			r.Get("/secrets", settings.SecretsGet)
			r.Post("/secrets/api-key", settings.SecretsSetAPIKey)
			r.Post("/secrets/credentials", settings.SecretsSetCredentials)
			r.Get("/styles", settings.StylesList)
			r.Get("/markets", settings.MarketsList)
		})

		// ── Static file serving for generated PDFs ───────────────────────
		r.Get("/api/files/*", func(w http.ResponseWriter, r *http.Request) {
			p := chi.URLParam(r, "*")
			http.ServeFile(w, r, filepath.Join("job_applications", p))
		})

		// ── Usage ─────────────────────────────────────────────────────────
		r.Get("/api/usage/session", usage.Session)
	})

	// ── SPA static files ──────────────────────────────────────────────
	if h := spaHandler(); h != nil {
		r.Get("/*", h)
	}

	return r
}

func spaHandler() http.HandlerFunc {
	webDist := os.Getenv("WEB_DIST")
	if webDist == "" {
		webDist = "web/dist"
	}
	fi, err := os.Stat(webDist)
	if err != nil || !fi.IsDir() {
		return nil
	}
	fs := http.FileServer(http.Dir(webDist))
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p != "/" && strings.HasSuffix(p, "/") {
			p = strings.TrimSuffix(p, "/")
		}
		if _, err := os.Stat(filepath.Join(webDist, p)); err == nil {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(webDist, "index.html"))
	}
}
