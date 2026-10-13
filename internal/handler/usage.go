package handler

import (
	"net/http"
	"time"

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

// usageSummaryResponse is the customer-facing credit breakdown for the calling user's
// current billing period. USD cost fields are intentionally omitted — customers see
// credits only.
type usageSummaryResponse struct {
	Period                   string  `json:"period"`
	PeriodStart              string  `json:"period_start"`
	PeriodEnd                string  `json:"period_end"`
	TotalCreditsUsed         int64   `json:"total_credits_used"`
	AICredits                int64   `json:"ai_credits"`
	BrowserAutomationCredits int64   `json:"browser_automation_credits"`
	DocumentRenderCredits    int64   `json:"document_render_credits"`
	AICalls                  int64   `json:"ai_calls"`
	BrowserActiveMS          int64   `json:"browser_active_ms"`
	BrowserActiveMinutes     float64 `json:"browser_active_minutes"`
	DocumentRenders          int64   `json:"document_renders"`
}

// usagePeriodForUser derives the billing period boundaries from the user's quota row.
//
// Subscription users use PeriodStart/PeriodEnd.
// Trial users use TrialEndsAt as end, computing start = TrialEndsAt - TrialDays.
// Fallback is now-30d/now (no official period). period_end is always returned.
//
// Limitation: trial_start is computed (TrialEndsAt − TrialDays), not stored. If
// TrialDays defaults change after trial creation the displayed start may be approximate.
func usagePeriodForUser(row domain.UserQuotaRow, defaults domain.QuotaDefaults) (start, end time.Time, periodEndStr string) {
	now := time.Now().UTC()

	if row.PeriodStart != nil {
		start = row.PeriodStart.UTC()
		if row.PeriodEnd != nil {
			end = row.PeriodEnd.UTC()
			periodEndStr = end.Format(time.RFC3339)
		} else {
			end = now
			periodEndStr = end.Format(time.RFC3339)
		}
		return start, end, periodEndStr
	}

	if row.Plan == "trial" && row.TrialEndsAt != nil {
		trialDays := defaults.TrialDays
		if trialDays <= 0 {
			trialDays = 7
		}
		end = row.TrialEndsAt.UTC()
		start = end.AddDate(0, 0, -trialDays)
		periodEndStr = end.Format(time.RFC3339)
		return start, end, periodEndStr
	}

	// Fallback: rolling 30-day window. Always include period_end.
	end = now
	return now.AddDate(0, 0, -30), end, end.Format(time.RFC3339)
}

// GET /api/usage/summary — credit breakdown for the calling user's billing period.
// Available to any authenticated user (not admin-only). Never accepts a user_id param —
// always scoped to the authenticated caller for isolation.
func (h *UsageHandlers) Summary(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	// Derive billing period from quota row. Never let one user see another's data.
	var row domain.UserQuotaRow
	if h.svc.Quota != nil {
		var err error
		row, err = h.svc.Quota.RowForUser(userID)
		if err != nil {
			// Row may not exist yet for a newly registered user — treat as empty trial.
			row = domain.UserQuotaRow{Plan: "trial"}
		}
	}

	var defaults domain.QuotaDefaults
	if h.svc.Quota != nil {
		defaults = h.svc.Quota.LoadDefaults()
	}

	periodStart, periodEnd, periodEndStr := usagePeriodForUser(row, defaults)
	since := periodStart.Format(time.RFC3339)
	until := periodEnd.Format(time.RFC3339)

	resp := usageSummaryResponse{
		Period:      "billing_period",
		PeriodStart: since,
		PeriodEnd:   periodEndStr,
	}

	// AI credits: sum credits_burned (authoritative); count only successful calls.
	// Exclude personal_provider rows (those burn 0 platform credits).
	// Bind periodStart/periodEnd as time.Time — llm_usage_events.created_at is inserted
	// as time.Time by RecordBillingTx, so bounds must match that stored format.
	if err := h.svc.DB.QueryRowContext(r.Context(), `
		SELECT
			COALESCE(SUM(credits_burned), 0),
			COALESCE(SUM(CASE WHEN success=1 THEN 1 ELSE 0 END), 0)
		FROM llm_usage_events
		WHERE user_id=? AND personal_provider=0 AND created_at>=? AND created_at<?`,
		userID, periodStart, periodEnd,
	).Scan(&resp.AICredits, &resp.AICalls); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}

	// Resource credits: sum credits_burned; count events and duration from successful rows only.
	resRows, err := h.svc.DB.QueryContext(r.Context(), `
		SELECT resource_type,
			COALESCE(SUM(CASE WHEN success=1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(credits_burned), 0),
			COALESCE(SUM(CASE WHEN success=1 THEN duration_ms ELSE 0 END), 0)
		FROM resource_usage_events
		WHERE user_id=? AND created_at>=? AND created_at<?
		GROUP BY resource_type`,
		userID, since, until,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = resRows.Close() }()
	for resRows.Next() {
		var rt string
		var successfulEvents, credits, durationMS int64
		if err := resRows.Scan(&rt, &successfulEvents, &credits, &durationMS); err != nil {
			continue
		}
		switch rt {
		case string(domain.ResourceTypeBrowserAutomation):
			resp.BrowserAutomationCredits = credits
			resp.BrowserActiveMS = durationMS
			resp.BrowserActiveMinutes = float64(durationMS) / 60_000
		case string(domain.ResourceTypeDocumentRender):
			resp.DocumentRenderCredits = credits
			resp.DocumentRenders = successfulEvents
		}
	}
	if err := resRows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}

	resp.TotalCreditsUsed = resp.AICredits + resp.BrowserAutomationCredits + resp.DocumentRenderCredits

	writeJSON(w, http.StatusOK, resp)
}
