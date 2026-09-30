package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type economicsOverview struct {
	PeriodStart        string  `json:"period_start"`
	RawCostUSDMicro    int64   `json:"raw_cost_usd_micro"`
	LoadedCostUSDMicro int64   `json:"loaded_cost_usd_micro"`
	CreditsBurned      int64   `json:"credits_burned"`
	Calls              int64   `json:"calls"`
	SuccessCalls       int64   `json:"success_calls"`
	AvgRawCostPerCall  float64 `json:"avg_raw_cost_per_call_usd"`
}

type economicsTaskRow struct {
	Task                 string   `json:"task"`
	Calls                int64    `json:"calls"`
	RawCostUSDMicro      int64    `json:"raw_cost_usd_micro"`
	LoadedCostUSDMicro   int64    `json:"loaded_cost_usd_micro"`
	CreditsBurned        int64    `json:"credits_burned"`
	AvgLatencyMS         float64  `json:"avg_latency_ms"`
	ErrorRate            *float64 `json:"error_rate,omitempty"`
	ErrorRateAvailable   bool     `json:"error_rate_available"`
}

// GET /api/admin/economics/overview?period=30d|7d|today|all
func (h *AdminHandlers) EconomicsOverview(w http.ResponseWriter, r *http.Request) {
	period := adminPeriod(r)
	since, allTime := adminSince(period)
	var out economicsOverview
	out.PeriodStart = since.Format(time.RFC3339)
	if allTime {
		out.PeriodStart = "all"
	}
	err := h.svc.DB.QueryRowContext(r.Context(), `
		SELECT
			COALESCE(SUM(raw_cost_usd_micro),0),
			COALESCE(SUM(loaded_cost_usd_micro),0),
			COALESCE(SUM(credits_burned),0),
			COUNT(*),
			COALESCE(SUM(CASE WHEN success=1 THEN 1 ELSE 0 END),0)
		FROM llm_usage_events WHERE `+economicsTimeWhere(allTime), economicsTimeArg(since, allTime)...).Scan(
		&out.RawCostUSDMicro, &out.LoadedCostUSDMicro, &out.CreditsBurned, &out.Calls, &out.SuccessCalls,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	if out.Calls > 0 {
		out.AvgRawCostPerCall = float64(out.RawCostUSDMicro) / float64(out.Calls) / 1_000_000
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/admin/economics/tasks?period=30d
func (h *AdminHandlers) EconomicsTasks(w http.ResponseWriter, r *http.Request) {
	period := adminPeriod(r)
	since, allTime := adminSince(period)
	rows, err := h.svc.DB.QueryContext(r.Context(), `
	 SELECT task, COUNT(*),
	  COALESCE(SUM(raw_cost_usd_micro),0),
	  COALESCE(SUM(loaded_cost_usd_micro),0),
	  COALESCE(SUM(credits_burned),0),
	  COALESCE(AVG(latency_ms),0),
	  COALESCE(SUM(CASE WHEN success=0 THEN 1 ELSE 0 END),0)
	 FROM llm_usage_events WHERE `+economicsTimeWhere(allTime)+`
	 GROUP BY task ORDER BY SUM(raw_cost_usd_micro) DESC`, economicsTimeArg(since, allTime)...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	var failureTracking bool
	_ = h.svc.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM llm_usage_events WHERE success=0 LIMIT 1)`).Scan(&failureTracking)
	var list []economicsTaskRow
	for rows.Next() {
		var row economicsTaskRow
		var fails int64
		if err := rows.Scan(&row.Task, &row.Calls, &row.RawCostUSDMicro, &row.LoadedCostUSDMicro, &row.CreditsBurned, &row.AvgLatencyMS, &fails); err != nil {
			continue
		}
		row.ErrorRateAvailable = failureTracking
		if failureTracking && row.Calls > 0 {
			rate := float64(fails) / float64(row.Calls)
			row.ErrorRate = &rate
		}
		list = append(list, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": list, "failure_tracking_enabled": failureTracking})
}

// GET /api/admin/economics/application/{job_id}
func (h *AdminHandlers) EconomicsApplication(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "job_id")
	if jobID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "job_id required"})
		return
	}
	var raw, loaded, credits int64
	var calls int64
	err := h.svc.DB.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(raw_cost_usd_micro),0),
			COALESCE(SUM(loaded_cost_usd_micro),0),
			COALESCE(SUM(credits_burned),0),
			COUNT(*)
		FROM llm_usage_events WHERE job_id = ?`, jobID).Scan(&raw, &loaded, &credits, &calls)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job_id": jobID, "calls": calls,
		"raw_cost_usd_micro": raw, "loaded_cost_usd_micro": loaded, "credits_burned": credits,
	})
}

