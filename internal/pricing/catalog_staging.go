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
	Source         string    `json:"source"`
	RefreshedAt    time.Time `json:"refreshed_at"`
	ModelsSeen     int       `json:"models_seen"`
	NewModels      []string  `json:"new_models"`
	ChangedPrices  []string  `json:"changed_prices"`
	Deprecated     []string  `json:"deprecated"`
	Errors         []string  `json:"errors,omitempty"`
	UsedLastGood   bool      `json:"used_last_good"`
}

type litellmEntry struct {
	InputCostPerToken  float64 `json:"input_cost_per_token"`
	OutputCostPerToken float64 `json:"output_cost_per_token"`
	MaxInputTokens     int     `json:"max_input_tokens"`
	MaxOutputTokens    int     `json:"max_output_tokens"`
	SupportsVision     bool    `json:"supports_vision"`
}

// StageLiteLLMDiscovery fetches LiteLLM public metadata and upserts model_catalog_candidates.
// It never modifies the approved billing catalog.
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
	if rawMap == nil {
		var snap string
		_ = db.QueryRowContext(ctx, `SELECT COALESCE(discovery_snapshot_json,'') FROM model_catalog_meta WHERE id=1`).Scan(&snap)
		if snap != "" {
			_ = json.Unmarshal([]byte(snap), &rawMap)
			res.UsedLastGood = true
		}
	}
	if len(rawMap) == 0 {
		_ = persistDiscoveryMeta(ctx, db, res, err)
		if err != nil {
			return res, err
		}
		return res, fmt.Errorf("no discovery data available")
	}
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
		id := uuid.NewString()
		diff := catalogDiffJSON(approved, prov, modelKey, ent)
		state := "unchanged"
		if diff != "{}" {
			state = "changed"
		}
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
			boolInt(ent.SupportsVision), string(raw), state, diff,
		)
		if err != nil {
			return res, err
		}
		key := prov + "/" + modelKey
		if state == "changed" {
			if approved != nil {
				if _, e := approved.Resolve(modelKey, false); e != nil {
					res.NewModels = append(res.NewModels, key)
				} else {
					res.ChangedPrices = append(res.ChangedPrices, key)
				}
			} else {
				res.NewModels = append(res.NewModels, key)
			}
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

func catalogDiffJSON(approved *Catalog, provider, model string, ent litellmEntry) string {
	diff := map[string]any{}
	if approved != nil {
		if rec, err := approved.Resolve(model, false); err == nil {
			if rec.Record.InputPerM > 0 && ent.InputCostPerToken > 0 {
				lit := ent.InputCostPerToken * 1_000_000
				if abs(lit-rec.Record.InputPerM) > 0.001 {
					diff["input_price"] = map[string]float64{"approved": rec.Record.InputPerM, "discovered": lit}
				}
			}
		} else {
			diff["approved_catalog"] = "absent"
		}
	}
	diff["provider"] = provider
	b, _ := json.Marshal(diff)
	return string(b)
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
