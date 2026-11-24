package bot

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"strings"
	"testing"
)

func TestInterpretationOptions_LargeDropdownColdLearnsWarmReuses(t *testing.T) {
	profile := &domain.ResumeProfile{EducationDetails: []domain.EducationDetail{{Institution: "South College", EducationLevel: "BSc", FieldOfStudy: "Computer Science"}}}
	var options []formFieldOption
	for i := 0; i < 320; i++ {
		options = append(options, formFieldOption{Label: fmt.Sprintf("Discipline %d", i), Value: fmt.Sprintf("discipline_%d", i)})
	}
	options[200] = formFieldOption{Label: "Computer and Information Science", Value: "Computer_and_Information_Science"}
	snap := captureStepSnapshot([]formField{{Type: "select", Question: "Education Field of Study", ID: "study", Required: true, ProfileRecordPath: "/education_details/0", Options: options}}, nil, "linkedin", 0, "Education")
	snap.EmployerKey = "verified-issuer"
	require.True(t, snap.Fields[0].Truncated)
	require.NotContains(t, snap.Fields[0].Options, "Computer and Information Science")
	originalSignature := snap.Fields[0].FieldSignature(snap.Platform)
	server := newGroundedCountingServer(t, func(body []byte) string {
		require.Contains(t, string(body), "Computer and Information Science")
		return `[{"ref":"f0","action":"fill","intent_type":"profile","binding_key":"","value":"Computer and Information Science","evidence":[{"path":"/education_details/0/field_of_study","quote":"Computer Science"}]}]`
	})
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	cold, err := (&StepInterpreter{LLM: NewLLMFormInterpreter(server.newClient(t)), Mapping: store}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, cold.Paused, "%+v", cold.Fields)
	require.Equal(t, "Computer and Information Science", cold.Fields[0].Value)
	require.Equal(t, ProvenanceVerifiedByModel, cold.Fields[0].Provenance)
	require.Equal(t, int32(2), server.requests.Load())
	writeVerifiedFilledStep(context.Background(), store, snap, cold, StepExecResult{Outcome: StepOutcomeError, FilledRefs: []string{"f0"}})
	warm, err := (&StepInterpreter{Mapping: store}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, warm.Paused)
	require.Equal(t, SourceMapping, warm.Fields[0].Source)
	require.Equal(t, "Computer and Information Science", warm.Fields[0].Value)
	require.Equal(t, int32(2), server.requests.Load(), "warm replay adds no model requests")
	require.True(t, snap.Fields[0].Truncated, "interpretation does not expand stored snapshots")
	require.Equal(t, originalSignature, snap.Fields[0].FieldSignature(snap.Platform))
}

func TestInterpretationOptions_RemainBounded(t *testing.T) {
	for _, options := range [][]formFieldOption{make([]formFieldOption, maxInterpretOptions+1), {{Label: strings.Repeat("x", maxInterpretOptionBytes+1)}}} {
		field := SnapField{Truncated: true, BrowserField: &formField{Options: options}}
		_, ok := expandInterpretationOptions(field)
		require.False(t, ok)
	}
}
