package handler

import (
	"net/http"
	"strconv"
	"time"
)

func adminPeriod(r *http.Request) string {
	p := r.URL.Query().Get("period")
	switch p {
	case "today", "7d", "30d", "all":
		return p
	}
	if r.URL.Query().Get("days") != "" {
		return "30d"
	}
	return "30d"
}

func adminSince(period string) (since time.Time, allTime bool) {
	now := time.Now().UTC()
	switch period {
	case "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), false
	case "7d":
		return now.AddDate(0, 0, -7), false
	case "all":
		return time.Time{}, true
	default:
		return now.AddDate(0, 0, -30), false
	}
}

func adminLimitOffset(r *http.Request, defLimit, maxLimit int) (limit, offset int) {
	limit = defLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	offset = 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}
