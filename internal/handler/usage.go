package handler

import (
	"net/http"

	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/quota"
)

// UsageHandlers serves token usage endpoints.
type UsageHandlers struct{ svc *Services }

func NewUsageHandlers(svc *Services) *UsageHandlers { return &UsageHandlers{svc: svc} }

type sessionUsageResponse = domain.SessionUsage

// costForUser returns the model cost for the given user, or false for Ollama/unknown.
// Used only for the session endpoint (in-memory, no per-model breakdown).
func (h *UsageHandlers) costForUser(userID string) (quota.ModelCost, bool) {
	var gs struct {
		LLM struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"llm"`
	}
	if err := h.svc.Config.Get(userID, "general_settings", &gs); err != nil {
		return quota.ModelCost{}, false
	}
	if gs.LLM.Provider == "ollama" {
		return quota.ModelCost{}, false
	}
	c, ok := quota.LookupCost(gs.LLM.Model)
	return c, ok
}

// GET /api/usage/totals — persistent cumulative usage from the database.
func (h *UsageHandlers) Totals(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	rows, err := db.TotalUsageByModel(h.svc.DB, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	var totals domain.SessionUsage
	var totalCost float64
	hasCost := false
	for _, row := range rows {
		totals.InputTokens += row.InputTokens
		totals.OutputTokens += row.OutputTokens
		totals.Calls += row.Calls
		if c, ok := quota.LookupCost(row.Model); ok {
			totalCost += float64(row.InputTokens)/1_000_000*c.InputPerM +
				float64(row.OutputTokens)/1_000_000*c.OutputPerM
			hasCost = true
		}
	}
	if hasCost {
		totals.EstimatedCostUSD = &totalCost
	}

	writeJSON(w, http.StatusOK, totals)
}

// GET /api/usage/session
func (h *UsageHandlers) Session(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	snap := h.svc.UsageStore.Session(userID)

	resp := sessionUsageResponse{
		InputTokens:  snap.InputTokens,
		OutputTokens: snap.OutputTokens,
		Calls:        snap.Calls,
	}

	if c, ok := h.costForUser(userID); ok {
		cost := float64(snap.InputTokens)/1_000_000*c.InputPerM +
			float64(snap.OutputTokens)/1_000_000*c.OutputPerM
		resp.EstimatedCostUSD = &cost
	}

	writeJSON(w, http.StatusOK, resp)
}
