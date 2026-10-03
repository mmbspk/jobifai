package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/retention"
)

// GET /api/admin/retention/defaults
func (h *AdminHandlers) RetentionDefaultsGet(w http.ResponseWriter, r *http.Request) {
	if h.svc.Retention == nil {
		writeJSON(w, http.StatusOK, domain.DocumentRetentionDefaults{LatestSubmittedApplications: domain.DocumentRetentionDefault}.Normalized())
		return
	}
	writeJSON(w, http.StatusOK, retention.LoadDefaults(h.svc.Config))
}

// PUT /api/admin/retention/defaults
func (h *AdminHandlers) RetentionDefaultsSet(w http.ResponseWriter, r *http.Request) {
	if h.svc.Retention == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "retention not enabled"})
		return
	}
	var incoming domain.DocumentRetentionDefaults
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	adminID := auth.UserIDFromCtx(r.Context())
	next, err := retention.SaveDefaultsAudited(r.Context(), h.svc.Config, h.svc.DB, adminID, incoming)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, next)
}

// GET /api/admin/retention/audit
func (h *AdminHandlers) RetentionAuditList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := retention.ListAudit(r.Context(), h.svc.DB, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	if rows == nil {
		rows = []retention.AuditRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

// GET /api/admin/retention/preview/{user_id}
func (h *AdminHandlers) RetentionPreviewUser(w http.ResponseWriter, r *http.Request) {
	if h.svc.Retention == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "retention not enabled"})
		return
	}
	userID := chi.URLParam(r, "user_id")
	candidates, metrics, err := h.svc.Retention.PreviewEviction(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates, "metrics": metrics})
}

// POST /api/admin/retention/run/{user_id}
func (h *AdminHandlers) RetentionRunUser(w http.ResponseWriter, r *http.Request) {
	if h.svc.Retention == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "retention not enabled"})
		return
	}
	userID := chi.URLParam(r, "user_id")
	batch, _ := strconv.Atoi(r.URL.Query().Get("batch"))
	metrics, err := h.svc.Retention.RunEviction(r.Context(), userID, batch)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}
