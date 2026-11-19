package bot

// mapping_test.go — unit tests for Story #137: DBMappingStore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appdb "github.com/user/jobifai/internal/db"
)

func openMappingDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// snapWithFields builds a minimal StepSnapshot for mapping tests.
func snapWithFields(fields ...SnapField) StepSnapshot {
	return StepSnapshot{
		V:         SnapshotSchemaV1,
		Platform:  "linkedin",
		StepTitle: "Contact Info",
		Fields:    fields,
	}
}

// ── cold miss → warm hit ──────────────────────────────────────────────────────

func TestDBMappingStore_ColdMiss_WarmHit(t *testing.T) {
	db := openMappingDB(t)
	snap := snapWithFields(
		SnapField{Ref: "f0", Label: "Years of Experience", Type: SnapTypeText},
	)
	sig := snap.SemanticSignature()
	store := &DBMappingStore{DB: db, UserID: "user-1"}

	// Cold: no mapping yet.
	_, ok := store.Lookup(sig, "f0")
	require.False(t, ok, "cold lookup must miss")

	// Write a verified LLM resolution.
	resolutions := []FieldResolution{
		{Ref: "f0", Source: SourceLLM, Action: ActionFill, Value: "5 years"},
	}
	require.NoError(t, store.Write(context.Background(), snap, resolutions))

	// Warm: mapping now exists.
	got, ok := store.Lookup(sig, "f0")
	require.True(t, ok, "warm lookup must hit")
	assert.Equal(t, SourceMapping, got.Source)
	assert.Equal(t, ActionFill, got.Action)
}

// ── pending resolutions are never written ─────────────────────────────────────

func TestDBMappingStore_PendingNotWritten(t *testing.T) {
	db := openMappingDB(t)
	snap := snapWithFields(SnapField{Ref: "f0", Label: "Cover Letter", Type: SnapTypeTextarea})
	sig := snap.SemanticSignature()
	store := &DBMappingStore{DB: db, UserID: "user-1"}

	pending := []FieldResolution{
		{Ref: "f0", Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingLLMUnavailable},
	}
	require.NoError(t, store.Write(context.Background(), snap, pending))

	_, ok := store.Lookup(sig, "f0")
	assert.False(t, ok, "pending resolution must not be written")
}

// ── shadow mode: ReadOnly=true blocks writes ──────────────────────────────────

func TestDBMappingStore_ShadowReadOnly_NoWrite(t *testing.T) {
	db := openMappingDB(t)
	snap := snapWithFields(SnapField{Ref: "f0", Label: "Job Title", Type: SnapTypeText})
	sig := snap.SemanticSignature()

	shadowStore := &DBMappingStore{DB: db, UserID: "user-1", ReadOnly: true}
	resolutions := []FieldResolution{
		{Ref: "f0", Source: SourceLLM, Action: ActionFill, Value: "Engineer"},
	}
	require.NoError(t, shadowStore.Write(context.Background(), snap, resolutions))

	// ReadOnly store must not write.
	_, ok := shadowStore.Lookup(sig, "f0")
	assert.False(t, ok, "shadow/ReadOnly store must not write mappings")
}

// ── user-scoped vs global scope ───────────────────────────────────────────────

func TestDBMappingStore_UserScopedFallbackToGlobal(t *testing.T) {
	db := openMappingDB(t)
	snap := snapWithFields(SnapField{Ref: "f0", Label: "Department", Type: SnapTypeSelect, Options: []string{"Engineering", "Sales"}})
	sig := snap.SemanticSignature()

	// Write with global scope (user_id='').
	globalStore := &DBMappingStore{DB: db, UserID: ""}
	resolutions := []FieldResolution{{Ref: "f0", Source: SourceLLM, Action: ActionFill}}
	require.NoError(t, globalStore.Write(context.Background(), snap, resolutions))

	// A different user's store should still find it via global fallback.
	userStore := &DBMappingStore{DB: db, UserID: "user-99"}
	got, ok := userStore.Lookup(sig, "f0")
	require.True(t, ok, "user-scoped lookup must fall back to global mapping")
	assert.Equal(t, SourceMapping, got.Source)
}

