package pricing

import (
	"fmt"
	"strings"
	"sync"
	"time"

)

// ModelRecord is a priced model entry snapshotted on each LLM event.
type ModelRecord struct {
	Provider           string
	CanonicalID        string
	Aliases            []string
	InputPerM          float64
	OutputPerM         float64
	CacheWritePerM     float64
	CacheWrite5mPerM   float64
	CacheWrite1hPerM   float64
	CacheReadPerM      float64
	BatchInputPerM     float64
	BatchOutputPerM    float64
	ContextLimit       int
	SupportsVision     bool
	SupportsStructured bool
	Active             bool
	Deprecated         bool
	Source             string
	EffectiveAt        time.Time
}

// Catalog resolves models to prices. Unknown models must not return zero cost silently.
type Catalog struct {
	mu       sync.RWMutex
	models   map[string]ModelRecord // canonical id -> record
	alias    map[string]string
	version  string
	source   string
	refreshed time.Time
	// Conservative fallback when model unknown (USD per 1M tokens).
	fallbackInputPerM  float64
	fallbackOutputPerM float64
}

// DefaultCatalog returns built-in pricing verified against Anthropic list prices (2026-09-29).
func DefaultCatalog() *Catalog {
	c := &Catalog{
		models:             make(map[string]ModelRecord),
		alias:              make(map[string]string),
		version:            "2026-09-29",
		source:             "builtin",
		refreshed:          time.Now().UTC(),
		fallbackInputPerM:  15.0,
		fallbackOutputPerM: 75.0,
	}
	seed := []ModelRecord{
		{
			Provider: "claude", CanonicalID: "claude-sonnet-4-6",
			Aliases: []string{"claude-sonnet-4.6"},
			InputPerM: 3, OutputPerM: 15, CacheWritePerM: 3.75, CacheReadPerM: 0.30,
			ContextLimit: 1_000_000, SupportsVision: true, SupportsStructured: true, Active: true, Source: "anthropic-list",
			CacheWrite5mPerM: 3.75, CacheWrite1hPerM: 6.0,
		},
		{
			Provider: "claude", CanonicalID: "claude-sonnet-4-5",
			InputPerM: 3, OutputPerM: 15, CacheWritePerM: 3.75, CacheReadPerM: 0.30,
			ContextLimit: 200_000, SupportsVision: true, SupportsStructured: true, Active: true, Source: "anthropic-list",
			CacheWrite5mPerM: 3.75, CacheWrite1hPerM: 6.0,
		},
		{
			Provider: "claude", CanonicalID: "claude-haiku-4-5",
			Aliases: []string{"claude-haiku-4.5"},
			InputPerM: 1, OutputPerM: 5, CacheWritePerM: 1.25, CacheReadPerM: 0.10,
			ContextLimit: 200_000, SupportsVision: true, SupportsStructured: true, Active: true, Source: "anthropic-list",
			CacheWrite5mPerM: 1.25, CacheWrite1hPerM: 2.0,
		},
		{
			Provider: "claude", CanonicalID: "claude-sonnet-5-5",
			Aliases: []string{"claude-sonnet-5.5"},
			InputPerM: 2, OutputPerM: 10, CacheWritePerM: 2.5, CacheReadPerM: 0.20,
			ContextLimit: 1_000_000, SupportsVision: true, SupportsStructured: true, Active: true, Source: "anthropic-list",
			CacheWrite5mPerM: 2.5, CacheWrite1hPerM: 4.0,
		},
		{
			Provider: "openai", CanonicalID: "gpt-4o",
			InputPerM: 2.5, OutputPerM: 10, ContextLimit: 128_000, Active: true, Source: "openai-list",
		},
	}
	for _, m := range seed {
		m.EffectiveAt = c.refreshed
		c.models[strings.ToLower(m.CanonicalID)] = m
		c.alias[strings.ToLower(m.CanonicalID)] = m.CanonicalID
		for _, a := range m.Aliases {
			c.alias[strings.ToLower(a)] = m.CanonicalID
		}
	}
	return c
}

func (c *Catalog) Meta() (source, version string, refreshed time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.source, c.version, c.refreshed
}

func (c *Catalog) fallbackRecord(model string) ModelRecord {
	return ModelRecord{
		CanonicalID: model,
		Provider:    "unknown",
		InputPerM:   c.fallbackInputPerM,
		OutputPerM:  c.fallbackOutputPerM,
		Active:      true,
		Source:      "conservative-fallback",
		EffectiveAt: c.refreshed,
	}
}

// TokenUsage for cost computation.
type TokenUsage struct {
	InputTokens       int64
	OutputTokens      int64
	CacheWrite5mTokens int64
	CacheWrite1hTokens int64
	CacheReadTokens   int64
}

// RawCostMicroUSD computes provider cost from snapshotted catalog entry.
func RawCostMicroUSD(rec ModelRecord, u TokenUsage) int64 {
	inUSD := float64(u.InputTokens) / 1_000_000 * rec.InputPerM
	outUSD := float64(u.OutputTokens) / 1_000_000 * rec.OutputPerM
	cw5 := rec.CacheWrite5mPerM
	if cw5 <= 0 {
		cw5 = rec.CacheWritePerM
	}
	cw1 := rec.CacheWrite1hPerM
	if cw1 <= 0 {
		cw1 = rec.CacheWritePerM * 1.6
	}
	cwUSD := float64(u.CacheWrite5mTokens)/1_000_000*cw5 + float64(u.CacheWrite1hTokens)/1_000_000*cw1
	crUSD := float64(u.CacheReadTokens) / 1_000_000 * rec.CacheReadPerM
	total := inUSD + outUSD + cwUSD + crUSD
	if total <= 0 && u.InputTokens+u.OutputTokens+u.CacheReadTokens+u.CacheWrite5mTokens+u.CacheWrite1hTokens == 0 {
		return 0
	}
	if total <= 0 {
		total = 0.000001
	}
	return int64(total * 1_000_000)
}

// ErrUnpricedModel is returned when billing policy rejects unknown models (optional strict mode).
var ErrUnpricedModel = fmt.Errorf("model not in catalog and strict pricing enabled")
