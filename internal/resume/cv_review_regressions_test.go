package resume

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"testing"
)

func TestApplyCVValueMalformedFactsPreserveExistingValues(t *testing.T) {
	for _, binding := range []string{"salary_range", "relocation", "work_rights"} {
		for _, raw := range []string{"null", " {broken", "[]", "false", `{"minimum":"oops","maximum":42}`, "{}"} {
			t.Run(binding+raw, func(t *testing.T) {
				defaults := domain.ApplicationDefaults{SalaryRange: &domain.ApplicationSalaryRange{Details: "Keep salary"}, Relocation: &domain.ApplicationRelocation{Details: "Keep relocation"}, WorkRights: []domain.ApplicationWorkRight{{Country: "Australia", Status: "Permanent resident"}}}
				before, _ := json.Marshal(defaults)
				item := domain.CVAnswerPrefill{Binding: cvBindingPrefix + binding, Value: json.RawMessage(raw)}
				require.NotPanics(t, func() { applyCVValue(&defaults, item) })
				after, _ := json.Marshal(defaults)
				require.JSONEq(t, string(before), string(after))
				if raw != "{}" {
					item.Evidence = []domain.CVAnswerEvidence{{Quote: "Evidence"}}
					require.NotPanics(t, func() { applyCVValue(&defaults, item) })
					after, _ = json.Marshal(defaults)
					require.JSONEq(t, string(before), string(after))
				}
			})
		}
	}
}
func TestCVEvidenceCurrentRightsAndUnrelatedPerformanceRates(t *testing.T) {
	quote := "Authorized to work in the UK currently."
	var defaults domain.ApplicationDefaults
	items := validateCVAnswers(quote, []json.RawMessage{cvTestItem(t, "work_rights", "work_rights", []map[string]string{{"country": "United Kingdom", "status": quote}}, quote)}, &defaults)
	require.Len(t, items, 1)
	require.False(t, items[0].NeedsConfirmation)
	require.Len(t, defaults.WorkRights, 1)
	salary := "Expected salary AUD 150000 per year excluding superannuation."
	defaults = domain.ApplicationDefaults{}
	items = validateCVAnswers(salary+"\nImproved deployment success rate by 25%.\nIncreased approval rate.", []json.RawMessage{cvTestItem(t, "salary", "salary_expectation", salary, salary)}, &defaults)
	require.Len(t, items, 1)
	require.False(t, items[0].NeedsConfirmation)
	require.Equal(t, salary, defaults.SalaryExpectation)
}
func TestCVShortCountryAliasesRejectHyphenatedDescriptors(t *testing.T) {
	for _, pair := range [][2]string{{"US-based clients", "United States"}, {"UK-based employer", "United Kingdom"}, {"NZ-based team", "New Zealand"}} {
		require.False(t, cvCountryIn(pair[0], pair[1]))
	}
	require.True(t, cvCountryIn("Australian permanent-resident", "Australia"))
	require.True(t, cvCountryIn("Authorized to work in the UK.", "United Kingdom"))
}

func TestCVEvidenceUnquotedHourlyRateStillPreventsSalaryHydration(t *testing.T) {
	quote := "Expected salary AUD 150000 per year excluding superannuation."
	var defaults domain.ApplicationDefaults
	items := validateCVAnswers(quote+"\nRate 150 per hour.", []json.RawMessage{cvTestItem(t, "salary", "salary_expectation", quote, quote)}, &defaults)
	require.Len(t, items, 1)
	require.True(t, items[0].NeedsConfirmation)
	require.Empty(t, defaults.SalaryExpectation)
}
