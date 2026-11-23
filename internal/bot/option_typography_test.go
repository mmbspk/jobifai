package bot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptionTypography_SmartQuotesLearnExactLabelAndReuse(t *testing.T) {
	profile := groundedFixtureProfile()
	snap := makeSnap(SnapField{Ref: "f0", Label: "Are you currently based in Hobart or willing to relocate?", Type: SnapTypeRadio, Required: true, Options: []string{"Yes, I’m based in Hobart", "No, but I’m willing to relocate", "No, and I’m not willing to relocate"}})
	snap.EmployerKey = "verified-issuer"
	server := newGroundedCountingServer(t, func([]byte) string {
		return `[{"ref":"f0","action":"fill","intent_type":"llm","value":"Yes, I'm based in Hobart","evidence":[{"path":"/personal_information/city","quote":"Hobart"}]}]`
	})
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	cold, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t)), Mapping: store}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, cold.Paused)
	require.Equal(t, snap.Fields[0].Options[0], cold.Fields[0].Value)
	require.Equal(t, ProvenanceVerifiedByModel, cold.Fields[0].Provenance)
	require.Equal(t, int32(2), server.requests.Load())
	writeVerifiedFilledStep(context.Background(), store, snap, cold, StepExecResult{FilledRefs: []string{"f0"}})
	warm, err := (&StepInterpreter{Mapping: store}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, warm.Paused)
	require.Equal(t, SourceMapping, warm.Fields[0].Source)
	require.Equal(t, snap.Fields[0].Options[0], warm.Fields[0].Value)
	require.Equal(t, int32(2), server.requests.Load())
}

func TestOptionTypography_OnlyUniqueTypographicEquivalence(t *testing.T) {
	for _, tc := range []struct {
		value   string
		options []string
		want    string
		ok      bool
	}{
		{"yes, I'm based here", []string{"Yes, I’m based here"}, "Yes, I’m based here", true},
		{"Yes  I’m based here", []string{"Yes I’m based here"}, "Yes I’m based here", true},
		{"No, I'm willing", []string{"No, I’m not willing"}, "", false},
		{"1-2 weeks", []string{"3-4 weeks"}, "", false},
		{"Yes, I'm based here", []string{"Yes, I’m based here", "Yes, I'm based here"}, "", false},
	} {
		value, ok := currentOptionTypography(SnapField{Options: tc.options}, tc.value)
		require.Equal(t, tc.ok, ok)
		require.Equal(t, tc.want, value)
	}
}
