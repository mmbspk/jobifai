package handler

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
)

// GET /api/admin/app/settings
func (h *AdminHandlers) AppSettingsGet(w http.ResponseWriter, r *http.Request) {
	var cfg domain.ApplicationSettings
	_ = h.svc.Config.Get(domain.SystemUserID, keyAppSettings, &cfg)
	writeJSON(w, http.StatusOK, cfg)
}

// PUT /api/admin/app/settings
// Body: {"public_app_url": "https://jobifai.com.au"}
// Empty string is accepted (clears the value). Non-empty values must be absolute
// http/https URLs with no query string or fragment.
func (h *AdminHandlers) AppSettingsSet(w http.ResponseWriter, r *http.Request) {
	var incoming domain.ApplicationSettings
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	normalized, err := NormalizeAndValidateURL(incoming.PublicAppURL)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
		return
	}
	incoming.PublicAppURL = normalized
	if err := h.svc.Config.Set(domain.SystemUserID, keyAppSettings, incoming); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "application settings saved")
}

// BootstrapPublicAppURL seeds the persisted public_app_url from envURL if and
// only if the persisted value is currently empty. Never overwrites an existing
// admin-configured value. Call once from main.go after cfgStore is ready.
func BootstrapPublicAppURL(cfgStore ConfigStore, envURL string) {
	if envURL == "" {
		return
	}
	normalized, err := NormalizeAndValidateURL(envURL)
	if err != nil || normalized == "" {
		log.Warn().Str("url", envURL).Msg("APP_BASE_URL is not a valid http/https URL — skipping bootstrap")
		return
	}
	var existing domain.ApplicationSettings
	_ = cfgStore.Get(domain.SystemUserID, keyAppSettings, &existing)
	if existing.PublicAppURL != "" {
		log.Info().Str("url", existing.PublicAppURL).Msg("public app URL already configured — APP_BASE_URL bootstrap skipped")
		return
	}
	if err := cfgStore.Set(domain.SystemUserID, keyAppSettings, domain.ApplicationSettings{PublicAppURL: normalized}); err != nil {
		log.Error().Err(err).Str("url", normalized).Msg("failed to bootstrap public app URL from APP_BASE_URL")
		return
	}
	log.Info().Str("url", normalized).Msg("bootstrapped public app URL from APP_BASE_URL")
}
