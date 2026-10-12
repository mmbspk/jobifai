package handler

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

// GET /api/admin/resource-metering/defaults
// Returns the current resource metering configuration plus computed display values.
func (h *AdminHandlers) ResourceMeteringDefaultsGet(w http.ResponseWriter, r *http.Request) {
	if h.svc.Resource == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "resource service unavailable"})
		return
	}
	def, err := h.svc.Resource.LoadDefaults()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "failed to load resource metering defaults"})
		return
	}
	writeJSON(w, http.StatusOK, buildResourceMeteringResponse(def, h.svc))
}

// PUT /api/admin/resource-metering/defaults
// Validates and saves resource metering configuration.
func (h *AdminHandlers) ResourceMeteringDefaultsSet(w http.ResponseWriter, r *http.Request) {
	if h.svc.Resource == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "resource service unavailable"})
		return
	}
	var incoming domain.ResourceMeteringDefaults
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	// Zero-value BillingIncrementSecs means "not provided" — apply safe default.
	if incoming.Browser.BillingIncrementSecs == 0 {
		incoming.Browser.BillingIncrementSecs = 30
	}
	if err := h.svc.Resource.SaveDefaults(incoming); err != nil {
		if isResourceValidationError(err) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "failed to save resource metering defaults"})
		}
		return
	}
	def, err := h.svc.Resource.LoadDefaults()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "failed to load resource metering defaults after save"})
		return
	}
	writeJSON(w, http.StatusOK, buildResourceMeteringResponse(def, h.svc))
}

// isResourceValidationError returns true when err is a validation error from
// the resource metering package (distinguished by its "resource_metering: " prefix).
func isResourceValidationError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "resource_metering: ")
}

// resourceMeteringResponse is the admin API response for resource metering defaults.
// Includes computed credit equivalents for display in the Admin UI.
type resourceMeteringResponse struct {
	domain.ResourceMeteringDefaults
	// Computed display helpers (read-only, not persisted)
	BrowserCreditsPerMinute float64 `json:"browser_credits_per_minute"`
	RenderCreditsPerRender  int64   `json:"render_credits_per_render"`
	// PerCallFeeIsAIOnly reminds the Admin UI that PerCallFeeUSD does not apply to resource usage.
	PerCallFeeIsAIOnly bool `json:"per_call_fee_is_ai_only"`
}

func buildResourceMeteringResponse(def domain.ResourceMeteringDefaults, svc *Services) resourceMeteringResponse {
	resp := resourceMeteringResponse{
		ResourceMeteringDefaults: def,
		PerCallFeeIsAIOnly:       true,
	}
	if svc.Resource == nil {
		return resp
	}
	resp.RenderCreditsPerRender = svc.Resource.EstimateRenderCredits()
	incrementCredits := svc.Resource.EstimateBrowserIncrementCredits()
	inc := def.Browser.BillingIncrementSecs
	if inc <= 0 {
		inc = 30
	}
	// credits/minute = credits/increment × (60s / incrementSecs)
	if incrementCredits > 0 {
		resp.BrowserCreditsPerMinute = math.Round(float64(incrementCredits)*(60.0/float64(inc))*100) / 100
	}
	return resp
}
