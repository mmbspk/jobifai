package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/domain"
)

const keyApplyEngineConfig = "apply_engine_config"

// AdminApplyEngineHandlers manages the apply engine mode switch (epic #127 / story #129).
type AdminApplyEngineHandlers struct{ svc *Services }

func NewAdminApplyEngineHandlers(svc *Services) *AdminApplyEngineHandlers {
	return &AdminApplyEngineHandlers{svc: svc}
}

// GET /api/admin/apply-engine — read current engine config and effective mode summary.
func (h *AdminApplyEngineHandlers) Get(w http.ResponseWriter, r *http.Request) {
	var cfg domain.ApplyEngineConfig
	if err := h.svc.Config.Get(domain.SystemUserID, keyApplyEngineConfig, &cfg); err != nil {
		cfg = domain.ApplyEngineConfig{Mode: domain.ApplyEngineLegacy}
	}
	writeJSON(w, http.StatusOK, cfg)
}

// PUT /api/admin/apply-engine — replace engine config and write an audit entry.
// Only accepts valid mode values; rejects unknown modes to prevent silent no-ops.
func (h *AdminApplyEngineHandlers) Set(w http.ResponseWriter, r *http.Request) {
	var incoming domain.ApplyEngineConfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request body: " + err.Error()})
		return
	}
	if !validEngineMode(incoming.Mode) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"message": "invalid mode: must be legacy, shadow or adaptive",
		})
		return
	}
	for platform, mode := range incoming.PlatformOverrides {
		if platform != string(domain.PlatformLinkedIn) && platform != string(domain.PlatformSeek) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unsupported platform override"})
			return
		}
		if !validEngineMode(mode) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"message": "invalid platform override mode for " + platform + ": must be legacy, shadow or adaptive",
			})
			return
		}
	}

	configTx, ok := h.svc.Config.(TransactionalConfigStore)
	if !ok || h.svc.DB == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "transactional engine updates are unavailable"})
		return
	}
	tx, err := h.svc.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not begin engine update"})
		return
	}
	defer func() { _ = tx.Rollback() }()
	old := domain.ApplyEngineConfig{Mode: domain.ApplyEngineLegacy}
	if err := configTx.GetTx(r.Context(), tx, domain.SystemUserID, keyApplyEngineConfig, &old); err != nil && !errors.Is(err, domain.ErrNotFound) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not read engine configuration"})
		return
	}
	if err := configTx.SetTx(r.Context(), tx, domain.SystemUserID, keyApplyEngineConfig, incoming); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	adminID := auth.UserIDFromCtx(r.Context())
	var reason string
	if q := r.URL.Query().Get("reason"); q != "" {
		reason = q
	}
	if err := bot.WriteEngineAuditTx(r.Context(), tx, adminID, reason, old, incoming); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not record engine audit"})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not save engine update"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "apply engine config saved",
		"mode":    string(incoming.Mode),
	})
}

// GET /api/admin/apply-engine/audit — recent audit trail, newest first.
func (h *AdminApplyEngineHandlers) Audit(w http.ResponseWriter, r *http.Request) {
	entries, err := bot.LoadEngineAudit(h.svc.DB, 50)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	if entries == nil {
		entries = []bot.EngineAuditEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func validEngineMode(m domain.ApplyEngineMode) bool {
	switch m {
	case domain.ApplyEngineLegacy, domain.ApplyEngineShadow, domain.ApplyEngineAdaptive, "":
		return true
	}
	return false
}
