package pricing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DiscoveryRefreshResult is returned to admin after staging LiteLLM metadata.
type DiscoveryRefreshResult struct {
	Source        string    `json:"source"`
	RefreshedAt   time.Time `json:"refreshed_at"`
	ModelsSeen    int       `json:"models_seen"`
	NewModels     []string  `json:"new_models"`
	ChangedPrices []string  `json:"changed_prices"`
	Deprecated    []string  `json:"deprecated"`
	Errors        []string  `json:"errors,omitempty"`
	UsedLastGood  bool      `json:"used_last_good"`
}

type litellmEntry struct {
	InputCostPerToken  float64 `json:"input_cost_per_token"`
	OutputCostPerToken float64 `json:"output_cost_per_token"`
	MaxInputTokens     int     `json:"max_input_tokens"`
	MaxOutputTokens    int     `json:"max_output_tokens"`
	SupportsVision     bool    `json:"supports_vision"`
}

type discoveryDiff struct {
	NewModel           bool             `json:"new_model,omitempty"`
	InputPrice         *priceChange     `json:"input_price,omitempty"`
	OutputPrice        *priceChange     `json:"output_price,omitempty"`
	ContextWindow      *intChange       `json:"context_window,omitempty"`
	VisionSupport      *boolChange      `json:"vision_support,omitempty"`
	Deprecated         bool             `json:"deprecated,omitempty"`
}

type priceChange struct {
	Approved  float64 `json:"approved_per_m"`
	Discovered float64 `json:"discovered_per_m"`
}

type intChange struct {
	Approved   int `json:"approved"`
	Discovered int `json:"discovered"`
}

type boolChange struct {
	Approved   bool `json:"approved"`
	Discovered bool `json:"discovered"`
}

// StageLiteLLMDiscovery fetches LiteLLM public metadata and upserts model_catalog_candidates.
func StageLiteLLMDiscovery(ctx context.Context, db *sql.DB, src *LiteLLMSource, approved *Catalog) (DiscoveryRefreshResult, error) {
	res := DiscoveryRefreshResult{Source: src.Name(), RefreshedAt: time.Now().UTC()}
	if src == nil {
		src = &LiteLLMSource{}
	}
	n, err := src.Refresh()
	res.ModelsSeen = n
	if err != nil {
		res.Errors = append(res.Errors, err.Error())
		res.UsedLastGood = src.LastGood != nil
	}
	rawMap := src.LastGood
	var prevSnap map[string]json.RawMessage
	var prevSnapStr string
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(discovery_snapshot_json,'') FROM model_catalog_meta WHERE id=1`).Scan(&prevSnapStr)
	if prevSnapStr != "" {
		_ = json.Unmarshal([]byte(prevSnapStr), &prevSnap)
	}
	if rawMap == nil && prevSnapStr != "" {
		rawMap = prevSnap
		res.UsedLastGood = true
	}
	if len(rawMap) == 0 {
		_ = persistDiscoveryMeta(ctx, db, res, err)
		if err != nil {
			return res, err
		}
		return res, fmt.Errorf("no discovery data available")
	}
	seenNow := map[string]bool{}
	snapBytes, _ := json.Marshal(rawMap)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback() }()

	for modelKey, raw := range rawMap {
		prov := ProviderGuess(modelKey)
		if prov == "" {
			continue
		}
		var ent litellmEntry
		_ = json.Unmarshal(raw, &ent)
		key := prov + "/" + modelKey
		seenNow[key] = true
		diff, state, kind := classifyDiscovery(approved, prov, modelKey, ent, prevSnap, rawMap)
		diffJSON, _ := json.Marshal(diff)
		switch kind {
		case "new":
			res.NewModels = append(res.NewModels, key)
		case "price", "capability":
			res.ChangedPrices = append(res.ChangedPrices, key)
		}
		id := uuid.NewString()
		_, err := tx.ExecContext(ctx, `
			INSERT INTO model_catalog_candidates (
				id, provider, model, canonical_model, source, input_price, output_price,
				context_window, max_output, vision_support, raw_metadata_json, discovery_state, catalog_diff_json, last_seen_at
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)
			ON CONFLICT(provider, model, source) DO UPDATE SET
				input_price=excluded.input_price, output_price=excluded.output_price,
				context_window=excluded.context_window, max_output=excluded.max_output,
				vision_support=excluded.vision_support, raw_metadata_json=excluded.raw_metadata_json,
				discovery_state=excluded.discovery_state, catalog_diff_json=excluded.catalog_diff_json,
				last_seen_at=CURRENT_TIMESTAMP`,
			id, prov, modelKey, modelKey, src.Name(),
			nullFloat(ent.InputCostPerToken), nullFloat(ent.OutputCostPerToken),
			nullInt(ent.MaxInputTokens), nullInt(ent.MaxOutputTokens),
			boolInt(ent.SupportsVision), string(raw), state, string(diffJSON),
		)
		if err != nil {
			return res, err
		}
	}
	for k := range prevSnap {
		prov := ProviderGuess(k)
		if prov == "" {
			continue
		}
		key := prov + "/" + k
		if !seenNow[key] {
			res.Deprecated = append(res.Deprecated, key)
		}
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE model_catalog_meta SET source=?, last_refresh_at=?, last_error=?, discovery_snapshot_json=?, updated_at=CURRENT_TIMESTAMP WHERE id=1`,
		src.Name(), res.RefreshedAt, strings.Join(res.Errors, "; "), string(snapBytes))
	if err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	_ = persistDiscoveryMeta(ctx, db, res, nil)
	return res, nil
}

