package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/domain"
	"gopkg.in/yaml.v3"
)

const (
	keyGeneralSettings = "general_settings"
	keyWorkPreferences = "work_preferences"
	keyResumeProfile   = "resume_profile"
)

// SettingsHandlers groups all settings/configuration handlers.
type SettingsHandlers struct{ svc *Services }

func NewSettingsHandlers(svc *Services) *SettingsHandlers { return &SettingsHandlers{svc: svc} }

// ── Resume profile ─────────────────────────────────────────────────────────

// GET /api/settings/resume
func (h *SettingsHandlers) ResumeGet(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var p domain.ResumeProfile
	if err := h.svc.Config.Get(userID, keyResumeProfile, &p); errors.Is(err, domain.ErrNotFound) {
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
		unprocessable(w, "LLM not configured, set an API key in Settings → Secrets first")
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

	text, err := h.svc.FileToText(f, fh.Filename)
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
	if err := h.svc.Config.Get(userID, keyResumeProfile, &p); errors.Is(err, domain.ErrNotFound) {
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
		ShowBrowser:      true,
		UseChromeProfile: true,
		RemoteDebugPort:  9222,
	},
	HumanBehavior: domain.HumanBehaviorConfig{
		DailyApplicationLimit: 40,
		JobReadTimeMin:        10,
		JobReadTimeMax:        30,
		PauseBetweenJobsMin:   5,
		PauseBetweenJobsMax:   15,
	},
	JobSuitabilityScore:       7,
	MaxJobsPerKeyword:         25,
	InterviewQuestionsEnabled: true,
}

// GET /api/settings/general
func (h *SettingsHandlers) GeneralGet(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var s domain.GeneralSettings
	if err := h.svc.Config.Get(userID, keyGeneralSettings, &s); errors.Is(err, domain.ErrNotFound) {
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
	if err := h.svc.Config.Get(userID, keyWorkPreferences, &p); errors.Is(err, domain.ErrNotFound) {
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
	for _, p := range []domain.Platform{domain.PlatformLinkedIn, domain.PlatformSeek} {
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
	if req.KeyType != "llm_api_key" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "key_type must be llm_api_key"})
		return
	}
	if err := h.svc.Secrets.Set(userID, req.KeyType, req.Value); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "API key saved")
}

// DELETE /api/settings/secrets/api-key
func (h *SettingsHandlers) SecretsDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if err := h.svc.Secrets.Delete(userID, "llm_api_key"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "API key deleted")
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

// DELETE /api/settings/secrets/credentials?platform=<platform>
func (h *SettingsHandlers) SecretsDeleteCredentials(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	platform := r.URL.Query().Get("platform")
	if platform == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "platform is required"})
		return
	}
	if err := h.svc.Secrets.Delete(userID, "cred:"+platform); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "credentials deleted")
}

// ── Styles ─────────────────────────────────────────────────────────────────

// GET /api/settings/styles
func (h *SettingsHandlers) StylesList(w http.ResponseWriter, r *http.Request) {
	stylesDir := h.svc.StylesDir
	if stylesDir == "" {
		writeJSON(w, http.StatusOK, []domain.ResumeStyle{})
		return
	}
	entries, err := os.ReadDir(stylesDir)
	if err != nil {
		// dir doesn't exist yet, return empty list
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
			CSSFile: filepath.Join(stylesDir, e.Name()),
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
		m, err := h.svc.MarketLoader(filepath.Join(h.svc.MarketDir, e.Name()), e.Name())
		if err != nil {
			continue
		}
		markets = append(markets, *m)
	}
	if markets == nil {
		markets = []domain.ResumeMarket{}
	}
	writeJSON(w, http.StatusOK, markets)
}

// ── Location suggest ───────────────────────────────────────────────────────

func (h *SettingsHandlers) LocationSuggest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	results, err := fetchLocationSuggestions(r.Context(), q, h.svc.HTTPClient)
	if err != nil || len(results) == 0 {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func fetchLocationSuggestions(ctx context.Context, q string, client *http.Client) ([]string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	// fetch=10 so we have room to filter by importance; accept-language=en avoids Arabic-script display names
	u := "https://nominatim.openstreetmap.org/search?format=json&limit=10&addressdetails=1&featuretype=city&accept-language=en&q=" + url.QueryEscape(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "jobifai/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var hits []struct {
		DisplayName string  `json:"display_name"`
		Importance  float64 `json:"importance"`
		Address     struct {
			City    string `json:"city"`
			Town    string `json:"town"`
			Village string `json:"village"`
			State   string `json:"state"`
			Country string `json:"country"`
		} `json:"address"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&hits); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var out []string
	for _, hit := range hits {
		// Skip low-importance results (small villages, obscure places) — major cities like Adelaide score ~0.7+
		if hit.Importance < 0.45 {
			continue
		}
		city := hit.Address.City
		if city == "" {
			city = hit.Address.Town
		}
		if city == "" {
			city = hit.Address.Village
		}
		label := hit.DisplayName
		if city != "" && hit.Address.State != "" && hit.Address.Country != "" {
			label = city + ", " + hit.Address.State + ", " + hit.Address.Country
		}
		if !seen[label] {
			seen[label] = true
			out = append(out, label)
		}
	}
	return out, nil
}
