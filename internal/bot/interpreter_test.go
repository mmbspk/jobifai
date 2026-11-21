package bot

// interpreter_test.go — unit tests for Story #131 (step interpreter).
//
// Three-pass resolution coverage:
//   - Deterministic (profile lookups, file upload, unsupported)
//   - Mapping (no-op hook, mapping hit)
//   - LLM batch (success, error, nil LLM)
//   - Validation (invalid action, bad option, missing ref)
//   - Integration (mixed fields, paused on pending)

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func basicProfile() *domain.ResumeProfile {
	return &domain.ResumeProfile{
		PersonalInformation: domain.PersonalInformation{
			Name:    "Fatima",
			Surname: "Malik",
			Email:   "fatima@example.com",
			Phone:   "+61400000001",
			City:    "Sydney",
			Country: "Australia",
			ZipCode: "2000",
			Address: "10 Bridge St",
		},
		ApplicationDefaults: domain.ApplicationDefaults{
			NoticePeriod:        "2 weeks",
			SalaryExpectation:   "90000",
			RequiresSponsorship: false,
		},
	}
}

func textField(ref, label string) SnapField {
	return SnapField{Ref: ref, Label: label, Type: SnapTypeText}
}

func selectField(ref, label string, opts ...string) SnapField {
	return SnapField{Ref: ref, Label: label, Type: SnapTypeSelect, Options: opts}
}

func radioField(ref, label string, opts ...string) SnapField {
	return SnapField{Ref: ref, Label: label, Type: SnapTypeRadio, Options: opts}
}

func fileField(ref, label string) SnapField {
	return SnapField{Ref: ref, Label: label, Type: SnapTypeFile}
}

func unsupportedField(ref, label string) SnapField {
	return SnapField{Ref: ref, Label: label, Type: SnapTypeUnsupported}
}

func makeSnap(fields ...SnapField) StepSnapshot {
	return StepSnapshot{
		V:         SnapshotSchemaV1,
		Platform:  "linkedin",
		StepTitle: "Test Step",
		Fields:    fields,
	}
}

// stubLLM returns a canned list of proposals for every call.
type stubLLM struct {
	proposals []LLMFieldProposal
	err       error
	calls     int
}

func (s *stubLLM) InterpretFields(_ context.Context, _ *domain.ResumeProfile, _ StepSnapshot, _ []string) ([]LLMFieldProposal, error) {
	s.calls++
	return s.proposals, s.err
}

// stubMapping returns a fixed resolution for fields whose sig matches.
// Use SnapField.FieldSignature(platform) to compute the sig to match against.
// Pass an empty sig to match nothing (always miss).
type stubMapping struct {
	fieldSig string
	res      FieldResolution
}

func (s *stubMapping) LookupField(fieldSig, _ string) (FieldResolution, bool) {
	if s.fieldSig != "" && s.fieldSig == fieldSig {
		return s.res, true
	}
	return FieldResolution{}, false
}

// resFor finds a FieldResolution by ref in a StepInterpretation.
func resFor(t *testing.T, interp StepInterpretation, ref string) FieldResolution {
	t.Helper()
	for _, r := range interp.Fields {
		if r.Ref == ref {
			return r
		}
	}
	t.Fatalf("ref %q not found in interpretation", ref)
	return FieldResolution{}
}

// ── deterministic ─────────────────────────────────────────────────────────────

