package handler

import (
	"errors"
	"net/http"

	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/quota"
)

func writeQuotaExceeded(w http.ResponseWriter, err error) {
	var ex *quota.ExceededError
	if errors.As(err, &ex) {
		writeJSON(w, http.StatusPaymentRequired, map[string]string{
			"code":   ex.Code,
			"scope":  ex.Scope,
			"message": "AI usage limit reached",
		})
		return
	}
	writeJSON(w, http.StatusPaymentRequired, map[string]string{
		"code":    "quota_exceeded",
		"message": err.Error(),
	})
}

// QuotaHandlers serves quota status for the current user.
type QuotaHandlers struct{ svc *Services }

func NewQuotaHandlers(svc *Services) *QuotaHandlers { return &QuotaHandlers{svc: svc} }

// GET /api/quota/status
func (h *QuotaHandlers) Status(w http.ResponseWriter, r *http.Request) {
	if h.svc.Quota == nil {
		writeJSON(w, http.StatusOK, map[string]any{"unlimited": true})
		return
	}
	userID := auth.UserIDFromCtx(r.Context())
	st, err := h.svc.Quota.Status(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}
