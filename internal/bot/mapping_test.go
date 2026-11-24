package bot

// mapping_test.go — unit tests for Story #137: DBMappingStore (field-level, story #138)

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

// Reference mappings contain a typed profile key, never a candidate value.
func referenceResolution(ref, key string) FieldResolution {
	return FieldResolution{Ref: ref, Source: SourceProfile, Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: key}
}

func TestDBMappingStore_ColdMiss_WarmHit(t *testing.T) {
	db := openMappingDB(t)
	snap, proposals := verifiedNoticeFixture()
	store := &DBMappingStore{DB: db, UserID: "user-1"}
	_, hit := store.LookupField(snap.Fields[0].FieldSignature("linkedin"), "")
	require.False(t, hit)
	interp := StepInterpretation{Fields: validateLLMProposals(proposals, []string{"f0"}, snap, basicProfile())}
	require.NoError(t, store.RecordProposals(context.Background(), snap, interp))
	require.NoError(t, store.Write(context.Background(), snap, interp.Fields))
	_, hit = store.LookupField(snap.Fields[0].FieldSignature("linkedin"), "")
	assert.False(t, hit, "mechanical acceptance is not semantic review")
	require.NoError(t, reviewFixtureMappings(t, store, snap, interp.Fields))
	got, hit := store.LookupField(snap.Fields[0].FieldSignature("linkedin"), "")
	require.True(t, hit)
	assert.Empty(t, got.Value, "cache must not retain a candidate answer")
	assert.Equal(t, "application_defaults.notice_period", got.BindingKey)
	assert.Equal(t, ProvenanceConfirmedByReviewer, got.Provenance)
}

func TestDBMappingStore_PendingNotWritten(t *testing.T) {
	db := openMappingDB(t)
	f := textField("f0", "Name")
	store := &DBMappingStore{DB: db, UserID: "user-1"}
	r := referenceResolution("f0", "personal_information.name")
	r.Pending = true
	require.NoError(t, store.Write(context.Background(), snapWithFields(f), []FieldResolution{r}))
	_, hit := store.LookupField(f.FieldSignature("linkedin"), "")
	assert.False(t, hit)
}

func TestDBMappingStore_ShadowReadOnly_NoWrite(t *testing.T) {
	db := openMappingDB(t)
	f := textField("f0", "Name")
	store := &DBMappingStore{DB: db, UserID: "user-1", ReadOnly: true}
	require.NoError(t, store.Write(context.Background(), snapWithFields(f), []FieldResolution{referenceResolution("f0", "personal_information.name")}))
	require.NoError(t, store.RecordProposals(context.Background(), snapWithFields(f), StepInterpretation{Fields: []FieldResolution{{Ref: "f0", Source: SourceLLM}}}))
	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM form_field_mappings").Scan(&n))
	assert.Zero(t, n)
}

func TestDBMappingStore_UserScopedFallbackToGlobal(t *testing.T) {
	db := openMappingDB(t)
	f := textField("f0", "Given name")
	// An explicitly validated generic reference is allowed to be shared. It
	// contains no applicant data and is resolved against each candidate at replay.
	global := &DBMappingStore{DB: db}
	require.NoError(t, global.Write(context.Background(), snapWithFields(f), []FieldResolution{referenceResolution("f0", "personal_information.name")}))
	user := &DBMappingStore{DB: db, UserID: "another-user"}
	got, hit := user.LookupField(f.FieldSignature("linkedin"), "")
	require.True(t, hit)
	assert.Equal(t, "personal_information.name", got.BindingKey)
	assert.Empty(t, got.Value)
}

func TestDBMappingStore_UserScopedTakesPrecedence(t *testing.T) {
	db := openMappingDB(t)
	f := textField("f0", "Contact name")
	global := &DBMappingStore{DB: db}
	own := &DBMappingStore{DB: db, UserID: "user-1"}
	require.NoError(t, global.Write(context.Background(), snapWithFields(f), []FieldResolution{referenceResolution("f0", "personal_information.name")}))
	require.NoError(t, own.Write(context.Background(), snapWithFields(f), []FieldResolution{referenceResolution("f0", "personal_information.full_name")}))
	got, hit := own.LookupField(f.FieldSignature("linkedin"), "")
	require.True(t, hit)
	assert.Equal(t, "personal_information.full_name", got.BindingKey)
	other := &DBMappingStore{DB: db, UserID: "user-2"}
	got, hit = other.LookupField(f.FieldSignature("linkedin"), "")
	require.True(t, hit)
	assert.Equal(t, "personal_information.name", got.BindingKey, "other candidate's specific mapping cannot leak")
}

func TestDBMappingStore_Invalidate_ExcludesFromLookup(t *testing.T) {
	db := openMappingDB(t)
	f := textField("f0", "Given name")
	snap := snapWithFields(f)
	own := &DBMappingStore{DB: db, UserID: "user-1"}
	other := &DBMappingStore{DB: db, UserID: "user-2"}
	r := referenceResolution("f0", "personal_information.name")
	require.NoError(t, own.Write(context.Background(), snap, []FieldResolution{r}))
	require.NoError(t, other.Write(context.Background(), snap, []FieldResolution{r}))
	require.NoError(t, own.Invalidate(context.Background(), f.FieldSignature("linkedin")))
	_, hit := own.LookupField(f.FieldSignature("linkedin"), "")
	assert.False(t, hit)
	_, hit = other.LookupField(f.FieldSignature("linkedin"), "")
	assert.True(t, hit)
}

func TestDBMappingStore_Invalidate_MissingEntry_IsNoOp(t *testing.T) {
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "user-1"}
	require.NoError(t, store.Invalidate(context.Background(), "missing"))
}

func TestDBMappingStore_ReviewSourceNotWritten(t *testing.T) {
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "user-1"}
	snap := snapWithFields(textField("f0", "Name"))
	require.NoError(t, store.Write(context.Background(), snap, []FieldResolution{{Ref: "f0", Source: SourceReview, Action: ActionSkip}}))
	_, hit := store.LookupField(snap.Fields[0].FieldSignature("linkedin"), "")
	assert.False(t, hit)
}

func TestDBMappingStore_NilDB_LookupReturnsMiss(t *testing.T) {
	_, hit := (&DBMappingStore{}).LookupField("field", "")
	assert.False(t, hit)
}

func TestDBMappingStore_NilStore_LookupReturnsMiss(t *testing.T) {
	var store *DBMappingStore
	_, hit := store.LookupField("field", "")
	assert.False(t, hit)
}

func TestDBMappingStore_WarmPath_ZeroLLMCalls(t *testing.T) {
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "user-1"}
	snap, proposals := verifiedNoticeFixture()
	si := &StepInterpreter{LLM: &stubLLM{proposals: proposals}, Mapping: store}
	cold, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	require.NoError(t, reviewFixtureMappings(t, store, snap, cold.Fields))
	warm := &StepInterpreter{Mapping: store}
	profile := basicProfile()
	profile.ApplicationDefaults.NoticePeriod = "3 weeks"
	got, err := warm.InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, got.Paused)
	assert.False(t, got.LLMUsed)
	assert.Equal(t, "3 weeks", got.Fields[0].Value)
}
