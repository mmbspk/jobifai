package handler

import (
	"net/http"

	"github.com/user/jobifai/internal/domain"
)

// ─── response types ───────────────────────────────────────────────────────────

type resourceTypeSummary struct {
	Events             int64   `json:"events"`
	DurationMS         int64   `json:"duration_ms,omitempty"`
	DurationMinutes    float64 `json:"duration_minutes,omitempty"`
	CreditsBurned      int64   `json:"credits_burned"`
	RawCostUSDMicro    int64   `json:"raw_cost_usd_micro"`
	LoadedCostUSDMicro int64   `json:"loaded_cost_usd_micro"`
}

type resourceUsageSummaryResponse struct {
	Period                  string               `json:"period"`
	PeriodStart             string               `json:"period_start"`
	BrowserAutomation       *resourceTypeSummary `json:"browser_automation,omitempty"`
	DocumentRender          *resourceTypeSummary `json:"document_render,omitempty"`
	TotalCreditsBurned      int64                `json:"total_credits_burned"`
	TotalRawCostUSDMicro    int64                `json:"total_raw_cost_usd_micro"`
	TotalLoadedCostUSDMicro int64                `json:"total_loaded_cost_usd_micro"`
}

type resourceUsageEventRow struct {
	ID                 string  `json:"id"`
	CreatedAt          string  `json:"created_at"`
	UserID             string  `json:"user_id"`
	ResourceType       string  `json:"resource_type"`
	Quantity           float64 `json:"quantity"`
	Unit               string  `json:"unit"`
	DurationMS         int64   `json:"duration_ms"`
	CreditsBurned      int64   `json:"credits_burned"`
	RawCostUSDMicro    int64   `json:"raw_cost_usd_micro"`
	LoadedCostUSDMicro int64   `json:"loaded_cost_usd_micro"`
	Success            bool    `json:"success"`
	ErrorCode          string  `json:"error_code"`
	AutomationRunID    string  `json:"automation_run_id"`
	ApplicationID      string  `json:"application_id"`
	JobID              string  `json:"job_id"`
}

// GET /api/admin/resource-usage/summary?period=30d|7d|today|all
func (h *AdminHandlers) ResourceUsageSummary(w http.ResponseWriter, r *http.Request) {
	period := adminPeriod(r)
	since, allTime := adminSince(period)

	timeWhere := economicsTimeWhere(allTime)
	timeArgs := resourceTimeArg(since, allTime)

	rows, err := h.svc.DB.QueryContext(r.Context(), `
		SELECT resource_type,
			COUNT(*) AS events,
			COALESCE(SUM(credits_burned), 0),
			COALESCE(SUM(duration_ms), 0),
			COALESCE(SUM(raw_cost_usd_micro), 0),
			COALESCE(SUM(loaded_cost_usd_micro), 0)
		FROM resource_usage_events
		WHERE `+timeWhere+`
		GROUP BY resource_type`,
		timeArgs...,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()

	out := resourceUsageSummaryResponse{Period: period}
	out.PeriodStart = since.Format("2006-01-02T15:04:05Z")
	if allTime {
		out.PeriodStart = "all"
	}

	for rows.Next() {
		var rt string
		var events, credits, durationMS, raw, loaded int64
		if err := rows.Scan(&rt, &events, &credits, &durationMS, &raw, &loaded); err != nil {
			continue
		}
		s := &resourceTypeSummary{
			Events:             events,
			CreditsBurned:      credits,
			RawCostUSDMicro:    raw,
			LoadedCostUSDMicro: loaded,
		}
		switch rt {
		case string(domain.ResourceTypeBrowserAutomation):
			s.DurationMS = durationMS
			s.DurationMinutes = float64(durationMS) / 60_000
			out.BrowserAutomation = s
		case string(domain.ResourceTypeDocumentRender):
			out.DocumentRender = s
		}
		out.TotalCreditsBurned += credits
		out.TotalRawCostUSDMicro += raw
		out.TotalLoadedCostUSDMicro += loaded
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}

	writeJSON(w, http.StatusOK, out)
}

// GET /api/admin/resource-usage/events?period=30d&user_id=&resource_type=&limit=50&offset=0
func (h *AdminHandlers) ResourceUsageEvents(w http.ResponseWriter, r *http.Request) {
	period := adminPeriod(r)
	since, allTime := adminSince(period)
	limit, offset := adminLimitOffset(r, 50, 500)

	userFilter := r.URL.Query().Get("user_id")
	typeFilter := r.URL.Query().Get("resource_type")

	timeWhere := economicsTimeWhere(allTime)
	timeArgs := resourceTimeArg(since, allTime)

	baseWhere := timeWhere + ` AND (?='' OR user_id=?) AND (?='' OR resource_type=?)`
	baseArgs := append(timeArgs, userFilter, userFilter, typeFilter, typeFilter)

	var total int64
	if err := h.svc.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM resource_usage_events WHERE `+baseWhere,
		baseArgs...,
	).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}

	rows, err := h.svc.DB.QueryContext(r.Context(), `
		SELECT id, created_at, user_id, resource_type, quantity, unit, duration_ms,
			credits_burned, raw_cost_usd_micro, loaded_cost_usd_micro,
			success, COALESCE(error_code,''),
			COALESCE(automation_run_id,''), COALESCE(application_id,''), COALESCE(job_id,'')
		FROM resource_usage_events
		WHERE `+baseWhere+`
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?`,
		append(baseArgs, limit, offset)...,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()

	events := make([]resourceUsageEventRow, 0)
	for rows.Next() {
		var ev resourceUsageEventRow
		var createdAt string
		if err := rows.Scan(
			&ev.ID, &createdAt, &ev.UserID, &ev.ResourceType,
			&ev.Quantity, &ev.Unit, &ev.DurationMS,
			&ev.CreditsBurned, &ev.RawCostUSDMicro, &ev.LoadedCostUSDMicro,
			&ev.Success, &ev.ErrorCode,
			&ev.AutomationRunID, &ev.ApplicationID, &ev.JobID,
		); err != nil {
			continue
		}
		if t, err := parseTime(createdAt); err == nil {
			ev.CreatedAt = t.UTC().Format("2006-01-02T15:04:05Z")
		} else {
			ev.CreatedAt = createdAt
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events, "total": total,
		"limit": limit, "offset": offset, "period": period,
	})
}
