package resume

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
)

func TestCVEvidence_ExplicitCitizenshipAndPermanentResidency(t *testing.T) {
	tests := []struct{ statement, country, canonical string }{
		{"Australian permanent resident", "Australia", "Australia"},
		{"Australian citizen", "Australian", "Australia"},
		{"I am a citizen of Australia.", "AU", "Australia"},
		{"Permanent resident of Germany", "DE", "Germany"},
		{"British citizen", "UK", "United Kingdom"},
		{"U.S. citizen", "United States", "United States"},
		{"Citizen of the USA", "US", "United States"},
		{"Canadian citizen", "Canada", "Canada"},
		{"New Zealand citizen", "NZ", "New Zealand"},
	}
	for _, test := range tests {
		t.Run(test.statement, func(t *testing.T) {
			var defaults domain.ApplicationDefaults
			value := []map[string]any{{"country": test.country, "status": test.statement}}
			got := validateCVAnswers(test.statement, []json.RawMessage{cvTestItem(t, "work_rights", "work_rights", value, test.statement)}, &defaults)
			require.Len(t, got, 1)
			require.False(t, got[0].NeedsConfirmation)
			require.Equal(t, "cv_evidence", got[0].Source)
			require.Len(t, defaults.WorkRights, 1)
			require.Equal(t, test.canonical, defaults.WorkRights[0].Country)
			require.Equal(t, test.statement, defaults.WorkRights[0].Status)
			require.Equal(t, test.statement, defaults.WorkRights[0].Details)
			require.Nil(t, defaults.RequiresSponsorship)
			require.Nil(t, defaults.WorkRights[0].RequiresSponsorship)
		})
	}
}

func TestCVEvidence_CitizenshipRequiresTheCandidatesExactScopedClaim(t *testing.T) {
	tests := []struct{ statement, country, status string }{
		{"Australian permanent resident", "Germany", "Australian permanent resident"},
		{"Not an Australian citizen", "Australia", "Australian citizen"},
		{"Non-Australian citizen", "Australia", "Australian citizen"},
		{"Not currently an Australian permanent resident", "Australia", "Australian permanent resident"},
		{"Neither Australian nor German citizen", "Germany", "German citizen"},
		{"Australian resident", "Australia", "Australian resident"},
		{"Citizen in Australia", "Australia", "Citizen in Australia"},
		{"Nationality: Australian", "Australia", "Nationality: Australian"},
		{"Permanent resident", "Australia", "Permanent resident"},
		{"German citizen living in Australia", "Australia", "German citizen living in Australia"},
		{"Australian resident and German citizen", "Australia", "Australian resident and German citizen"},
		{"German permanent resident of Australia", "Germany", "German permanent resident"},
		{"German permanent resident of Australia", "Australia", "permanent resident of Australia"},
		{"My spouse is an Australian citizen", "Australia", "Australian citizen"},
		{"CA permanent resident", "Canada", "CA permanent resident"},
		{"European citizen", "Germany", "European citizen"},
	}
	for _, test := range tests {
		t.Run(test.statement+"/"+test.country, func(t *testing.T) {
			var defaults domain.ApplicationDefaults
			text := "Sydney, Australia. Worked in Germany.\n" + test.statement
			value := []map[string]any{{"country": test.country, "status": test.status}}
			got := validateCVAnswers(text, []json.RawMessage{cvTestItem(t, "work_rights", "work_rights", value, test.statement)}, &defaults)
			require.Empty(t, got)
			require.Empty(t, defaults.WorkRights)
		})
	}
}

func TestCVEvidence_DualCountryDeclarationsKeepTheirOwnStatus(t *testing.T) {
	text := "Australian citizen and German permanent resident"
	var defaults domain.ApplicationDefaults
	got := validateCVAnswers(text, []json.RawMessage{
		cvTestItem(t, "work_rights", "work_rights", []map[string]any{{"country": "Australia", "status": "Australian citizen"}}, text),
		cvTestItem(t, "work_rights", "work_rights", []map[string]any{{"country": "Germany", "status": "German permanent resident"}}, text),
	}, &defaults)
	require.Len(t, got, 2)
	require.False(t, got[0].NeedsConfirmation)
	require.False(t, got[1].NeedsConfirmation)
	require.Len(t, defaults.WorkRights, 2)
	require.Equal(t, "Australian citizen", defaults.WorkRights[0].Status)
	require.Equal(t, "German permanent resident", defaults.WorkRights[1].Status)
}

func TestCVEvidence_OtherCountryNegationDoesNotNegateExplicitCitizenship(t *testing.T) {
	text := "Australian citizen and not a German citizen"
	var defaults domain.ApplicationDefaults
	got := validateCVAnswers(text, []json.RawMessage{
		cvTestItem(t, "work_rights", "work_rights", []map[string]any{{"country": "Australia", "status": "Australian citizen"}}, text),
		cvTestItem(t, "work_rights", "work_rights", []map[string]any{{"country": "Germany", "status": "German citizen"}}, text),
	}, &defaults)
	require.Len(t, got, 1)
	require.False(t, got[0].NeedsConfirmation)
	require.Len(t, defaults.WorkRights, 1)
	require.Equal(t, "Australia", defaults.WorkRights[0].Country)
}

func TestCVEvidence_QualifiedCitizenshipRequiresConfirmation(t *testing.T) {
	tests := []struct{ statement, country, status string }{
		{"Not an Australian citizen", "Australia", "Not an Australian citizen"},
		{"May be an Australian permanent resident", "Australia", "Australian permanent resident"},
		{"Former Australian permanent resident", "Australia", "Australian permanent resident"},
		{"Australian or German citizen", "Germany", "German citizen"},
		{"Australian and German citizen", "Germany", "German citizen"},
	}
	for _, test := range tests {
		t.Run(test.statement, func(t *testing.T) {
			var defaults domain.ApplicationDefaults
			value := []map[string]any{{"country": test.country, "status": test.status}}
			got := validateCVAnswers(test.statement, []json.RawMessage{cvTestItem(t, "work_rights", "work_rights", value, test.statement)}, &defaults)
			require.Len(t, got, 1)
			require.True(t, got[0].NeedsConfirmation)
			require.Equal(t, "model_suggestion", got[0].Source)
			require.Empty(t, defaults.WorkRights)
		})
	}
}

func TestCVEvidence_CountryAliasesStillDetectConflictingDeclarations(t *testing.T) {
	text := "Australian citizen\nNot a citizen of Australia"
	var defaults domain.ApplicationDefaults
	value := []map[string]any{{"country": "Australia", "status": "Australian citizen"}}
	got := validateCVAnswers(text, []json.RawMessage{cvTestItem(t, "work_rights", "work_rights", value, "Australian citizen")}, &defaults)
	require.Len(t, got, 1)
	require.True(t, got[0].NeedsConfirmation)
	require.Empty(t, defaults.WorkRights)
}
