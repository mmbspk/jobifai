package handler

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type llmUsageEventRow struct {
	ID                  string `json:"id"`
	CreatedAt           string `json:"created_at"`
	UserID              string `json:"user_id"`
	Task                string `json:"task"`
	Provider            string `json:"provider"`
	RequestedModel      string `json:"requested_model"`
	ActualModel         string `json:"actual_model"`
	ActualModelVerified bool   `json:"actual_model_verified"`
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	CacheReadTokens     int    `json:"cache_read_tokens"`
	CacheWrite5mTokens  int    `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens  int    `json:"cache_write_1h_tokens"`
	RawCostUSDMicro     int64  `json:"raw_cost_usd_micro"`
	LoadedCostUSDMicro  int64  `json:"loaded_cost_usd_micro"`
	CreditsBurned       int64  `json:"credits_burned"`
	LatencyMS           int     `json:"latency_ms"`
	Success             bool    `json:"success"`
	ErrorCode           string  `json:"error_code,omitempty"`
	JobID               string  `json:"job_id,omitempty"`
	AutomationRunID     string  `json:"automation_run_id,omitempty"`
}

// GET /api/admin/llm-usage/events
func (h *AdminHandlers) LLMUsageEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period := adminPeriod(r)
	since, allTime := adminSince(period)
	limit, offset := adminLimitOffset(r, 50, 200)

	where, args := llmUsageFilters(r, since, allTime)
	var total int64
	if err := h.svc.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_usage_events WHERE `+where, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}

	listArgs := append(append([]any{}, args...), limit, offset)
	rows, err := h.svc.DB.QueryContext(ctx, `
		SELECT id, created_at, user_id, task, provider, requested_model, actual_model,
			actual_model_verified, input_tokens, output_tokens,
			COALESCE(cache_read_tokens,0), COALESCE(cache_write_5m_tokens,0), COALESCE(cache_write_1h_tokens,0),
			raw_cost_usd_micro, loaded_cost_usd_micro, credits_burned, latency_ms, success,
			COALESCE(error_code,''), COALESCE(job_id,''), COALESCE(automation_run_id,'')
		FROM llm_usage_events WHERE `+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}
	defer func() { _ = rows.Close() }()
	out := scanLLMUsageRows(rows)
	writeJSON(w, http.StatusOK, map[string]any{
		"events": out, "total": total, "limit": limit, "offset": offset, "period": period,
	})
}

// GET /api/admin/llm-usage/summary
func (h *AdminHandlers) LLMUsageSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period := adminPeriod(r)
	since, allTime := adminSince(period)
	where, args := llmUsageFilters(r, since, allTime)

	var calls, success, inTok, outTok, raw, loaded, credits int64
	var avgLat float64
	if err := h.svc.DB.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN success=1 THEN 1 ELSE 0 END),0),
			COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
			COALESCE(SUM(raw_cost_usd_micro),0), COALESCE(SUM(loaded_cost_usd_micro),0),
			COALESCE(SUM(credits_burned),0), COALESCE(AVG(latency_ms),0)
		FROM llm_usage_events WHERE `+where, args...).Scan(
		&calls, &success, &inTok, &outTok, &raw, &loaded, &credits, &avgLat); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "query failed"})
		return
	}

	p90 := latencyP90(ctx, h.svc.DB, where, args)
	groupBy := r.URL.Query().Get("group_by")
	breakdown := adminLLMBreakdown(ctx, h.svc.DB, where, args, groupBy)

	writeJSON(w, http.StatusOK, map[string]any{
		"period": period, "calls": calls, "success_calls": success,
		"error_rate": 1 - pct(success, calls), "input_tokens": inTok, "output_tokens": outTok,
		"raw_cost_usd_micro": raw, "loaded_cost_usd_micro": loaded, "credits_burned": credits,
		"avg_latency_ms": avgLat, "p90_latency_ms": p90, "breakdown": breakdown,
	})
}

func scanLLMUsageRows(rows *sql.Rows) []llmUsageEventRow {
	var out []llmUsageEventRow
	for rows.Next() {
		var row llmUsageEventRow
		var verified, success int
		var created time.Time
		if err := rows.Scan(&row.ID, &created, &row.UserID, &row.Task, &row.Provider,
			&row.RequestedModel, &row.ActualModel, &verified,
			&row.InputTokens, &row.OutputTokens, &row.CacheReadTokens, &row.CacheWrite5mTokens, &row.CacheWrite1hTokens,
			&row.RawCostUSDMicro, &row.LoadedCostUSDMicro, &row.CreditsBurned, &row.LatencyMS, &success,
			&row.ErrorCode, &row.JobID, &row.AutomationRunID); err != nil {
			continue
		}
		row.CreatedAt = created.UTC().Format(time.RFC3339)
		row.ActualModelVerified = verified == 1
		row.Success = success == 1
		out = append(out, row)
	}
	return out
}

func llmUsageFilters(r *http.Request, since time.Time, allTime bool) (string, []any) {
	parts := []string{"1=1"}
	var args []any
	if !allTime {
		parts = append(parts, "created_at >= ?")
		args = append(args, since)
	}
	q := r.URL.Query()
	addEq := func(col, val string) {
		if val != "" {
			parts = append(parts, col+" = ?")
			args = append(args, val)
		}
	}
	addEq("user_id", q.Get("user_id"))
	addEq("task", q.Get("task"))
	addEq("provider", q.Get("provider"))
	addEq("requested_model", q.Get("requested_model"))
	addEq("actual_model", q.Get("actual_model"))
	addEq("error_code", q.Get("error_code"))
	addEq("job_id", q.Get("job_id"))
	addEq("automation_run_id", q.Get("automation_run_id"))
	if v := q.Get("success"); v == "true" || v == "1" {
		parts = append(parts, "success = 1")
	} else if v == "false" || v == "0" {
		parts = append(parts, "success = 0")
	}
	return strings.Join(parts, " AND "), args
}

func latencyP90(ctx context.Context, db *sql.DB, where string, args []any) float64 {
	var n int64
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_usage_events WHERE `+where, args...).Scan(&n)
	if n == 0 {
		return 0
	}
	offset := int64(float64(n-1) * 0.9)
	if offset < 0 {
		offset = 0
	}
	qArgs := append(append([]any{}, args...), offset)
	var lat float64
	err := db.QueryRowContext(ctx, `
		SELECT latency_ms FROM llm_usage_events WHERE `+where+`
		ORDER BY latency_ms LIMIT 1 OFFSET ?`, qArgs...).Scan(&lat)
	if err != nil {
		return 0
	}
	return lat
}

