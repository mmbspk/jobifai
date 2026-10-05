package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/retention"
)

// GET /api/admin/retention/defaults
func (h *AdminHandlers) RetentionDefaultsGet(w http.ResponseWriter, r *http.Request) {
	if h.svc.Retention == nil {
		writeJSON(w, http.StatusOK, domain.DocumentRetentionDefaults{LatestSubmittedApplications: domain.DocumentRetentionDefault}.Normalized())
		return
	}
	d, err := retention.LoadDefaults(h.svc.Config)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, d)
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
		if errors.Is(err, domain.ErrInvalidDocumentRetention) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	if h.svc.Retention != nil {
		h.svc.Retention.ScheduleReconcileAllUsers()
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

// GET /api/admin/documents/artifact-metrics
// Returns PDF-serve, LLM-cache-hit, and reconstruction counters accumulated since the
// last server start, plus the current count and total bytes of retained artifact blobs
// queried from the database.  All fields are zero when the documents service is not wired.
func (h *AdminHandlers) DocumentsArtifactMetrics(w http.ResponseWriter, r *http.Request) {
	snap := documents.DocumentsMetricsSnapshot{}
	if h.svc.Documents != nil && h.svc.Documents.Metrics != nil {
		snap = h.svc.Documents.Metrics.Snapshot()
	}
	if h.svc.LLMReuseMetrics != nil {
		snap.LLMCacheHits = h.svc.LLMReuseMetrics.LLMCacheHitCount()
	}
	if h.svc.DB != nil {
		var count, bytes int64
		_ = h.svc.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*), COALESCE(SUM(byte_size),0) FROM document_render_artifacts
			 WHERE evicted_at IS NULL AND state = 'ready'`,
		).Scan(&count, &bytes)
		snap.RetainedArtifacts = count
		snap.RetainedArtifactBytes = bytes
	}
	writeJSON(w, http.StatusOK, snap)
}
