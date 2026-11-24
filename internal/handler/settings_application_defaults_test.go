package handler_test

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"net/http"
	"testing"
)

func TestSettings_ApplicationDefaultsRoundTripAndInvalidSavePreservesProfile(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "application-facts@example.com", "password123")
	no := false
	minimum := 70000.0
	maximum := 90000.0
	profile := domain.ResumeProfile{PersonalInformation: domain.PersonalInformation{Name: "Candidate", Email: "candidate@example.com"}, Skills: []string{"Client service"}, PromptInstructions: "Keep my work examples concise", ApplicationDefaults: domain.ApplicationDefaults{
		NoticePeriod: "4 weeks", SalaryExpectation: "Negotiable", RequiresSponsorship: &no, FormAnswers: map[string]string{"reviewed": "Existing answer"}, Availability: "Weekdays", EarliestStartDate: "2027-01-04",
		SalaryRange: &domain.ApplicationSalaryRange{Minimum: &minimum, Maximum: &maximum, Currency: "AUD", PayPeriod: "year", Basis: "base", IncludesBenefits: &no},
		WorkRights:  []domain.ApplicationWorkRight{{Country: "Australia", Status: "Permanent resident", RequiresSponsorship: &no}, {Country: "New Zealand", Details: "Not specified yet"}},
		Relocation:  &domain.ApplicationRelocation{Willing: &no, Details: "Local roles preferred"}, CustomAnswers: []domain.ApplicationAnswer{{Question: "Can you travel?", Answer: "Occasionally", Country: "Australia"}},
	}}
	response := authPost(t, router, "/api/settings/resume", token, profile)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	read := func() domain.ResumeProfile {
		response := authGet(t, router, "/api/settings/resume", token)
		require.Equal(t, http.StatusOK, response.Code)
		var got domain.ResumeProfile
		require.NoError(t, json.NewDecoder(response.Body).Decode(&got))
		return got
	}
	require.Equal(t, profile, read())
	invalid := profile
	invalid.ApplicationDefaults.CustomAnswers = []domain.ApplicationAnswer{{Question: "Travel?"}}
	response = authPost(t, router, "/api/settings/resume", token, invalid)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "both a question and an answer")
	require.Equal(t, profile, read())
}
