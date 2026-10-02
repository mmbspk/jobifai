package documents

import "strings"

// NormalizeStyleName canonicalizes a style choice; empty string means Default (market CSS).
func NormalizeStyleName(style string) string {
	return strings.TrimSpace(style)
}

func styleNamesEqual(a, b string) bool {
	return strings.EqualFold(NormalizeStyleName(a), NormalizeStyleName(b))
}

// CoalesceDefaultsMeta maps legacy single Style to per-kind fields after load.
func CoalesceDefaultsMeta(m DefaultsMeta) DefaultsMeta {
	if m.Style != "" {
		if m.ResumeStyle == "" {
			m.ResumeStyle = NormalizeStyleName(m.Style)
		}
		if m.CoverStyle == "" {
			m.CoverStyle = NormalizeStyleName(m.Style)
		}
	}
	m.ResumeStyle = NormalizeStyleName(m.ResumeStyle)
	m.CoverStyle = NormalizeStyleName(m.CoverStyle)
	m.Style = ""
	return m
}
