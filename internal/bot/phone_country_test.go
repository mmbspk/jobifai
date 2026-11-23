package bot

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func phoneCountryFixture() StepSnapshot {
	var options []formFieldOption
	for i := 0; i < 25; i++ {
		options = append(options, formFieldOption{Label: fmt.Sprintf("Unrelated choice %d", i), Value: fmt.Sprintf("other_%d", i)})
	}
	options = append(options, formFieldOption{Label: "United States of America (+1)", Value: "USA_1"}, formFieldOption{Label: "Canada (+1)", Value: "CAN_1"}, formFieldOption{Label: "Australia (+61)", Value: "AUS_61"})
	return captureStepSnapshot([]formField{{Type: "select", Question: "Phone country code*", Required: true, Options: options}, {Type: "text", Question: "Phone number", Required: true, InputType: "tel"}, {Type: "text", Question: "Middle name"}, {Type: "text", Question: "Postal code"}}, nil, "linkedin", 0, "Contact information")
}

func TestPhoneCountry_ValidatedNationalNumberUsesFullChoicesWithoutModel(t *testing.T) {
	profile := basicProfile()
	profile.PersonalInformation.Country = "Australia"
	profile.PersonalInformation.Phone = "0412 345 678"
	profile.PersonalInformation.PhonePrefix = ""
	profile.PersonalInformation.ZipCode = ""
	snap := phoneCountryFixture()
	require.True(t, snap.Fields[0].Truncated)
	require.NotContains(t, snap.Fields[0].Options, "Australia (+61)")
	delegate := &stubLLM{}
	result, err := (&StepInterpreter{LLM: delegate}).InterpretStep(context.Background(), snap, profile)
	require.NoError(t, err)
	require.False(t, result.Paused, "resolutions: %+v", result.Fields)
	require.Zero(t, delegate.calls)
	require.Equal(t, "Australia (+61)", resFor(t, result, "f0").Value)
	require.Equal(t, "412345678", resFor(t, result, "f1").Value)
	require.Equal(t, ActionSkip, resFor(t, result, "f2").Action)
	require.Equal(t, ActionSkip, resFor(t, result, "f3").Action)
	store := &DBMappingStore{DB: openMappingDB(t), UserID: "candidate"}
	require.NoError(t, store.RecordProposals(context.Background(), snap, result))
	var pending int
	require.NoError(t, store.DB.QueryRow("SELECT COUNT(*) FROM form_field_mappings WHERE provenance='pending'").Scan(&pending))
	require.Zero(t, pending, "optional missing data does not ask the candidate for review")
}

func TestPhoneCountry_SharedCallingCodeUsesActualNumberRegion(t *testing.T) {
	for _, tc := range []struct{ phone, country, choice, national string }{
		{"+1 415 555 2671", "Canada", "United States of America (+1)", "4155552671"},
		{"+1 416 555 2671", "United States", "Canada (+1)", "4165552671"},
	} {
		profile := basicProfile()
		profile.PersonalInformation.Phone, profile.PersonalInformation.Country = tc.phone, tc.country
		profile.PersonalInformation.PhonePrefix = ""
		result, err := (&StepInterpreter{}).InterpretStep(context.Background(), phoneCountryFixture(), profile)
		require.NoError(t, err)
		require.False(t, result.Paused)
		require.Equal(t, tc.choice, resFor(t, result, "f0").Value)
		require.Equal(t, tc.national, resFor(t, result, "f1").Value)
	}
}