func classifyDiscovery(approved *Catalog, provider, model string, ent litellmEntry, prevSnap, curSnap map[string]json.RawMessage) (discoveryDiff, string, string) {
	var d discoveryDiff
	kind := "unchanged"
	state := "unchanged"
	if approved != nil {
		res, err := approved.Resolve(model, false)
		if err != nil || !res.Known || res.UsedFallback {
			d.NewModel = true
			state = "new"
			kind = "new"
			return d, state, kind
		}
		if ent.InputCostPerToken > 0 {
			lit := ent.InputCostPerToken * 1_000_000
			if abs(lit-res.Record.InputPerM) > 0.001 {
				d.InputPrice = &priceChange{Approved: res.Record.InputPerM, Discovered: lit}
				state = "changed"
				kind = "price"
			}
		}
		if ent.OutputCostPerToken > 0 {
			lit := ent.OutputCostPerToken * 1_000_000
			if abs(lit-res.Record.OutputPerM) > 0.001 {
				d.OutputPrice = &priceChange{Approved: res.Record.OutputPerM, Discovered: lit}
				state = "changed"
				kind = "price"
			}
		}
		if ent.MaxInputTokens > 0 && res.Record.ContextLimit > 0 && ent.MaxInputTokens != res.Record.ContextLimit {
			d.ContextWindow = &intChange{Approved: res.Record.ContextLimit, Discovered: ent.MaxInputTokens}
			state = "changed"
			kind = "capability"
		}
		if ent.SupportsVision != res.Record.SupportsVision {
			d.VisionSupport = &boolChange{Approved: res.Record.SupportsVision, Discovered: ent.SupportsVision}
			state = "changed"
			kind = "capability"
		}
	}
	if prevSnap != nil {
		if _, was := prevSnap[model]; !was && !d.NewModel {
			d.NewModel = true
			state = "new"
			kind = "new"
		}
	}
	if d.NewModel || d.InputPrice != nil || d.OutputPrice != nil || d.ContextWindow != nil || d.VisionSupport != nil {
		return d, state, kind
	}
	return discoveryDiff{}, "unchanged", kind
}

func persistDiscoveryMeta(ctx context.Context, db *sql.DB, res DiscoveryRefreshResult, fetchErr error) error {
	msg := strings.Join(res.Errors, "; ")
	if fetchErr != nil && msg == "" {
		msg = fetchErr.Error()
	}
	_, err := db.ExecContext(ctx, `
		UPDATE model_catalog_meta SET last_refresh_at=?, last_error=? WHERE id=1`,
		res.RefreshedAt, msg)
	return err
}

func nullFloat(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
