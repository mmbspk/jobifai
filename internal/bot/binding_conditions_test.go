package bot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBindingConditions_ChangedCurrencyAndPeriodPauseWithoutModel(t *testing.T) {
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "currency-test"}
	snap := makeSnap(textField("f0", "Expected annual salary in AUD"))
	snap.EmployerKey = "issuer"
	original := basicProfile()
	original.ApplicationDefaults.SalaryExpectation = "90000 AUD annually"
	require.NoError(t, reviewFixtureMappings(t, store, snap, []FieldResolution{{Ref: "f0", Source: SourceLLM, Action: ActionFill, IntentType: FieldIntentProfile, BindingKey: "application_defaults.salary_expectation"}}, original))
	delegate := &stubLLM{}
	for _, salary := range []string{"80000 EUR annually", "8000 AUD per month"} {
		current := basicProfile()
		current.ApplicationDefaults.SalaryExpectation = salary
		result, err := (&StepInterpreter{Mapping: store, LLM: delegate}).InterpretStep(context.Background(), snap, current)
		require.NoError(t, err)
		require.True(t, result.Paused)
		assert.Empty(t, result.Fields[0].Value)
	}
	assert.Zero(t, delegate.calls)
	current := basicProfile()
	current.ApplicationDefaults.SalaryExpectation = "95000 AUD annually"
	result, err := (&StepInterpreter{Mapping: store, LLM: delegate}).InterpretStep(context.Background(), snap, current)
	require.NoError(t, err)
	require.False(t, result.Paused)
	assert.Equal(t, "95000 AUD annually", result.Fields[0].Value)
}

func TestBindingConditions_ReviewRejectsIncompatibleControls(t *testing.T) {
	assert.False(t, bindingValueCompatible(SnapField{Type: SnapTypeNumber}, "", "NaN"))
	assert.False(t, bindingValueCompatible(SnapField{Type: SnapTypeNumber}, "", "seven years"))
	assert.True(t, bindingValueCompatible(SnapField{Type: SnapTypeNumber}, "", "7"))
	assert.False(t, bindingValueCompatible(SnapField{Type: SnapTypeText, MaxChars: 2}, "", "äöü"))
	assert.False(t, bindingValueCompatible(SnapField{Type: SnapTypeCheckbox, Options: []string{"English", "Arabic"}}, "", "French"))
}

func TestBindingConditions_DeterministicValuesRespectControlLimits(t *testing.T) {
	profile := basicProfile()
	profile.PersonalInformation.Name = "Long candidate name"
	profile.ApplicationDefaults.SalaryExpectation = "90000 AUD annually"
	for _, field := range []SnapField{
		{Ref: "f0", Label: "First name", Type: SnapTypeText, MaxChars: 3},
		{Ref: "f0", Label: "Salary", Type: SnapTypeNumber},
		{Ref: "f0", Label: "Salary", Type: SnapTypeText, HelpText: "Annual amount in EUR"},
	} {
		t.Run(field.Label+string(field.Type)+field.HelpText, func(t *testing.T) {
			delegate := &stubLLM{}
			result, err := (&StepInterpreter{LLM: delegate}).InterpretStep(context.Background(), makeSnap(field), profile)
			require.NoError(t, err)
			require.True(t, result.Paused)
			assert.Empty(t, result.Fields[0].Value)
			assert.Zero(t, delegate.calls)
		})
	}
}

func TestBindingConditions_NativeNumberBoundsAndStep(t *testing.T) {
	field := SnapField{Type: SnapTypeNumber, InputType: "number", MinValue: "1", MaxValue: "7", StepValue: "2"}
	for _, value := range []string{"1", "3", "7"} {
		assert.True(t, bindingValueCompatible(field, "", value))
	}
	for _, value := range []string{"0", "2", "8", "3.5"} {
		assert.False(t, bindingValueCompatible(field, "", value))
	}
	field.StepValue = "any"
	assert.True(t, bindingValueCompatible(field, "", "3.5"))
	field.StepValue = ""
	assert.False(t, bindingValueCompatible(field, "", "3.5"))
}

func TestBindingConditions_RoleQuestionRequiresReviewOfNamedProfileAnswer(t *testing.T) {
	profile := basicProfile()
	profile.ApplicationDefaults.FormAnswers = map[string]string{"interest": "I want a different kind of role"}
	delegate := &stubLLM{proposals: []LLMFieldProposal{{Ref: "f0", Action: "fill", IntentType: FieldIntentProfile, BindingKey: "application_defaults.form_answers.interest"}}}
	result, err := (&StepInterpreter{LLM: delegate}).InterpretStep(context.Background(), makeSnap(textField("f0", "Why are you interested in this role?")), profile)
	require.NoError(t, err)
	assert.True(t, result.Paused)
	assert.Empty(t, result.Fields[0].Value)
}
