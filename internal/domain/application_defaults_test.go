package domain

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestApplicationDefaults_SponsorshipPresenceSurvivesRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, payload  string
		present, value bool
	}{
		{"not provided", `{}`, false, false},
		{"explicit no", `{"requires_sponsorship":false}`, true, false},
		{"explicit yes", `{"requires_sponsorship":true}`, true, true},
		{"cleared", `{"requires_sponsorship":null}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var defaults ApplicationDefaults
			require.NoError(t, json.Unmarshal([]byte(tc.payload), &defaults))
			encoded, err := json.Marshal(defaults)
			require.NoError(t, err)
			var fields map[string]any
			require.NoError(t, json.Unmarshal(encoded, &fields))
			_, present := fields["requires_sponsorship"]
			require.Equal(t, tc.present, present)
			var reloaded ApplicationDefaults
			require.NoError(t, json.Unmarshal(encoded, &reloaded))
			if tc.present {
				require.NotNil(t, reloaded.RequiresSponsorship)
				require.Equal(t, tc.value, *reloaded.RequiresSponsorship)
			} else {
				require.Nil(t, reloaded.RequiresSponsorship)
			}
		})
	}
}

func TestApplicationDefaults_ExpandedFactsRoundTrip(t *testing.T) {
	payload := []byte(`{"personal_information":{"name":"Candidate","email":"candidate@example.com"},"skills":["Client service"],"application_defaults":{"notice_period":"4 weeks","salary_expectation":"Negotiable","requires_sponsorship":false,"form_answers":{"reviewed":"Existing answer"},"availability":"Weekdays","earliest_start_date":"2027-01-04","salary_range":{"minimum":70000,"maximum":90000,"currency":"AUD","pay_period":"year","basis":"base","includes_benefits":false,"benefits_details":"superannuation"},"work_rights":[{"country":"Australia","status":"Permanent resident","requires_sponsorship":false},{"country":"New Zealand","details":"Please ask me about this country"}],"relocation":{"willing":false,"locations":["Adelaide"],"details":"Local roles preferred"},"additional_details":"Available for occasional travel","custom_answers":[{"question":"Can you travel?","answer":"Occasionally","country":"Australia"}]}}`)
	var profile ResumeProfile
	require.NoError(t, json.Unmarshal(payload, &profile))
	require.NoError(t, profile.ApplicationDefaults.Validate())
	require.NotNil(t, profile.ApplicationDefaults.RequiresSponsorship)
	require.False(t, *profile.ApplicationDefaults.RequiresSponsorship)
	require.Nil(t, profile.ApplicationDefaults.WorkRights[1].RequiresSponsorship)
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			var encoded []byte
			var err error
			var reloaded ResumeProfile
			if format == "json" {
				encoded, err = json.Marshal(profile)
				require.NoError(t, err)
				err = json.Unmarshal(encoded, &reloaded)
			} else {
				encoded, err = yaml.Marshal(profile)
				require.NoError(t, err)
				err = yaml.Unmarshal(encoded, &reloaded)
			}
			require.NoError(t, err)
			require.Equal(t, profile, reloaded)
		})
	}
}

func TestApplicationDefaults_ValidationRejectsMalformedFacts(t *testing.T) {
	number := func(n float64) *float64 { return &n }
	for _, tc := range []struct {
		name    string
		value   ApplicationDefaults
		message string
	}{
		{"negative amount", ApplicationDefaults{SalaryRange: &ApplicationSalaryRange{Minimum: number(-1)}}, "finite numbers"},
		{"infinite amount", ApplicationDefaults{SalaryRange: &ApplicationSalaryRange{Minimum: number(math.Inf(1))}}, "finite numbers"},
		{"not a number", ApplicationDefaults{SalaryRange: &ApplicationSalaryRange{Minimum: number(math.NaN())}}, "finite numbers"},
		{"inverted range", ApplicationDefaults{SalaryRange: &ApplicationSalaryRange{Minimum: number(2), Maximum: number(1)}}, "minimum must not exceed"},
		{"missing currency", ApplicationDefaults{SalaryRange: &ApplicationSalaryRange{Minimum: number(1), PayPeriod: "year", Basis: "base"}}, "currency"},
		{"missing period", ApplicationDefaults{SalaryRange: &ApplicationSalaryRange{Minimum: number(1), Currency: "AUD", Basis: "base"}}, "pay period"},
		{"missing basis", ApplicationDefaults{SalaryRange: &ApplicationSalaryRange{Minimum: number(1), Currency: "AUD", PayPeriod: "year"}}, "base pay"},
		{"missing country", ApplicationDefaults{WorkRights: []ApplicationWorkRight{{Status: "Citizen"}}}, "requires a country"},
		{"unfinished custom answer", ApplicationDefaults{CustomAnswers: []ApplicationAnswer{{Question: "Travel?"}}}, "both a question and an answer"},
		{"unfinished custom question", ApplicationDefaults{CustomAnswers: []ApplicationAnswer{{Answer: "Yes"}}}, "both a question and an answer"},
		{"invalid start date", ApplicationDefaults{EarliestStartDate: "2026-02-30"}, "valid YYYY-MM-DD"},
		{"invalid visa date", ApplicationDefaults{WorkRights: []ApplicationWorkRight{{Country: "Australia", VisaExpiryDate: "next year"}}}, "valid YYYY-MM-DD"},
	} {
		t.Run(tc.name, func(t *testing.T) { require.ErrorContains(t, tc.value.Validate(), tc.message) })
	}
	require.NoError(t, (ApplicationDefaults{}).Validate())
	require.NoError(t, (ApplicationDefaults{NoticePeriod: "Immediately available", SalaryExpectation: "Negotiable"}).Validate())
}
