package pricing

import "strings"

// LookupKind describes how a model price was resolved.
type LookupKind string

const (
	LookupExact    LookupKind = "exact"
	LookupAlias    LookupKind = "alias"
	LookupLegacy   LookupKind = "legacy-prefix"
	LookupFallback LookupKind = "conservative-fallback"
)

// LookupResult is the outcome of catalog resolution.
type LookupResult struct {
	Record       ModelRecord
	Source       string
	Kind         LookupKind
	Known        bool // true when model is in catalog or legacy prefix table
	UsedFallback bool
}

// Resolve returns pricing for billing. Unknown models use conservative fallback (never zero cost).
func (c *Catalog) Resolve(model string, strict bool) (LookupResult, error) {
	rec, src, kind, known := c.lookupInternal(model)
	out := LookupResult{Record: rec, Source: src, Kind: kind, Known: known, UsedFallback: kind == LookupFallback}
	if strict && !known {
		return out, ErrUnpricedModel
	}
	return out, nil
}

// Lookup is backward-compatible: returns record, source, and known flag.
func (c *Catalog) Lookup(model string) (ModelRecord, string, bool) {
	res, _ := c.Resolve(model, false)
	return res.Record, res.Source, res.Known
}

func (c *Catalog) lookupInternal(model string) (ModelRecord, string, LookupKind, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	key := stringsLower(model)
	if key == "" {
		return c.fallbackRecord("unknown"), "conservative-fallback", LookupFallback, false
	}
	if canon, ok := c.alias[key]; ok {
		if r, found := c.models[stringsLower(canon)]; found {
			return r, r.Source, LookupAlias, true
		}
	}
	if r, found := c.models[key]; found {
		return r, r.Source, LookupExact, true
	}
	if inPerM, outPerM, legacyOK := legacyPrefixCost(model); legacyOK {
		return ModelRecord{
			CanonicalID: model, Provider: "legacy-prefix",
			InputPerM: inPerM, OutputPerM: outPerM,
			Active: true, Source: "legacy-prefix-table", EffectiveAt: c.refreshed,
		}, "legacy-prefix-table", LookupLegacy, true
	}
	return c.fallbackRecord(model), "conservative-fallback", LookupFallback, false
}

func stringsLower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
