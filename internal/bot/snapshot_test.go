package bot_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/bot"
)

// ── CaptureStepSnapshot ───────────────────────────────────────────────────────

func TestSnapshot_BasicCapture(t *testing.T) {
	fields := []bot.ExportedFormField{
		{Type: "text", Question: "First name", Required: true},
		{Type: "radio", Question: "Work authorization", Options: []bot.ExportedFormFieldOption{
			{Label: "Yes"}, {Label: "No"},
		}},
		{Type: "select", Question: "Years of experience", Options: []bot.ExportedFormFieldOption{
			{Label: "0-2"}, {Label: "3-5"}, {Label: "5+"},
		}},
	}

	snap := bot.ExportCaptureStepSnapshot(fields, nil, "linkedin", 0, "Personal info")

	assert.Equal(t, 1, snap.V, "schema version must be 1")
	assert.Equal(t, "linkedin", snap.Platform)
	assert.Equal(t, 0, snap.StepIndex)
	assert.Equal(t, "Personal info", snap.StepTitle)
	require.Len(t, snap.Fields, 3)

	f0 := snap.Fields[0]
	assert.Equal(t, "f0", f0.Ref)
	assert.Equal(t, "First name", f0.Label)
	assert.Equal(t, bot.SnapTypeText, f0.Type)
	assert.True(t, f0.Required)
	assert.Empty(t, f0.Options, "text field has no options")

	f1 := snap.Fields[1]
	assert.Equal(t, "f1", f1.Ref)
	assert.Equal(t, bot.SnapTypeRadio, f1.Type)
	assert.Equal(t, []string{"Yes", "No"}, f1.Options)

	f2 := snap.Fields[2]
	assert.Equal(t, bot.SnapTypeSelect, f2.Type)
	assert.Equal(t, []string{"0-2", "3-5", "5+"}, f2.Options)
}

func TestSnapshot_PrivacyNoValues(t *testing.T) {
	// Even if the caller somehow passes a field with HasFile=false (not filled),
	// and another with HasFile=true, the snapshot only records Filled bool.
	fields := []bot.ExportedFormField{
		{Type: "file", Question: "Resume", HasFile: true, DocumentKind: "resume"},
		{Type: "text", Question: "Cover note"},
	}
	snap := bot.ExportCaptureStepSnapshot(fields, nil, "seek", 1, "")

	assert.False(t, snap.Fields[0].Filled, "an existing platform attachment does not prove the approved document is selected")
	assert.False(t, snap.Fields[1].Filled, "text field not filled → Filled=false")
}

func TestSnapshot_FieldByRef_Present(t *testing.T) {
	fields := []bot.ExportedFormField{
		{Type: "text", Question: "City"},
		{Type: "radio", Question: "Remote?", Options: []bot.ExportedFormFieldOption{{Label: "Yes"}, {Label: "No"}}},
	}
	snap := bot.ExportCaptureStepSnapshot(fields, nil, "seek", 0, "")

	f, ok := snap.FieldByRef("f1")
	require.True(t, ok)
	assert.Equal(t, "Remote?", f.Label)
}

func TestSnapshot_FieldByRef_StaleReturnsFalse(t *testing.T) {
	fields := []bot.ExportedFormField{{Type: "text", Question: "Name"}}
	snap := bot.ExportCaptureStepSnapshot(fields, nil, "linkedin", 0, "")

	_, ok := snap.FieldByRef("f99")
	assert.False(t, ok, "stale/nonexistent ref must return false")
}

func TestSnapshot_DuplicateLabels_BothCaptured(t *testing.T) {
	// Same label text must produce distinct refs — not deduplicated.
	fields := []bot.ExportedFormField{
		{Type: "text", Question: "Experience (years)"},
		{Type: "text", Question: "Experience (years)"},
	}
	snap := bot.ExportCaptureStepSnapshot(fields, nil, "linkedin", 2, "")
	require.Len(t, snap.Fields, 2)
	assert.Equal(t, "f0", snap.Fields[0].Ref)
	assert.Equal(t, "f1", snap.Fields[1].Ref)
	assert.Equal(t, snap.Fields[0].Label, snap.Fields[1].Label, "labels match")
}

