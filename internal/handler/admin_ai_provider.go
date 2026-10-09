package handler

import (
	"net/http"

	"github.com/user/jobifai/internal/domain"
)

// AdminAIProviderHandlers handles admin-only AI provider connection test.
type AdminAIProviderHandlers struct{ svc *Services }

func NewAdminAIProviderHandlers(svc *Services) *AdminAIProviderHandlers {
	return &AdminAIProviderHandlers{svc: svc}
}

// POST /api/admin/ai-provider/test
func (h *AdminAIProviderHandlers) Test(w http.ResponseWriter, r *http.Request) {
	if h.svc.LLMConnectionTester == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "connection testing not available"})
		return
	}
	provider, model, latencyMS, err := h.svc.LLMConnectionTester(r.Context(), domain.SystemUserID)
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
