package handler

import (
	"net/http"
	"strings"

	"github.com/user/jobifai/internal/auth"
	"github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

// modelCost holds per-million-token prices for a model.
type modelCost struct {
	InputPerM  float64
	OutputPerM float64
}

// costTable maps model name prefixes (lowercase) to pricing.
// Prices are in USD per million tokens as of April 2026.
var costTable = []struct {
	prefix string
	cost   modelCost
}{
	{"claude-opus", modelCost{15.00, 75.00}},
	{"claude-sonnet", modelCost{3.00, 15.00}},
	{"claude-haiku", modelCost{0.25, 1.25}},
	{"gpt-4o", modelCost{2.50, 10.00}},
	{"gpt-4", modelCost{30.00, 60.00}},
	{"gpt-3.5", modelCost{0.50, 1.50}},
}

func lookupCost(model string) (modelCost, bool) {
	lower := strings.ToLower(model)
	for _, entry := range costTable {
		if strings.HasPrefix(lower, entry.prefix) {
			return entry.cost, true
		}
	}
	return modelCost{}, false
}

// UsageHandlers serves token usage endpoints.
type UsageHandlers struct{ svc *Services }

func NewUsageHandlers(svc *Services) *UsageHandlers { return &UsageHandlers{svc: svc} }

type sessionUsageResponse = domain.SessionUsage

// costForUser returns the model cost for the given user, or false for Ollama/unknown.
// Used only for the session endpoint (in-memory, no per-model breakdown).
func (h *UsageHandlers) costForUser(userID string) (modelCost, bool) {
	var gs struct {
		LLM struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"llm"`
	}
	if err := h.svc.Config.Get(userID, "general_settings", &gs); err != nil {
		return modelCost{}, false
	}
	if gs.LLM.Provider == "ollama" {
		return modelCost{}, false
	}
	return lookupCost(gs.LLM.Model)
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
		if c, ok := lookupCost(row.Model); ok {
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
