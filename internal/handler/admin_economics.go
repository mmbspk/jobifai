package handler

import (
	"net/http"
	"strconv"
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
	Task            string  `json:"task"`
	Calls           int64   `json:"calls"`
	RawCostUSDMicro int64   `json:"raw_cost_usd_micro"`
	CreditsBurned   int64   `json:"credits_burned"`
	AvgLatencyMS    float64 `json:"avg_latency_ms"`
	ErrorRate       float64 `json:"error_rate"`
}

// GET /api/admin/economics/overview?days=30
func (h *AdminHandlers) EconomicsOverview(w http.ResponseWriter, r *http.Request) {
	days := queryDays(r, 30)
	since := time.Now().UTC().AddDate(0, 0, -days)
	var out economicsOverview
	out.PeriodStart = since.Format(time.RFC3339)
	err := h.svc.DB.QueryRowContext(r.Context(), `
		SELECT
			COALESCE(SUM(raw_cost_usd_micro),0),
			COALESCE(SUM(loaded_cost_usd_micro),0),
			COALESCE(SUM(credits_burned),0),
			COUNT(*),
			COALESCE(SUM(CASE WHEN success=1 THEN 1 ELSE 0 END),0)
		FROM llm_usage_events WHERE created_at >= ?`, since).Scan(
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

// GET /api/admin/economics/tasks?days=30
func (h *AdminHandlers) EconomicsTasks(w http.ResponseWriter, r *http.Request) {
	days := queryDays(r, 30)
	since := time.Now().UTC().AddDate(0, 0, -days)
	rows, err := h.svc.DB.QueryContext(r.Context(), `
	 SELECT task, COUNT(*),
	  COALESCE(SUM(raw_cost_usd_micro),0),
	  COALESCE(SUM(credits_burned),0),
	  COALESCE(AVG(latency_ms),0),
	  COALESCE(SUM(CASE WHEN success=0 THEN 1 ELSE 0 END),0)
	 FROM llm_usage_events WHERE created_at >= ?
	 GROUP BY task ORDER BY SUM(raw_cost_usd_micro) DESC`, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	var list []economicsTaskRow
	for rows.Next() {
		var row economicsTaskRow
		var fails int64
		if err := rows.Scan(&row.Task, &row.Calls, &row.RawCostUSDMicro, &row.CreditsBurned, &row.AvgLatencyMS, &fails); err != nil {
			continue
		}
		if row.Calls > 0 {
			row.ErrorRate = float64(fails) / float64(row.Calls)
		}
		list = append(list, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": list})
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

func queryDays(r *http.Request, def int) int {
	v := r.URL.Query().Get("days")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}