func TestSnapshot_HiddenControlsExcluded(t *testing.T) {
	// jsScanFields already filters invisible controls; snapshot trusts that.
	// A file field with UploadViaButton (button-only, no visible input) must
	// still appear — it IS on the visible step, just captured differently.
	fields := []bot.ExportedFormField{
		{Type: "file", Question: "Resume", UploadViaButton: true},
		{Type: "text", Question: "City"},
	}
	snap := bot.ExportCaptureStepSnapshot(fields, nil, "linkedin", 0, "")
	require.Len(t, snap.Fields, 2, "button-upload file field must be captured")
	assert.Equal(t, bot.SnapTypeFile, snap.Fields[0].Type)
}

func TestSnapshot_OptionsBounded(t *testing.T) {
	// More than maxSnapOptions (20) options must be truncated.
	var opts []bot.ExportedFormFieldOption
	for i := 0; i < 30; i++ {
		opts = append(opts, bot.ExportedFormFieldOption{Label: strings.Repeat("x", 5)})
	}
	fields := []bot.ExportedFormField{{Type: "select", Question: "Country", Options: opts}}
	snap := bot.ExportCaptureStepSnapshot(fields, nil, "seek", 0, "")
	assert.LessOrEqual(t, len(snap.Fields[0].Options), 20, "options must be capped at 20")
}

func TestSnapshot_OptionLabelTruncated(t *testing.T) {
	longLabel := strings.Repeat("a", 200)
	fields := []bot.ExportedFormField{
		{Type: "select", Question: "Q", Options: []bot.ExportedFormFieldOption{{Label: longLabel}}},
	}
	snap := bot.ExportCaptureStepSnapshot(fields, nil, "linkedin", 0, "")
	require.Len(t, snap.Fields[0].Options, 1)
	assert.LessOrEqual(t, len(snap.Fields[0].Options[0]), 100)
}

func TestSnapshot_FieldsBounded(t *testing.T) {
	// More than 50 fields must be truncated.
	var fields []bot.ExportedFormField
	for i := 0; i < 60; i++ {
		fields = append(fields, bot.ExportedFormField{Type: "text", Question: "Q"})
	}
	snap := bot.ExportCaptureStepSnapshot(fields, nil, "linkedin", 0, "")
	assert.LessOrEqual(t, len(snap.Fields), 50)
}

// ── SemanticSignature ─────────────────────────────────────────────────────────

func TestSnapshot_SemanticSignature_SameStructure(t *testing.T) {
	fields := []bot.ExportedFormField{
		{Type: "text", Question: "City"},
		{Type: "radio", Question: "Sponsorship?", Options: []bot.ExportedFormFieldOption{{Label: "Yes"}, {Label: "No"}}},
	}
	s1 := bot.ExportCaptureStepSnapshot(fields, nil, "linkedin", 0, "Work auth")
	s2 := bot.ExportCaptureStepSnapshot(fields, nil, "linkedin", 1, "Work auth")

	// Step index does not affect semantic signature; step title and fields do.
	assert.Equal(t, s1.SemanticSignature(), s2.SemanticSignature(),
		"different step indices with same structure must have equal signatures")
}

func TestSnapshot_SemanticSignature_DifferentPlatform(t *testing.T) {
	fields := []bot.ExportedFormField{{Type: "text", Question: "Name"}}
	s1 := bot.ExportCaptureStepSnapshot(fields, nil, "linkedin", 0, "")
	s2 := bot.ExportCaptureStepSnapshot(fields, nil, "seek", 0, "")
	assert.NotEqual(t, s1.SemanticSignature(), s2.SemanticSignature())
}

