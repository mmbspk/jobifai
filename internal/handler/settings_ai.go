package handler

import (
	"encoding/json"
	"net/http"

	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

// SettingsAIHandlers handles tester-only personal AI provider endpoints.
type SettingsAIHandlers struct{ svc *Services }

func NewSettingsAIHandlers(svc *Services) *SettingsAIHandlers { return &SettingsAIHandlers{svc: svc} }

// GET /api/settings/ai-provider
func (h *SettingsAIHandlers) Get(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var overrides domain.LLMOverrides
	_ = h.svc.Config.Get(userID, config.KeyLLMOverrides, &overrides)
	hasKey := h.svc.Secrets.Has(userID, "llm_api_key")

	activeSource := "admin_default"
	if hasKey {
		activeSource = "personal"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"provider":      overrides.Provider,
		"model":         overrides.Model,
		"has_key":       hasKey,
		"active_source": activeSource,
	})
}

// PUT /api/settings/ai-provider
func (h *SettingsAIHandlers) Update(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid json"})
		return
	}
	validProviders := map[string]bool{"claude": true, "openai": true, "gemini": true, "ollama": true}
	if req.Provider != "" && !validProviders[req.Provider] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "provider must be one of: claude, openai, gemini, ollama"})
		return
	}
	// Always set the full override struct from the request, even when fields are empty.
	// Empty string clears that override so the system default takes effect.
	var overrides domain.LLMOverrides
	_ = h.svc.Config.Get(userID, config.KeyLLMOverrides, &overrides)
	overrides.Provider = req.Provider
	overrides.Model = req.Model
	if err := h.svc.Config.Set(userID, config.KeyLLMOverrides, overrides); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "AI provider settings saved")
}

// POST /api/settings/ai-provider/test
func (h *SettingsAIHandlers) Test(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if h.svc.LLMConnectionTester == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "connection testing not available"})
		return
	}
	provider, model, latencyMS, err := h.svc.LLMConnectionTester(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":    false,
			"latency_ms": latencyMS,
			"provider":   provider,
			"model":      model,
			"error":      err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"latency_ms": latencyMS,
		"provider":   provider,
		"model":      model,
	})
}
