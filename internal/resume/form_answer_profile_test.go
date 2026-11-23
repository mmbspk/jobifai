package resume_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/resume"
)

func TestFormAnswer_ProviderReceivesReadableCurrentProfile(t *testing.T) {
	var calls atomic.Int32
	prompts := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		prompts <- request.Messages[0].Content
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []map[string]string{{"type": "text", "text": "1 month"}}, "usage": map[string]int{"input_tokens": 50, "output_tokens": 10}})
	}))
	defer server.Close()
	client := llm.New(domain.LLMConfig{Provider: "claude", Model: "mock", UseProxy: true, ProxyURL: server.URL}, "fixture")
	tailor := resume.NewTailor(client, client, client, client)
	profile := []byte(`{"personal_information":{"name":"Amina Sample"},"application_defaults":{"notice_period":"1 month"}}`)
	answer, err := tailor.AnswerFormQuestion(context.Background(), profile, "How soon can you leave?", nil)
	require.NoError(t, err)
	assert.Equal(t, "1 month", answer)
	prompt := <-prompts
	assert.Contains(t, prompt, `"name":"Amina Sample"`)
	assert.Contains(t, prompt, `"notice_period":"1 month"`)
	_, err = tailor.AnswerFormQuestion(context.Background(), []byte("invalid JSON"), "How soon can you leave?", nil)
	require.Error(t, err)
	assert.Equal(t, int32(1), calls.Load(), "invalid profile data must not reach the provider")
}

func TestFormAnswerPrompt_LocationRespectsKnownAndUnknownRelocation(t *testing.T) {
	no, yes := false, true
	for _, tc := range []struct {
		name       string
		relocation *domain.ApplicationRelocation
		want       string
	}{
		{"not supplied", nil, `"city":"Adelaide"`},
		{"unknown", &domain.ApplicationRelocation{Willing: nil, Details: "Local roles preferred"}, `"details":"Local roles preferred"`},
		{"declined", &domain.ApplicationRelocation{Willing: &no}, `"willing":false`},
		{"conditional", &domain.ApplicationRelocation{Willing: &yes, Locations: []string{"Perth"}, Details: "Only after a confirmed offer"}, `"details":"Only after a confirmed offer"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := domain.ResumeProfile{
				PersonalInformation: domain.PersonalInformation{City: "Adelaide", Country: "Australia"},
				ApplicationDefaults: domain.ApplicationDefaults{Relocation: tc.relocation},
			}
			raw, err := json.Marshal(profile)
			require.NoError(t, err)
			prompt := resume.BuildFormAnswerPrompt(raw, "Can you relocate to Perth?", []string{"Yes", "No"})
			require.Contains(t, prompt, tc.want)
			require.Contains(t, prompt, "When relocation.willing is false, respect that answer and its conditions")
			require.Contains(t, prompt, "when it is absent or null, willingness is unknown")
			require.Contains(t, prompt, "even when options are supplied")
			require.NotContains(t, prompt, "happy to relocate for the right opportunity")
			require.NotContains(t, prompt, "Never say you cannot or are unable to work there")
		})
	}
}

func TestFormAnswerPrompt_ExpandedRecruiterFactsKeepQualifiersAndMissingFacts(t *testing.T) {
	minimum, maximum, excluded := 70000.0, 90000.0, false
	profile := domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{
		Availability: "Weekdays after a planned commitment", EarliestStartDate: "2027-01-04",
		SalaryRange:   &domain.ApplicationSalaryRange{Minimum: &minimum, Maximum: &maximum, Currency: "AUD", PayPeriod: "year", Basis: "base", IncludesBenefits: &excluded, BenefitsDetails: "superannuation"},
		WorkRights:    []domain.ApplicationWorkRight{{Country: "Australia", Status: "Permanent resident", WorkRestrictions: "Use the supplied status without adding visa details"}},
		CustomAnswers: []domain.ApplicationAnswer{{Question: "Can you travel?", Answer: "One trip per month, with notice", Country: "Australia"}},
	}}
	raw, err := json.Marshal(profile)
	require.NoError(t, err)
	prompt := resume.BuildFormAnswerPrompt(raw, "What are your availability and salary expectations?", nil)
	// The provider receives the actual new fields and their qualifiers, not just
	// the legacy notice/salary/sponsorship subset.
	require.Contains(t, prompt, string(raw))
	require.Contains(t, prompt, "Retain supplied conditions and qualifiers")
	require.Contains(t, prompt, "Do not convert currencies or pay periods, select an arbitrary amount from a range")
	require.Contains(t, prompt, "Residence or a sponsorship answer does not establish legal work eligibility")
	require.Contains(t, prompt, "An unspecified sponsorship answer remains unknown")
	require.Contains(t, prompt, "Count only dated experience explicitly relevant")
	require.NotContains(t, prompt, `"requires_sponsorship":false`)
	require.NotContains(t, prompt, `"visa_expiry_date"`)
}
