package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
)

const keyApplyEngineConfig = "apply_engine_config"

// engineConfigReader is the minimal config interface needed to load engine config.
type engineConfigReader interface {
	Get(userID, key string, dst any) error
}

// loadEngineConfig loads the deployment-wide engine configuration. Returns the
// default (Legacy, external ATS off) on any error or if not yet configured.
func loadEngineConfig(store engineConfigReader) domain.ApplyEngineConfig {
	var cfg domain.ApplyEngineConfig
	if err := store.Get(domain.SystemUserID, keyApplyEngineConfig, &cfg); err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			log.Warn().Err(err).Msg("engine: failed to load apply engine config, using legacy default")
		}
		return domain.ApplyEngineConfig{Mode: domain.ApplyEngineLegacy}
	}
	if cfg.Mode == "" {
		cfg.Mode = domain.ApplyEngineLegacy
	}
	return cfg
}

// pinnedEngine reads the current engine config and pins the effective mode for
// the given user and platform. Returns Legacy when store is nil.
func pinnedEngine(store engineConfigReader, userID string, platform domain.Platform) domain.ApplyEngineMode {
	if store == nil {
		return domain.ApplyEngineLegacy
	}
	cfg := loadEngineConfig(store)
	return cfg.EffectiveModeFor(userID, platform)
}

// killSwitchCheck re-reads the engine config and returns an error if the mode
// has been downgraded to Legacy since the attempt started. Call this immediately
// before any irreversible action (submit click) on non-legacy paths.
func killSwitchCheck(store engineConfigReader, ac *AttemptContext) error {
	if ac == nil || ac.Engine == domain.ApplyEngineLegacy || store == nil {
		return nil
	}
	current := pinnedEngine(store, ac.UserID, domain.Platform(ac.Platform))
	if current == domain.ApplyEngineLegacy {
		return errEngineModeReverted
	}
	return nil
}

var errEngineModeReverted = errors.New("engine mode reverted to legacy; aborting non-legacy path")

// WriteEngineAudit persists a change to the engine config in the audit table.
// Non-fatal: logs on error rather than propagating.
// reason is optional context from the caller (e.g. "emergency rollback").
func WriteEngineAudit(db *sql.DB, adminID, reason string, oldCfg, newCfg domain.ApplyEngineConfig) {
	if db == nil {
		return
	}
	raw, err := json.Marshal(newCfg)
	if err != nil {
		log.Warn().Err(err).Msg("engine audit: failed to marshal config")
		raw = []byte("{}")
	}
	id := newAttemptID()
	now := time.Now().UTC().Format(time.RFC3339)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx,
		`INSERT INTO apply_engine_audit(id,admin_id,old_mode,new_mode,reason,config_json,created_at)
		 VALUES(?,?,?,?,?,?,?)`,
		id, adminID, string(oldCfg.Mode), string(newCfg.Mode), reason, string(raw), now,
	)
	if err != nil {
		log.Warn().Err(err).Str("admin_id", adminID).Msg("engine audit: failed to write")
	}
}

// EngineAuditEntry is the read-side shape for audit rows.
type EngineAuditEntry struct {
	ID        string                   `json:"id"`
	AdminID   string                   `json:"admin_id"`
	OldMode   domain.ApplyEngineMode   `json:"old_mode"`
	NewMode   domain.ApplyEngineMode   `json:"new_mode"`
	Reason    string                   `json:"reason,omitempty"`
	Config    domain.ApplyEngineConfig `json:"config"`
	CreatedAt time.Time                `json:"created_at"`
}

// LoadEngineAudit returns the N most recent audit entries, newest first.
func LoadEngineAudit(db *sql.DB, limit int) ([]EngineAuditEntry, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.QueryContext(context.Background(),
		`SELECT id,admin_id,old_mode,new_mode,reason,config_json,created_at
		 FROM apply_engine_audit ORDER BY created_at DESC, rowid DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EngineAuditEntry
	for rows.Next() {
		var e EngineAuditEntry
		var configJSON, createdAt string
		if err := rows.Scan(&e.ID, &e.AdminID, &e.OldMode, &e.NewMode, &e.Reason, &configJSON, &createdAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(configJSON), &e.Config)
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, e)
	}
	return out, rows.Err()
}
