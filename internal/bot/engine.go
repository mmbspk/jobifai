package bot

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/rs/zerolog/log"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

const keyApplyEngineConfig = "apply_engine_config"

// configureApplyEngine is shared by batch workers and single-job submissions.
// Keep mapping lookup available even when the model is disabled.
func (m *Manager) configureApplyEngine(cfg *Config, client *llm.Client) {
	cfg.EngineStore = m.cfgStore
	cfg.EngineMode = pinnedEngine(m.cfgStore, cfg.UserID, cfg.Platform)
	if m.cfgStore != nil {
		cfg.EnableExternalATS = loadEngineConfig(m.cfgStore).ExternalATSEnabled
	}
	cfg.AdaptiveMapping = &DBMappingStore{DB: m.db, UserID: cfg.UserID}
	if client != nil {
		adapter := NewLLMFormInterpreter(client)
		adapter.learning = m.formLearning
		settings, _ := json.Marshal(cfg.Settings.LLM)
		settingsHash := sha256.Sum256(settings)
		adapter.scope = fmt.Sprintf("%s\x00%s\x00%s\x00%x", cfg.UserID, client.ProviderName(), client.ModelName(), settingsHash)
		cfg.AdaptiveLLM = adapter
	}
}

// applyAttempt is the only engine dispatch used by batch, queue and direct apply.
// The pinned attempt selects the executor; a rollback never switches executors.
func (b *Bot) applyAttempt(ctx context.Context, page *rod.Page, lazy *lazyDocGen, ac *AttemptContext) error {
	platform := b.cfg.Platform
	if ac != nil && ac.Platform != "" {
		if platform != "" && platform != domain.Platform(ac.Platform) {
			return fmt.Errorf("engine: attempt platform does not match worker")
		}
		platform = domain.Platform(ac.Platform)
	}
	if err := killSwitchCheck(b.cfg.EngineStore, ac); err != nil {
		return err
	}
	if ac != nil {
		ctx = llm.AttachAttribution(ctx, domain.LLMCallContext{
			UserID: ac.UserID, JobID: ac.JobID, ApplyAttemptID: ac.AttemptID,
			Platform: ac.Platform, Engine: ac.Engine, AutomationRunID: b.cfg.AutomationRunID,
		})
		if lazy != nil {
			lazy.ctx = ctx
		}
		if ac.Engine == domain.ApplyEngineAdaptive {
			if platform == domain.PlatformLinkedIn && b.cfg.EnableExternalATS && lazy != nil && page != nil {
				ready, err := page.Context(ctx).Eval(`()=>document.readyState`)
				if err != nil || ready == nil || ready.Value.String() != "complete" {
					if err := page.Context(ctx).Timeout(30 * time.Second).WaitLoad(); err != nil {
						return err
					}
				}
				r, err := resolveLinkedInJob(page.Context(ctx), linkedInExpectedID(lazy.job.URL), false, lazy.job)
				if err != nil {
					return err
				}
				if r.IdentityVerified && r.Kind == "external" && classifyExternalATS(r.Destination) != ATSKindUnknown {
					return b.externalATSApply(ctx, page, lazy, ac, r.Destination)
				}
			}
			if platform == domain.PlatformSeek {
				return b.adaptiveSeekApply(ctx, page, lazy, ac)
			}
			return b.adaptiveEasyApply(ctx, page, lazy, ac)
		}
	}
	previousObserver := b.cfg.ShadowStepFn
	b.cfg.ShadowStepFn = nil
	defer func() { b.cfg.ShadowStepFn = previousObserver }()
	installShadowObserverIfNeeded(b, ac, nil)
	if observer := b.cfg.ShadowStepFn; observer != nil && lazy != nil && page != nil {
		scope := nativeEmployerScope(ctx, page, string(platform), lazy.job)
		b.cfg.ShadowStepFn = func(ctx context.Context, snap StepSnapshot) {
			snap.EmployerKey = scope
			observer(ctx, snap)
		}
	}
	switch platform {
	case domain.PlatformLinkedIn:
		return b.easyApply(ctx, page, lazy)
	case domain.PlatformSeek:
		return b.seekApply(ctx, page, lazy)
	default:
		return fmt.Errorf("no apply handler for platform %s", platform)
	}
}

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
	if current == domain.ApplyEngineLegacy || (ac.Engine == domain.ApplyEngineAdaptive && current != domain.ApplyEngineAdaptive) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := writeEngineAudit(ctx, db, adminID, reason, oldCfg, newCfg); err != nil {
		log.Warn().Err(err).Str("admin_id", adminID).Msg("engine audit: failed to write")
	}
}

// WriteEngineAuditTx records the change in the same transaction as its settings.
func WriteEngineAuditTx(ctx context.Context, tx *sql.Tx, adminID, reason string, oldCfg, newCfg domain.ApplyEngineConfig) error {
	return writeEngineAudit(ctx, tx, adminID, reason, oldCfg, newCfg)
}

func writeEngineAudit(ctx context.Context, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, adminID, reason string, oldCfg, newCfg domain.ApplyEngineConfig) error {
	raw, err := json.Marshal(newCfg)
	if err != nil {
		return err
	}
	id := newAttemptID()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx,
		`INSERT INTO apply_engine_audit(id,admin_id,old_mode,new_mode,reason,config_json,created_at)
		 VALUES(?,?,?,?,?,?,?)`,
		id, adminID, string(oldCfg.Mode), string(newCfg.Mode), reason, string(raw), now,
	)
	return err
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
	defer func() { _ = rows.Close() }()
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