func economicsTimeWhere(allTime bool) string {
	if allTime {
		return "1=1"
	}
	return "created_at >= ?"
}

func economicsTimeArg(since time.Time, allTime bool) []any {
	if allTime {
		return nil
	}
	return []any{since}
}

// GET /api/admin/economics/models?period=30d
func (h *AdminHandlers) EconomicsModels(w http.ResponseWriter, r *http.Request) {
	period := adminPeriod(r)
	since, allTime := adminSince(period)
	rows, err := h.svc.DB.QueryContext(r.Context(), `
		SELECT requested_model, actual_model, COUNT(*),
			COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
			COALESCE(SUM(raw_cost_usd_micro),0), COALESCE(SUM(loaded_cost_usd_micro),0),
			COALESCE(AVG(latency_ms),0), COALESCE(SUM(CASE WHEN success=0 THEN 1 ELSE 0 END),0)
		FROM llm_usage_events WHERE `+economicsTimeWhere(allTime)+`
		GROUP BY requested_model, actual_model ORDER BY COUNT(*) DESC`, economicsTimeArg(since, allTime)...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	var list []map[string]any
	for rows.Next() {
		var req, act string
		var calls, inTok, outTok, raw, loaded, fails int64
		var avgLat float64
		if rows.Scan(&req, &act, &calls, &inTok, &outTok, &raw, &loaded, &avgLat, &fails) != nil {
			continue
		}
		avgCost := 0.0
		if calls > 0 {
			avgCost = float64(raw) / float64(calls) / 1_000_000
		}
		list = append(list, map[string]any{
			"requested_model": req, "actual_model": act, "calls": calls,
			"input_tokens": inTok, "output_tokens": outTok,
			"raw_cost_usd_micro": raw, "loaded_cost_usd_micro": loaded,
			"avg_cost_per_call_usd": avgCost, "avg_latency_ms": avgLat, "failures": fails,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": list, "period": period})
}

// GET /api/admin/economics/users?period=30d
func (h *AdminHandlers) EconomicsUsers(w http.ResponseWriter, r *http.Request) {
	period := adminPeriod(r)
	since, allTime := adminSince(period)
	rows, err := h.svc.DB.QueryContext(r.Context(), `
		SELECT user_id, COUNT(*),
			COALESCE(SUM(raw_cost_usd_micro),0), COALESCE(SUM(loaded_cost_usd_micro),0),
			COALESCE(SUM(credits_burned),0)
		FROM llm_usage_events WHERE `+economicsTimeWhere(allTime)+`
		GROUP BY user_id ORDER BY SUM(credits_burned) DESC LIMIT 100`, economicsTimeArg(since, allTime)...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	var list []map[string]any
	for rows.Next() {
		var uid string
		var calls, raw, loaded, credits int64
		if rows.Scan(&uid, &calls, &raw, &loaded, &credits) != nil {
			continue
		}
		list = append(list, map[string]any{
			"user_id": uid, "calls": calls, "raw_cost_usd_micro": raw,
			"loaded_cost_usd_micro": loaded, "credits_burned": credits,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": list, "period": period})
}

