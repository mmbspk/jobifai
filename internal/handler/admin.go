package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

// AdminHandlers serves deployment-wide defaults and user administration.
type AdminHandlers struct{ svc *Services }

func NewAdminHandlers(svc *Services) *AdminHandlers { return &AdminHandlers{svc: svc} }

// GET /api/admin/system
func (h *AdminHandlers) SystemGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, config.SystemGeneralKV(h.svc.Config))
}

// PUT /api/admin/system
func (h *AdminHandlers) SystemSet(w http.ResponseWriter, r *http.Request) {
	var incoming domain.GeneralSettings
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	stored := config.SystemGeneralKV(h.svc.Config)
	stored.LLM = incoming.LLM
	stored.Browser = incoming.Browser
	stored.HumanBehavior = incoming.HumanBehavior
	if err := h.svc.Config.Set(domain.SystemUserID, config.KeyGeneralSettings, stored); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "system settings saved")
}

// GET /api/admin/system/secrets
func (h *AdminHandlers) SystemSecretsGet(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"has_default_api_key": h.svc.Secrets.Has(domain.SystemUserID, "llm_api_key"),
		"has_proxy_key":       h.svc.Secrets.Has(domain.SystemUserID, "proxy_key"),
	}
	if out["has_default_api_key"].(bool) {
		out["llm_api_key"] = "****"
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/admin/system/secrets/api-key
func (h *AdminHandlers) SystemSetAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		KeyType string `json:"key_type"`
		Value   string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	keyType := req.KeyType
	if keyType == "" {
		keyType = "llm_api_key"
	}
	if keyType != "llm_api_key" && keyType != "proxy_key" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "key_type must be llm_api_key or proxy_key"})
		return
	}
	if req.Value == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "value is required"})
		return
	}
	if err := h.svc.Secrets.Set(domain.SystemUserID, keyType, req.Value); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "system API key saved")
}

// DELETE /api/admin/system/secrets/api-key
func (h *AdminHandlers) SystemDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	keyType := r.URL.Query().Get("key_type")
	if keyType == "" {
		keyType = "llm_api_key"
	}
	if err := h.svc.Secrets.Delete(domain.SystemUserID, keyType); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "system API key deleted")
}

// GET /api/admin/users
func (h *AdminHandlers) UsersList(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.Users.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	rows := make([]domain.AdminUserRow, 0, len(users))
	for _, u := range users {
		rows = append(rows, domain.AdminUserRow{
			ID:          u.ID,
			Email:       u.Email,
			DisplayName: u.DisplayName,
			IsAdmin:     u.IsAdmin,
			CreatedAt:   u.CreatedAt.Format(time.RFC3339),
			HasAPIKey:   config.HasUserLLMAPIKey(h.svc.Secrets, u.ID),
		})
	}
	writeJSON(w, http.StatusOK, rows)
}

// GET /api/admin/users/{user_id}
func (h *AdminHandlers) UserGet(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "user_id")
	u, err := h.svc.Users.ByID(userID)
	if errors.Is(err, auth.ErrUserNotFound) {
		notFound(w, "user not found")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	var overrides domain.LLMOverrides
	_ = h.svc.Config.Get(userID, config.KeyLLMOverrides, &overrides)
	writeJSON(w, http.StatusOK, domain.AdminUserDetail{
		AdminUserRow: domain.AdminUserRow{
			ID:          u.ID,
			Email:       u.Email,
			DisplayName: u.DisplayName,
			IsAdmin:     u.IsAdmin,
			CreatedAt:   u.CreatedAt.Format(time.RFC3339),
			HasAPIKey:   config.HasUserLLMAPIKey(h.svc.Secrets, u.ID),
		},
		LLMOverrides: overrides,
	})
}

// PUT /api/admin/users/{user_id}
func (h *AdminHandlers) UserUpdate(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "user_id")
	if _, err := h.svc.Users.ByID(userID); errors.Is(err, auth.ErrUserNotFound) {
		notFound(w, "user not found")
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	var req struct {
		IsAdmin      *bool                `json:"is_admin"`
		LLMOverrides *domain.LLMOverrides `json:"llm_overrides"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if req.IsAdmin != nil {
		if err := h.svc.Users.SetAdmin(userID, *req.IsAdmin); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
			return
		}
	}
	if req.LLMOverrides != nil {
		if err := h.svc.Config.Set(userID, config.KeyLLMOverrides, *req.LLMOverrides); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
			return
		}
	}
	okMsg(w, "user updated")
}

// POST /api/admin/users/{user_id}/secrets/api-key
func (h *AdminHandlers) UserSetAPIKey(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "user_id")
	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if req.Value == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "value is required"})
		return
	}
	if err := h.svc.Secrets.Set(userID, "llm_api_key", req.Value); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "user API key saved")
}

// DELETE /api/admin/users/{user_id}/secrets/api-key
func (h *AdminHandlers) UserDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "user_id")
	if err := h.svc.Secrets.Delete(userID, "llm_api_key"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "user API key removed")
}
