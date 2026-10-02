package ws

import (
	"strings"
)

// VisibleOnDashboard decides whether a structured log line is shown on the home
// dashboard WebSocket in production when the viewer does not have verbose logs enabled.
func VisibleOnDashboard(entry map[string]any) bool {
	if entry == nil {
		return true
	}
	level := strings.ToLower(stringField(entry, "level"))
	if level == "debug" {
		return false
	}
	if boolField(entry, "llm_call") {
		return false
	}
	ev := stringField(entry, "event")
	if ev == "llm_call" || strings.HasPrefix(ev, "llm_") {
		return false
	}
	msg := stringField(entry, "message")
	if msg == "" {
		msg = stringField(entry, "msg")
	}
	if strings.HasPrefix(strings.ToLower(msg), "llm:") || strings.Contains(msg, "llm call completed") {
		return false
	}
	if level == "error" || level == "warn" || level == "warning" {
		return true
	}
	if level != "info" && level != "" {
		return customerMessage(msg)
	}
	return customerMessage(msg)
}

func customerMessage(msg string) bool {
	if msg == "" {
		return false
	}
	lower := strings.ToLower(msg)
	// Automation progress (platform runners + approve flow).
	for _, prefix := range []string{
		"seek:", "linkedin:", "approve:", "automation", "bot:",
		"test_automation:", "ai apply:",
	} {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	// High-signal phrases without a stable prefix.
	for _, needle := range []string{
		"submitted", "applied", "daily limit", "daily progress",
		"stopped", "starting", "processing", "skip (", "skip,",
		"score ", "queued for review", "top matches",
		"session expired", "re-login", "not logged in",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func boolField(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}