func TestDBMappingStore_UserScopedTakesPrecedence(t *testing.T) {
	db := openMappingDB(t)
	snap := snapWithFields(SnapField{Ref: "f0", Label: "Job Level", Type: SnapTypeSelect})
	sig := snap.SemanticSignature()

	globalStore := &DBMappingStore{DB: db, UserID: ""}
	userStore := &DBMappingStore{DB: db, UserID: "user-1"}

	// Both write.
	require.NoError(t, globalStore.Write(context.Background(), snap, []FieldResolution{
		{Ref: "f0", Source: SourceLLM, Action: ActionSkip},
	}))
	require.NoError(t, userStore.Write(context.Background(), snap, []FieldResolution{
		{Ref: "f0", Source: SourceLLM, Action: ActionFill},
	}))

	// User-scoped lookup should return ActionFill (user scope), not ActionSkip (global).
	got, ok := userStore.Lookup(sig, "f0")
	require.True(t, ok)
	assert.Equal(t, ActionFill, got.Action, "user-scoped entry must take precedence over global")
}

// ── invalidation ─────────────────────────────────────────────────────────────

func TestDBMappingStore_Invalidate_ExcludesFromLookup(t *testing.T) {
	db := openMappingDB(t)
	snap := snapWithFields(SnapField{Ref: "f0", Label: "Years of Experience", Type: SnapTypeText})
	sig := snap.SemanticSignature()
	store := &DBMappingStore{DB: db, UserID: "user-1"}

	require.NoError(t, store.Write(context.Background(), snap, []FieldResolution{
		{Ref: "f0", Source: SourceLLM, Action: ActionFill},
	}))
	_, ok := store.Lookup(sig, "f0")
	require.True(t, ok, "must be found before invalidation")

	require.NoError(t, store.Invalidate(context.Background(), sig, "f0"))
	_, ok = store.Lookup(sig, "f0")
	assert.False(t, ok, "must not be found after invalidation")
}

func TestDBMappingStore_Invalidate_MissingEntry_IsNoOp(t *testing.T) {
	db := openMappingDB(t)
	store := &DBMappingStore{DB: db, UserID: "user-1"}
	err := store.Invalidate(context.Background(), "nonexistent-sig", "f0")
	assert.NoError(t, err)
}

// ── review-source not written ─────────────────────────────────────────────────

func TestDBMappingStore_ReviewSourceNotWritten(t *testing.T) {
	db := openMappingDB(t)
	snap := snapWithFields(SnapField{Ref: "f0", Label: "Portfolio URL", Type: SnapTypeText})
	sig := snap.SemanticSignature()
	store := &DBMappingStore{DB: db, UserID: "user-1"}

	require.NoError(t, store.Write(context.Background(), snap, []FieldResolution{
		{Ref: "f0", Source: SourceReview, Action: ActionSkip, Pending: false},
	}))
	_, ok := store.Lookup(sig, "f0")
	assert.False(t, ok, "SourceReview resolutions must not be stored")
}

// ── nil db / nil store safety ─────────────────────────────────────────────────

func TestDBMappingStore_NilDB_LookupReturnsMiss(t *testing.T) {
	store := &DBMappingStore{DB: nil}
	_, ok := store.Lookup("sig", "f0")
	assert.False(t, ok)
}

func TestDBMappingStore_NilStore_LookupReturnsMiss(t *testing.T) {
	var store *DBMappingStore
	_, ok := store.Lookup("sig", "f0")
	assert.False(t, ok)
}

// ── zero LLM calls on warm path (integration) ─────────────────────────────────

func TestDBMappingStore_WarmPath_ZeroLLMCalls(t *testing.T) {
	db := openMappingDB(t)
	snap := snapWithFields(
		SnapField{Ref: "f0", Label: "Years of Experience", Type: SnapTypeText},
		SnapField{Ref: "f1", Label: "Cover Letter Prompt", Type: SnapTypeTextarea},
	)

	store := &DBMappingStore{DB: db, UserID: "user-1"}
	llm := &stubLLM{proposals: []LLMFieldProposal{
		{Ref: "f0", Action: "fill", Value: "5"},
		{Ref: "f1", Action: "fill", Value: "I am excited to apply."},
	}}

	si := &StepInterpreter{LLM: llm, Mapping: store}

	// Cold run — LLM called once.
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	assert.Equal(t, 1, llm.calls, "LLM must be called on cold run")
	require.NoError(t, store.Write(context.Background(), snap, interp.Fields))

	// Reset LLM call count.
	llm.calls = 0

	// Warm run — mapping store should handle both fields; LLM must NOT be called.
	interp2, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	assert.Equal(t, 0, llm.calls, "LLM must not be called on warm run")
	assert.False(t, interp2.LLMUsed)
	for _, r := range interp2.Fields {
		assert.Equal(t, SourceMapping, r.Source, "all fields must come from mapping on warm run")
	}
}