func TestSnapshot_SemanticSignature_FieldOrderIndependent(t *testing.T) {
	a := bot.ExportedFormField{Type: "text", Question: "City"}
	b := bot.ExportedFormField{Type: "radio", Question: "Sponsorship?"}

	snap1 := bot.ExportCaptureStepSnapshot([]bot.ExportedFormField{a, b}, nil, "linkedin", 0, "Step")
	snap2 := bot.ExportCaptureStepSnapshot([]bot.ExportedFormField{b, a}, nil, "linkedin", 0, "Step")

	assert.Equal(t, snap1.SemanticSignature(), snap2.SemanticSignature(),
		"field order is sorted out of signature — same form reordered must match")
}

func TestSnapshot_SemanticSignature_DifferentFields(t *testing.T) {
	s1 := bot.ExportCaptureStepSnapshot([]bot.ExportedFormField{
		{Type: "text", Question: "City"},
	}, nil, "linkedin", 0, "")
	s2 := bot.ExportCaptureStepSnapshot([]bot.ExportedFormField{
		{Type: "text", Question: "Country"},
	}, nil, "linkedin", 0, "")
	assert.NotEqual(t, s1.SemanticSignature(), s2.SemanticSignature())
}

// ── NavActions ────────────────────────────────────────────────────────────────

func TestSnapshot_NavActionsPreserved(t *testing.T) {
	navs := []bot.SnapNav{
		{Ref: "n0", Label: "Back", Kind: bot.NavKindBack},
		{Ref: "n1", Label: "Next", Kind: bot.NavKindNext},
	}
	fields := []bot.ExportedFormField{{Type: "text", Question: "Name"}}
	snap := bot.ExportCaptureStepSnapshot(fields, navs, "linkedin", 2, "")

	require.Len(t, snap.NavActions, 2)
	n, ok := snap.NavByRef("n1")
	require.True(t, ok)
	assert.Equal(t, bot.NavKindNext, n.Kind)
}

// ── Type classification ───────────────────────────────────────────────────────

func TestSnapshot_TypeClassification(t *testing.T) {
	cases := []struct {
		field bot.ExportedFormField
		want  bot.SnapFieldType
	}{
		{bot.ExportedFormField{Type: "text", InputType: ""}, bot.SnapTypeText},
		{bot.ExportedFormField{Type: "text", InputType: "number"}, bot.SnapTypeNumber},
		{bot.ExportedFormField{Type: "text", InputType: "textarea"}, bot.SnapTypeTextarea},
		{bot.ExportedFormField{Type: "text", Typeahead: true}, bot.SnapTypeTypeahead},
		{bot.ExportedFormField{Type: "radio"}, bot.SnapTypeRadio},
		{bot.ExportedFormField{Type: "select"}, bot.SnapTypeSelect},
		{bot.ExportedFormField{Type: "file"}, bot.SnapTypeFile},
		{bot.ExportedFormField{Type: "checkbox"}, bot.SnapTypeCheckbox},
		{bot.ExportedFormField{Type: "unknown_future_type"}, bot.SnapTypeUnsupported},
	}
	for _, tc := range cases {
		snap := bot.ExportCaptureStepSnapshot([]bot.ExportedFormField{tc.field}, nil, "linkedin", 0, "")
		require.Len(t, snap.Fields, 1)
		assert.Equal(t, tc.want, snap.Fields[0].Type, "type=%q inputType=%q typeahead=%v", tc.field.Type, tc.field.InputType, tc.field.Typeahead)
	}
}

func TestSnapshot_LabelNormalized(t *testing.T) {
	fields := []bot.ExportedFormField{{Type: "text", Question: "  First   name  "}}
	snap := bot.ExportCaptureStepSnapshot(fields, nil, "linkedin", 0, "")
	assert.Equal(t, "First name", snap.Fields[0].Label)
}