func TestInterpreter_FileFieldAlwaysUpload(t *testing.T) {
	si := &StepInterpreter{}
	snap := makeSnap(fileField("f0", "Resume"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.Equal(t, ActionUpload, r.Action)
	assert.Equal(t, SourceDeterministic, r.Source)
	assert.False(t, r.Pending)
}

func TestInterpreter_UnsupportedFieldPending(t *testing.T) {
	si := &StepInterpreter{}
	snap := makeSnap(unsupportedField("f0", "CAPTCHA"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.True(t, r.Pending)
	assert.Equal(t, PendingUnsupported, r.Reason)
}

func TestInterpreter_FirstNameFromProfile(t *testing.T) {
	si := &StepInterpreter{}
	snap := makeSnap(textField("f0", "First Name"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.Equal(t, "Fatima", r.Value)
	assert.Equal(t, SourceProfile, r.Source)
	assert.Equal(t, ActionFill, r.Action)
}

func TestInterpreter_LastNameFromProfile(t *testing.T) {
	si := &StepInterpreter{}
	snap := makeSnap(textField("f0", "Last Name"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.Equal(t, "Malik", r.Value)
}

func TestInterpreter_EmailFromProfile(t *testing.T) {
	si := &StepInterpreter{}
	snap := makeSnap(textField("f0", "Email Address"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.Equal(t, "fatima@example.com", r.Value)
}

func TestInterpreter_PhoneFromProfile(t *testing.T) {
	si := &StepInterpreter{}
	snap := makeSnap(textField("f0", "Mobile Number"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.Equal(t, "+61400000001", r.Value)
}

func TestInterpreter_NoticePeriodFromDefaults(t *testing.T) {
	si := &StepInterpreter{}
	snap := makeSnap(textField("f0", "Notice Period"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.Equal(t, "2 weeks", r.Value)
}

func TestInterpreter_SalaryFromDefaults(t *testing.T) {
	si := &StepInterpreter{}
	snap := makeSnap(textField("f0", "Expected Salary"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.Equal(t, "90000", r.Value)
}

func TestInterpreter_SponsorshipNo(t *testing.T) {
	si := &StepInterpreter{}
	snap := makeSnap(radioField("f0", "Require Sponsorship", "Yes", "No"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.Equal(t, "No", r.Value)
	assert.Equal(t, SourceProfile, r.Source)
}

func TestInterpreter_SponsorshipYes(t *testing.T) {
	si := &StepInterpreter{}
	p := basicProfile()
	p.ApplicationDefaults.RequiresSponsorship = true
	snap := makeSnap(radioField("f0", "Require Sponsorship", "Yes", "No"))
	interp, err := si.InterpretStep(context.Background(), snap, p)
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.Equal(t, "Yes", r.Value)
}

func TestInterpreter_SponsorshipOptionMissing_FallsToLLM(t *testing.T) {
	// Options don't have Yes/No — deterministic can't resolve, field goes to LLM.
	llm := &stubLLM{proposals: []LLMFieldProposal{{Ref: "f0", Action: "fill", Value: "Absolutely"}}}
	si := &StepInterpreter{LLM: llm}
	snap := makeSnap(radioField("f0", "Require Sponsorship", "Absolutely", "Never"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	assert.Equal(t, 1, llm.calls, "LLM must be called for unresolved sponsorship")
	r := resFor(t, interp, "f0")
	assert.Equal(t, SourceLLM, r.Source)
}

func TestInterpreter_MissingProfilePending(t *testing.T) {
	si := &StepInterpreter{}
	p := basicProfile()
	p.PersonalInformation.Email = ""
	snap := makeSnap(textField("f0", "Email Address"))
	interp, err := si.InterpretStep(context.Background(), snap, p)
	require.NoError(t, err)
	// Deterministic handler doesn't match because value is empty; field goes to LLM (nil) → pending.
	r := resFor(t, interp, "f0")
	assert.True(t, r.Pending)
}

func TestInterpreter_NilProfileNameField_Pending(t *testing.T) {
	si := &StepInterpreter{}
	snap := makeSnap(textField("f0", "First Name"))
	interp, err := si.InterpretStep(context.Background(), snap, nil)
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.True(t, r.Pending)
	assert.Equal(t, PendingMissingProfile, r.Reason)
}

// ── mapping pass ──────────────────────────────────────────────────────────────

func TestInterpreter_MappingHit_NoLLMCall(t *testing.T) {
	field := selectField("f0", "Job Title", "Engineer", "Manager")
	fixedRes := FieldResolution{Ref: "f0", Source: SourceMapping, Action: ActionFill, Value: "Engineer"}
	llm := &stubLLM{proposals: []LLMFieldProposal{{Ref: "f0", Action: "fill", Value: "wrong"}}}
	si := &StepInterpreter{
		LLM:     llm,
		Mapping: &stubMapping{fieldSig: field.FieldSignature("linkedin"), res: fixedRes},
	}
	// Use a non-profile field so deterministic pass misses.
	snap := makeSnap(field)
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	assert.Equal(t, 0, llm.calls, "LLM must not be called when mapping hits")
	r := resFor(t, interp, "f0")
	assert.Equal(t, SourceMapping, r.Source)
	assert.Equal(t, "Engineer", r.Value)
}

func TestInterpreter_MappingMiss_FallsToLLM(t *testing.T) {
	llm := &stubLLM{proposals: []LLMFieldProposal{{Ref: "f0", Action: "fill", Value: "Manager"}}}
	si := &StepInterpreter{
		LLM:     llm,
		Mapping: &stubMapping{}, // empty fieldSig → always miss
	}
	snap := makeSnap(selectField("f0", "Job Title", "Engineer", "Manager"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	assert.Equal(t, 1, llm.calls)
	r := resFor(t, interp, "f0")
	assert.Equal(t, SourceLLM, r.Source)
}

// ── LLM pass ──────────────────────────────────────────────────────────────────

func TestInterpreter_LLMBatch_Success(t *testing.T) {
	llm := &stubLLM{proposals: []LLMFieldProposal{
		{Ref: "f0", Action: "fill", Value: "5 years"},
		{Ref: "f1", Action: "skip"},
	}}
	si := &StepInterpreter{LLM: llm}
	snap := makeSnap(
		textField("f0", "Years of Experience"),
		textField("f1", "Portfolio URL"),
	)
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	assert.True(t, interp.LLMUsed)
	assert.Equal(t, 1, llm.calls)

	r0 := resFor(t, interp, "f0")
	assert.Equal(t, "5 years", r0.Value)
	assert.Equal(t, SourceLLM, r0.Source)

	r1 := resFor(t, interp, "f1")
	assert.Equal(t, ActionSkip, r1.Action)
}

func TestInterpreter_LLMNil_AllPending(t *testing.T) {
	si := &StepInterpreter{} // no LLM
	snap := makeSnap(
		textField("f0", "Years of Experience"),
		textField("f1", "Cover Letter"),
	)
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	assert.False(t, interp.LLMUsed)
	for _, r := range interp.Fields {
		assert.True(t, r.Pending, "field %s should be pending", r.Ref)
		assert.Equal(t, PendingLLMUnavailable, r.Reason)
	}
	assert.True(t, interp.Paused)
}

func TestInterpreter_LLMError_AllPending(t *testing.T) {
	llm := &stubLLM{err: ErrLLMUnavailable}
	si := &StepInterpreter{LLM: llm}
	snap := makeSnap(textField("f0", "Cover Letter"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.True(t, r.Pending)
	assert.Equal(t, PendingLLMUnavailable, r.Reason)
	assert.False(t, interp.LLMUsed)
}

func TestInterpreter_LLMLimitExceeded(t *testing.T) {
	llm := &stubLLM{err: ErrLLMLimitExceeded}
	si := &StepInterpreter{LLM: llm}
	snap := makeSnap(textField("f0", "Cover Letter"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.Equal(t, PendingLimitExceeded, r.Reason)
}

func TestInterpreter_LLMWrappedError_MapsToUnavailable(t *testing.T) {
	llm := &stubLLM{err: errors.New("connection reset")}
	si := &StepInterpreter{LLM: llm}
	snap := makeSnap(textField("f0", "Cover Letter"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.Equal(t, PendingLLMUnavailable, r.Reason)
}

// ── validation ────────────────────────────────────────────────────────────────

func TestInterpreter_LLMInvalidAction_Pending(t *testing.T) {
	llm := &stubLLM{proposals: []LLMFieldProposal{
		{Ref: "f0", Action: "delete", Value: "oops"},
	}}
	si := &StepInterpreter{LLM: llm}
	snap := makeSnap(textField("f0", "Cover Letter"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.True(t, r.Pending)
	assert.Equal(t, PendingMalformedOutput, r.Reason)
}

func TestInterpreter_LLMBadOptionForSelect_Pending(t *testing.T) {
	llm := &stubLLM{proposals: []LLMFieldProposal{
		{Ref: "f0", Action: "fill", Value: "Purple"},
	}}
	si := &StepInterpreter{LLM: llm}
	snap := makeSnap(selectField("f0", "Preferred Shift", "Morning", "Afternoon", "Night"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.True(t, r.Pending)
	assert.Equal(t, PendingInvalidOption, r.Reason)
}

func TestInterpreter_LLMValidOptionForSelect(t *testing.T) {
	llm := &stubLLM{proposals: []LLMFieldProposal{
		{Ref: "f0", Action: "fill", Value: "Morning"},
	}}
	si := &StepInterpreter{LLM: llm}
	snap := makeSnap(selectField("f0", "Preferred Shift", "Morning", "Afternoon", "Night"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.False(t, r.Pending)
	assert.Equal(t, "Morning", r.Value)
}

func TestInterpreter_LLMOptionCaseInsensitiveMatch(t *testing.T) {
	// LLM returns "morning" (lowercase), field has "Morning"
	llm := &stubLLM{proposals: []LLMFieldProposal{
		{Ref: "f0", Action: "fill", Value: "morning"},
	}}
	si := &StepInterpreter{LLM: llm}
	snap := makeSnap(selectField("f0", "Preferred Shift", "Morning", "Afternoon"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.False(t, r.Pending, "case-insensitive match should succeed")
}

func TestInterpreter_LLMMissingRef_Pending(t *testing.T) {
	// LLM returns a proposal for f1 but we asked for f0 — f0 becomes malformed.
	llm := &stubLLM{proposals: []LLMFieldProposal{
		{Ref: "f1", Action: "fill", Value: "something"},
	}}
	si := &StepInterpreter{LLM: llm}
	snap := makeSnap(textField("f0", "Cover Letter"))
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	r := resFor(t, interp, "f0")
	assert.True(t, r.Pending)
	assert.Equal(t, PendingMalformedOutput, r.Reason)
}

// ── integration ───────────────────────────────────────────────────────────────

func TestInterpreter_MixedFields_OneFieldPendingPausesStep(t *testing.T) {
	llm := &stubLLM{proposals: []LLMFieldProposal{
		{Ref: "f2", Action: "fill", Value: "5 years"},
	}}
	si := &StepInterpreter{LLM: llm}
	snap := makeSnap(
		textField("f0", "First Name"),                       // deterministic
		fileField("f1", "Resume"),                           // deterministic (upload)
		textField("f2", "Years of Experience"),              // LLM
		unsupportedField("f3", "Secret validation widget"),  // unsupported → pending
	)
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	assert.True(t, interp.Paused, "step must be paused when any field is pending")

	r0 := resFor(t, interp, "f0")
	assert.Equal(t, "Fatima", r0.Value)

	r1 := resFor(t, interp, "f1")
	assert.Equal(t, ActionUpload, r1.Action)

	r2 := resFor(t, interp, "f2")
	assert.Equal(t, SourceLLM, r2.Source)

	r3 := resFor(t, interp, "f3")
	assert.True(t, r3.Pending)
}

func TestInterpreter_AlreadyFilledFieldSkipped(t *testing.T) {
	si := &StepInterpreter{}
	// Build a snapshot; the captureStepSnapshot ignores Filled — we set it manually.
	snap := makeSnap(textField("f0", "First Name"))
	// Mark the field as already filled.
	snap.Fields[0].Filled = true

	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	// No resolutions — already-filled field is skipped.
	assert.Empty(t, interp.Fields)
}

func TestInterpreter_AllResolved_NoPause(t *testing.T) {
	si := &StepInterpreter{}
	snap := makeSnap(
		textField("f0", "First Name"),
		textField("f1", "Last Name"),
		textField("f2", "Email Address"),
	)
	interp, err := si.InterpretStep(context.Background(), snap, basicProfile())
	require.NoError(t, err)
	assert.False(t, interp.Paused)
	assert.Equal(t, 3, len(interp.Fields))
}

// ── pendingReasonFromErr ──────────────────────────────────────────────────────

func TestPendingReasonFromErr_LimitExceeded(t *testing.T) {
	assert.Equal(t, PendingLimitExceeded, pendingReasonFromErr(ErrLLMLimitExceeded))
}

func TestPendingReasonFromErr_Unavailable(t *testing.T) {
	assert.Equal(t, PendingLLMUnavailable, pendingReasonFromErr(ErrLLMUnavailable))
}

func TestPendingReasonFromErr_WrappedLimit(t *testing.T) {
	wrapped := errors.New("some wrapper: " + ErrLLMLimitExceeded.Error())
	// wrapped is NOT %w-wrapped so it won't match errors.Is; confirms behavior.
	assert.Equal(t, PendingLLMUnavailable, pendingReasonFromErr(wrapped))
}

func TestPendingReasonFromErr_Unknown(t *testing.T) {
	assert.Equal(t, PendingLLMUnavailable, pendingReasonFromErr(errors.New("network hiccup")))
}

// ── matchesAny / containsOptionIgnoreCase ────────────────────────────────────

func TestMatchesAny_HitsSubstring(t *testing.T) {
	assert.True(t, matchesAny("your first name here", "first name"))
}

func TestMatchesAny_Miss(t *testing.T) {
	assert.False(t, matchesAny("years of experience", "first name", "surname"))
}

func TestContainsOptionIgnoreCase_Hit(t *testing.T) {
	assert.True(t, containsOptionIgnoreCase([]string{"Yes", "No"}, "yes"))
	assert.True(t, containsOptionIgnoreCase([]string{"Yes", "No"}, "YES"))
}

func TestContainsOptionIgnoreCase_Miss(t *testing.T) {
	assert.False(t, containsOptionIgnoreCase([]string{"Yes", "No"}, "Maybe"))
}

// ── profileValueForLabel ──────────────────────────────────────────────────────

func TestProfileValueForLabel_NilProfile(t *testing.T) {
	assert.Equal(t, "", profileValueForLabel("first name", nil))
}

func TestProfileValueForLabel_KnownLabels(t *testing.T) {
	p := basicProfile()
	cases := []struct {
		label string
		want  string
	}{
		{"first name", "Fatima"},
		{"given name", "Fatima"},
		{"last name", "Malik"},
		{"email", "fatima@example.com"},
		{"mobile", "+61400000001"},
		{"city", "Sydney"},
		{"country", "Australia"},
		{"notice", "2 weeks"},
		{"salary", "90000"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got := profileValueForLabel(strings.ToLower(tc.label), p)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestProfileValueForLabel_Unknown(t *testing.T) {
	assert.Equal(t, "", profileValueForLabel("years of experience", basicProfile()))
}