func TestPhoneCountry_ISOOnlyChoicesUseValidatedRegionWithoutModel(t *testing.T) {
	for _, tc := range []struct{ phone, country, iso, label, national string }{
		{"0412 345 678", "Australia", "au", "Australia (+61)", "412345678"},
		{"+1 415 555 2671", "Canada", "us", "United States (+1)", "4155552671"},
		{"+1 416 555 2671", "United States", "ca", "Canada (+1)", "4165552671"},
	} {
		t.Run(tc.iso, func(t *testing.T) {
			profile := basicProfile()
			profile.PersonalInformation.Phone, profile.PersonalInformation.Country, profile.PersonalInformation.PhonePrefix = tc.phone, tc.country, ""
			options := []formFieldOption{{Value: "au", Label: "Australia (+61)"}, {Value: "us", Label: "United States (+1)"}, {Value: "ca", Label: "Canada (+1)"}}
			snap := captureStepSnapshot([]formField{{Type: "select", Question: "Phone country code", Required: true, Options: options}, {Type: "text", Question: "Phone number", Required: true, InputType: "tel"}}, nil, "linkedin", 0, "Contact information")
			delegate := &stubLLM{}
			result, err := (&StepInterpreter{LLM: delegate}).InterpretStep(context.Background(), snap, profile)
			require.NoError(t, err)
			require.False(t, result.Paused)
			require.Zero(t, delegate.calls)
			require.Equal(t, tc.label, resFor(t, result, "f0").Value)
			require.Equal(t, tc.national, resFor(t, result, "f1").Value)
		})
	}
}

func TestOptionPhoneRegion_ValidatesISOValuesAgainstVisibleCallingCode(t *testing.T) {
	for _, tc := range []struct {
		value, label, region string
		code                 int
		valid                bool
	}{
		{"au", "Australia (+61)", "AU", 61, true},
		{" AUS ", "Australia (+61)", "AU", 61, true},
		{"CAN", "Canada (+1)", "CA", 1, true},
		{"au", "Australia (+1)", "", 0, false},
		{"au", "Australia", "", 0, false},
		{"au", "Australia (+61) (+1)", "", 0, false},
		{"zz", "Unknown (+61)", "", 0, false},
		{"AUS_1", "Australia (+61)", "", 0, false},
	} {
		t.Run(tc.value+"/"+tc.label, func(t *testing.T) {
			region, code, valid := optionPhoneRegion(formFieldOption{Value: tc.value, Label: tc.label})
			require.Equal(t, tc.valid, valid)
			require.Equal(t, tc.region, region)
			require.Equal(t, tc.code, code)
		})
	}
}

func TestPhoneCountry_MobileLabelsUseNationalDigits(t *testing.T) {
	for _, label := range []string{"Mobile phone number*", "Mobile number", "Cell phone number", "Telephone number"} {
		t.Run(label, func(t *testing.T) {
			profile := basicProfile()
			profile.PersonalInformation.Country = "Australia"
			profile.PersonalInformation.Phone = "0412 345 678"
			profile.PersonalInformation.PhonePrefix = ""
			snap := phoneCountryFixture()
			snap.Fields[1].Label = label
			delegate := &stubLLM{}
			result, err := (&StepInterpreter{LLM: delegate}).InterpretStep(context.Background(), snap, profile)
			require.NoError(t, err)
			require.False(t, result.Paused)
			require.Zero(t, delegate.calls)
			require.Equal(t, "412345678", resFor(t, result, "f1").Value)
		})
	}
}

func TestPhoneCountry_AmbiguousOrContradictoryFactsHold(t *testing.T) {
	for _, name := range []string{"missing number", "invalid number", "conflicting prefix", "duplicate region choices", "misleading code label"} {
		t.Run(name, func(t *testing.T) {
			profile := basicProfile()
			profile.PersonalInformation.Phone, profile.PersonalInformation.Country, profile.PersonalInformation.PhonePrefix = "0412345678", "Australia", ""
			snap := phoneCountryFixture()
			switch name {
			case "missing number":
				profile.PersonalInformation.Phone = ""
			case "invalid number":
				profile.PersonalInformation.Phone = "123"
			case "conflicting prefix":
				profile.PersonalInformation.Phone = "+14155552671"
				profile.PersonalInformation.PhonePrefix = "+61"
			case "duplicate region choices":
				snap.Fields[0].BrowserField.Options = append(snap.Fields[0].BrowserField.Options, formFieldOption{Label: "Australia duplicate (+61)", Value: "AUS_61"})
			case "misleading code label":
				snap.Fields[0].BrowserField.Options[len(snap.Fields[0].BrowserField.Options)-1].Label = "Australia (+1)"
			}
			result, err := (&StepInterpreter{}).InterpretStep(context.Background(), snap, profile)
			require.NoError(t, err)
			require.True(t, result.Paused)
			require.Empty(t, resFor(t, result, "f0").Value)
		})
	}
}