func adminLLMBreakdown(ctx context.Context, db *sql.DB, where string, args []any, groupBy string) []map[string]any {
	var groupCols string
	switch groupBy {
	case "task":
		groupCols = "task"
	case "user":
		groupCols = "user_id"
	case "model":
		groupCols = "requested_model, actual_model"
	case "task_model":
		groupCols = "task, requested_model, actual_model"
	default:
		return nil
	}
	q := fmt.Sprintf(`
		SELECT %s, COUNT(*),
			COALESCE(SUM(raw_cost_usd_micro),0), COALESCE(SUM(loaded_cost_usd_micro),0),
			COALESCE(SUM(credits_burned),0), COALESCE(AVG(latency_ms),0),
			COALESCE(SUM(CASE WHEN success=0 THEN 1 ELSE 0 END),0)
		FROM llm_usage_events WHERE %s GROUP BY %s ORDER BY COUNT(*) DESC LIMIT 50`, groupCols, where, groupCols)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var out []map[string]any
	for rows.Next() {
		switch groupBy {
		case "task":
			var task string
			var calls, raw, loaded, credits, fails int64
			var avgLat float64
			if rows.Scan(&task, &calls, &raw, &loaded, &credits, &avgLat, &fails) == nil {
				out = append(out, map[string]any{
					"task": task, "calls": calls, "raw_cost_usd_micro": raw, "loaded_cost_usd_micro": loaded,
					"credits_burned": credits, "avg_latency_ms": avgLat, "failures": fails,
				})
			}
		case "user":
			var uid string
			var calls, raw, loaded, credits, fails int64
			var avgLat float64
			if rows.Scan(&uid, &calls, &raw, &loaded, &credits, &avgLat, &fails) == nil {
				out = append(out, map[string]any{
					"user_id": uid, "calls": calls, "raw_cost_usd_micro": raw, "loaded_cost_usd_micro": loaded,
					"credits_burned": credits, "avg_latency_ms": avgLat, "failures": fails,
				})
			}
		case "model":
			var req, act string
			var calls, raw, loaded, credits, fails int64
			var avgLat float64
			if rows.Scan(&req, &act, &calls, &raw, &loaded, &credits, &avgLat, &fails) == nil {
				out = append(out, map[string]any{
					"requested_model": req, "actual_model": act, "calls": calls,
					"raw_cost_usd_micro": raw, "loaded_cost_usd_micro": loaded,
					"credits_burned": credits, "avg_latency_ms": avgLat, "failures": fails,
				})
			}
		case "task_model":
			var task, req, act string
			var calls, raw, loaded, credits, fails int64
			var avgLat float64
			if rows.Scan(&task, &req, &act, &calls, &raw, &loaded, &credits, &avgLat, &fails) == nil {
				out = append(out, map[string]any{
					"task": task, "requested_model": req, "actual_model": act, "calls": calls,
					"raw_cost_usd_micro": raw, "loaded_cost_usd_micro": loaded,
					"credits_burned": credits, "avg_latency_ms": avgLat, "failures": fails,
				})
			}
		}
	}
	return out
}
