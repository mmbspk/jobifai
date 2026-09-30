package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/user/jobifai/internal/billing"
)

// GET /api/admin/billing/summary
func (h *AdminHandlers) BillingSummary(w http.ResponseWriter, r *http.Request) {
	if h.svc.DB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "database unavailable"})
		return
	}
	out, err := billing.LoadBillingSummary(r.Context(), h.svc.DB)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/admin/billing/users
func (h *AdminHandlers) BillingUsers(w http.ResponseWriter, r *http.Request) {
	if h.svc.DB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "database unavailable"})
		return
	}
	rows, err := billing.ListUserBilling(r.Context(), h.svc.DB)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// GET /api/admin/billing/webhook-events
func (h *AdminHandlers) BillingWebhookEvents(w http.ResponseWriter, r *http.Request) {
	if h.svc.DB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "database unavailable"})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	f := billing.WebhookEventFilter{
		Status:    r.URL.Query().Get("status"),
		EventType: r.URL.Query().Get("event_type"),
		UserID:    r.URL.Query().Get("user_id"),
		Limit:     limit,
	}
	rows, err := billing.ListWebhookEvents(r.Context(), h.svc.DB, f)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// POST /api/admin/billing/users/{user_id}/reconcile
func (h *AdminHandlers) BillingReconcileUser(w http.ResponseWriter, r *http.Request) {
	if h.svc.Quota == nil || h.svc.DB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "billing not enabled"})
		return
	}
	userID := chi.URLParam(r, "user_id")
	proc := &billing.Processor{DB: h.svc.DB, Quota: h.svc.Quota}
	res, err := proc.ReconcileUser(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}
