package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
	resumepkg "github.com/user/jobifai/internal/resume"
	"gopkg.in/yaml.v3"
)

const (
	keyGeneralSettings  = "general_settings"
	keyWorkPreferences  = "work_preferences"
	keyResumeProfile    = "resume_profile"
	keyResumeStylesDir  = "resume_style"
)

// SettingsHandlers groups all settings/configuration handlers.
type SettingsHandlers struct{ svc *Services }

func NewSettingsHandlers(svc *Services) *SettingsHandlers { return &SettingsHandlers{svc: svc} }

// ── Resume profile ─────────────────────────────────────────────────────────

// GET /api/settings/resume
func (h *SettingsHandlers) ResumeGet(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var p domain.ResumeProfile
	if err := h.svc.Config.Get(userID, keyResumeProfile, &p); errors.Is(err, config.ErrNotFound) {
		notFound(w, "no resume profile saved yet")
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// POST /api/settings/resume
func (h *SettingsHandlers) ResumeSet(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var p domain.ResumeProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if err := h.svc.Config.Set(userID, keyResumeProfile, p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "resume profile saved")
}

// POST /api/settings/resume/upload
// Accepts a plain-text resume file. Extracts a structured ResumeProfile via LLM
// and returns it for the caller to review before saving (do NOT auto-save).
// PDF/DOCX parsing is added in Phase 4.
func (h *SettingsHandlers) ResumeUpload(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	extractor, _ := h.svc.LLMFactory(userID)
	if extractor == nil {
		unprocessable(w, "LLM not configured — set an API key in Settings → Secrets first")
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "could not parse multipart form"})
		return
	}
	f, fh, err := r.FormFile("resume_file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "resume_file field missing"})
		return
	}
	defer f.Close()

	text, err := resumepkg.TextFromReader(f, fh.Filename)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not read file: " + err.Error()})
		return
	}

	profile, err := extractor.ExtractFromText(r.Context(), text)
	if err != nil {
		unprocessable(w, "LLM extraction failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

// GET /api/settings/resume/download
func (h *SettingsHandlers) ResumeDownload(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var p domain.ResumeProfile
	if err := h.svc.Config.Get(userID, keyResumeProfile, &p); errors.Is(err, config.ErrNotFound) {
		notFound(w, "no resume profile saved yet")
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	out, err := yaml.Marshal(p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Content-Disposition", `attachment; filename="resume_profile.yaml"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// ── General settings ───────────────────────────────────────────────────────

var defaultGeneralSettings = domain.GeneralSettings{
	LLM: domain.LLMConfig{
		Provider: "claude",
		Model:    "claude-sonnet-4-6",
		UseProxy: false,
	},
	Browser: domain.BrowserConfig{
		ShowBrowser:     true,
		UseChromeProfile: true,
		RemoteDebugPort: 9222,
	},
	HumanBehavior: domain.HumanBehaviorConfig{
		DailyApplicationLimit: 40,
		JobReadTimeMin:        10,
		JobReadTimeMax:        30,
		PauseBetweenJobsMin:   5,
		PauseBetweenJobsMax:   15,
	},
	JobSuitabilityScore: 7,
	MaxJobsPerKeyword:   25,
}

// GET /api/settings/general
func (h *SettingsHandlers) GeneralGet(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var s domain.GeneralSettings
	if err := h.svc.Config.Get(userID, keyGeneralSettings, &s); errors.Is(err, config.ErrNotFound) {
		writeJSON(w, http.StatusOK, defaultGeneralSettings)
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// POST /api/settings/general
func (h *SettingsHandlers) GeneralSet(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var s domain.GeneralSettings
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if err := h.svc.Config.Set(userID, keyGeneralSettings, s); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "general settings saved")
}

// ── Work preferences ───────────────────────────────────────────────────────

// GET /api/settings/preferences
func (h *SettingsHandlers) PreferencesGet(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var p domain.WorkPreferences
	if err := h.svc.Config.Get(userID, keyWorkPreferences, &p); errors.Is(err, config.ErrNotFound) {
		writeJSON(w, http.StatusOK, domain.WorkPreferences{})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// POST /api/settings/preferences
func (h *SettingsHandlers) PreferencesSet(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var p domain.WorkPreferences
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if err := h.svc.Config.Set(userID, keyWorkPreferences, p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "work preferences saved")
}

// ── Secrets ────────────────────────────────────────────────────────────────

// GET /api/settings/secrets
func (h *SettingsHandlers) SecretsGet(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	out := domain.SecretsConfig{}
	if h.svc.Secrets.Has(userID, "llm_api_key") {
		out.LLMAPIKey = "****"
	}
	if h.svc.Secrets.Has(userID, "proxy_key") {
		out.ProxyKey = "****"
	}
	for _, p := range []domain.Platform{domain.PlatformLinkedIn, domain.PlatformSeek, domain.PlatformIndeed} {
		if h.svc.Secrets.Has(userID, "cred:"+string(p)) {
			out.CredentialPlatforms = append(out.CredentialPlatforms, p)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/settings/secrets/api-key
func (h *SettingsHandlers) SecretsSetAPIKey(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		KeyType string `json:"key_type"`
		Value   string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if req.KeyType != "llm_api_key" && req.KeyType != "proxy_key" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "key_type must be llm_api_key or proxy_key"})
		return
	}
	if err := h.svc.Secrets.Set(userID, req.KeyType, req.Value); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "API key saved")
}

// POST /api/settings/secrets/credentials
func (h *SettingsHandlers) SecretsSetCredentials(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		Platform string `json:"platform"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	type cred struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	raw, _ := json.Marshal(cred{Email: req.Email, Password: req.Password})
	if err := h.svc.Secrets.Set(userID, "cred:"+req.Platform, string(raw)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "credentials saved")
}

// ── Styles ─────────────────────────────────────────────────────────────────

// GET /api/settings/styles
func (h *SettingsHandlers) StylesList(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(keyResumeStylesDir)
	if err != nil {
		// dir doesn't exist yet — return empty list
		writeJSON(w, http.StatusOK, []domain.ResumeStyle{})
		return
	}
	var styles []domain.ResumeStyle
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".css") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".css")
		name := strings.ReplaceAll(strings.TrimPrefix(stem, "style_"), "_", " ")
		// Title-case each word
		words := strings.Fields(name)
		for i, w := range words {
			if len(w) > 0 {
				words[i] = strings.ToUpper(w[:1]) + w[1:]
			}
		}
		styles = append(styles, domain.ResumeStyle{
			Name:    strings.Join(words, " "),
			CSSFile: filepath.Join(keyResumeStylesDir, e.Name()),
		})
	}
	if styles == nil {
		styles = []domain.ResumeStyle{}
	}
	writeJSON(w, http.StatusOK, styles)
}

// ── Markets ────────────────────────────────────────────────────────────────

// GET /api/settings/markets
func (h *SettingsHandlers) MarketsList(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.svc.MarketDir)
	if err != nil {
		writeJSON(w, http.StatusOK, []domain.ResumeMarket{})
		return
	}
	var markets []domain.ResumeMarket
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		m, err := resumepkg.LoadMarket(filepath.Join(h.svc.MarketDir, e.Name()))
		if err != nil {
			continue
		}
		markets = append(markets, domain.ResumeMarket{
			Name:     m.Name,
			YAMLFile: filepath.Join(h.svc.MarketDir, e.Name()),
			HasCSS:   m.CSSFile != "",
		})
	}
	if markets == nil {
		markets = []domain.ResumeMarket{}
	}
	writeJSON(w, http.StatusOK, markets)
}
