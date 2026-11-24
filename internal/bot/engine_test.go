package bot_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/domain"
)

// --- EffectiveModeFor tests ---

func TestEffectiveModeFor_DefaultLegacy(t *testing.T) {
	cfg := domain.ApplyEngineConfig{}
	assert.Equal(t, domain.ApplyEngineLegacy, cfg.EffectiveModeFor("any-user", "linkedin"))
}

func TestEffectiveModeFor_GlobalShadow(t *testing.T) {
	cfg := domain.ApplyEngineConfig{Mode: domain.ApplyEngineShadow}
	assert.Equal(t, domain.ApplyEngineShadow, cfg.EffectiveModeFor("any-user", "linkedin"))
}

func TestEffectiveModeFor_PlatformOverrideWins(t *testing.T) {
	cfg := domain.ApplyEngineConfig{
		Mode: domain.ApplyEngineLegacy,
		PlatformOverrides: map[string]domain.ApplyEngineMode{
			"seek": domain.ApplyEngineShadow,
		},
	}
	assert.Equal(t, domain.ApplyEngineShadow, cfg.EffectiveModeFor("any-user", "seek"))
	assert.Equal(t, domain.ApplyEngineLegacy, cfg.EffectiveModeFor("any-user", "linkedin"))
}

func TestEffectiveModeFor_PilotRestriction_Adaptive(t *testing.T) {
	cfg := domain.ApplyEngineConfig{
		Mode:         domain.ApplyEngineAdaptive,
		PilotUserIDs: []string{"pilot-user"},
	}
	// Pilot user gets adaptive.
	assert.Equal(t, domain.ApplyEngineAdaptive, cfg.EffectiveModeFor("pilot-user", "linkedin"))
	// Non-pilot user falls back to legacy.
	assert.Equal(t, domain.ApplyEngineLegacy, cfg.EffectiveModeFor("other-user", "linkedin"))
}

func TestEffectiveModeFor_PilotRestriction_Shadow(t *testing.T) {
	cfg := domain.ApplyEngineConfig{
		Mode:         domain.ApplyEngineShadow,
		PilotUserIDs: []string{"pilot-user"},
	}
	assert.Equal(t, domain.ApplyEngineShadow, cfg.EffectiveModeFor("pilot-user", "linkedin"))
	assert.Equal(t, domain.ApplyEngineLegacy, cfg.EffectiveModeFor("non-pilot", "linkedin"))
}

func TestEffectiveModeFor_EmptyPilotListMeansAll(t *testing.T) {
	cfg := domain.ApplyEngineConfig{
		Mode:         domain.ApplyEngineAdaptive,
		PilotUserIDs: nil,
	}
	assert.Equal(t, domain.ApplyEngineAdaptive, cfg.EffectiveModeFor("any-user", "linkedin"))
}

// --- Audit table tests ---

func TestWriteAndLoadEngineAudit(t *testing.T) {
	db := openTestDB(t)

	old := domain.ApplyEngineConfig{Mode: domain.ApplyEngineLegacy}
	newCfg := domain.ApplyEngineConfig{Mode: domain.ApplyEngineShadow, ExternalATSEnabled: false}

	bot.WriteEngineAudit(db, "admin-1", "test reason", old, newCfg)

	entries, err := bot.LoadEngineAudit(db, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	e := entries[0]
	assert.Equal(t, "admin-1", e.AdminID)
	assert.Equal(t, domain.ApplyEngineLegacy, e.OldMode)
	assert.Equal(t, domain.ApplyEngineShadow, e.NewMode)
	assert.Equal(t, "test reason", e.Reason)
	assert.Equal(t, domain.ApplyEngineShadow, e.Config.Mode)
}

func TestLoadEngineAudit_NilDB(t *testing.T) {
	entries, err := bot.LoadEngineAudit(nil, 10)
	require.NoError(t, err)
	assert.Nil(t, entries)
}

func TestLoadEngineAudit_MostRecentFirst(t *testing.T) {
	db := openTestDB(t)

	bot.WriteEngineAudit(db, "admin-1", "", domain.ApplyEngineConfig{Mode: domain.ApplyEngineLegacy}, domain.ApplyEngineConfig{Mode: domain.ApplyEngineShadow})
	bot.WriteEngineAudit(db, "admin-1", "", domain.ApplyEngineConfig{Mode: domain.ApplyEngineShadow}, domain.ApplyEngineConfig{Mode: domain.ApplyEngineLegacy})

	entries, err := bot.LoadEngineAudit(db, 10)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, domain.ApplyEngineLegacy, entries[0].NewMode, "most recent entry should be first")
	assert.Equal(t, domain.ApplyEngineShadow, entries[1].NewMode)
}

// --- #128 Baseline: Attempt engine column tests ---

func TestAttemptContext_EngineFieldPersistedByBeginAttempt(t *testing.T) {
	db := openTestDB(t)

	ac := bot.ExportNewAttemptContext("user-1", "linkedin", "job-123", "https://linkedin.com/jobs/view/123", "Engineer", "Acme", "runSubmit", "run-1")
	ac.Engine = domain.ApplyEngineShadow
	bot.ExportBeginAttempt(db, ac)

	var engine string
	err := db.QueryRow(`SELECT engine FROM apply_attempts WHERE id = ?`, ac.AttemptID).Scan(&engine)
	require.NoError(t, err)
	assert.Equal(t, "shadow", engine)
}

func TestAttemptContext_LegacyEngineDefault(t *testing.T) {
	db := openTestDB(t)

	// ac with zero Engine value (Legacy is default).
	ac := bot.ExportNewAttemptContext("user-2", "seek", "job-456", "https://seek.com.au/job/456", "Designer", "Corp", "runSubmit", "run-2")
	bot.ExportBeginAttempt(db, ac)

	var engine string
	err := db.QueryRow(`SELECT engine FROM apply_attempts WHERE id = ?`, ac.AttemptID).Scan(&engine)
	require.NoError(t, err)
	assert.Equal(t, "", engine, "empty string is the legacy default for the engine column")
}
