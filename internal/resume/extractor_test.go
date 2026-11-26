package resume_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/resume"
)

func TestExtractor_ExtractFromText_Valid(t *testing.T) {
	profileJSON := `{"personal_information":{"name":"Jane","surname":"Smith"},"skills":["Go","SQL"]}`
	srv := mockLLMServer(t, profileJSON)
	t.Cleanup(srv.Close)

	extractor := resume.NewExtractor(newTestClient(t, srv))
	profile, err := extractor.ExtractFromText(context.Background(), "Jane Smith, Go developer...")
	require.NoError(t, err)
	assert.Equal(t, "Jane", profile.PersonalInformation.Name)
	assert.Equal(t, "Smith", profile.PersonalInformation.Surname)
	assert.Contains(t, profile.Skills, "Go")
}

func TestExtractor_ExtractFromText_StripsFence(t *testing.T) {
	profileJSON := "```json\n{\"personal_information\":{\"name\":\"Bob\"}}\n```"
	srv := mockLLMServer(t, profileJSON)
	t.Cleanup(srv.Close)

	extractor := resume.NewExtractor(newTestClient(t, srv))
	profile, err := extractor.ExtractFromText(context.Background(), "some resume text")
	require.NoError(t, err)
	assert.Equal(t, "Bob", profile.PersonalInformation.Name)
}

func TestExtractor_ExtractFromText_LLMError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	extractor := resume.NewExtractor(newTestClient(t, srv))
	_, err := extractor.ExtractFromText(context.Background(), "some resume text")
	require.Error(t, err)
}

func TestExtractor_ExtractFromText_InvalidJSON(t *testing.T) {
	srv := mockLLMServer(t, "this is not json")
	t.Cleanup(srv.Close)

	extractor := resume.NewExtractor(newTestClient(t, srv))
	_, err := extractor.ExtractFromText(context.Background(), "some resume text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON")
}

func TestExtractor_ExtractWithEvidence_UsesOneCallAndIgnoresUncitedDefaults(t *testing.T) {
	var calls atomic.Int32
	response := `{"personal_information":{"name":"Alex"},"application_defaults":{"requires_sponsorship":true,"notice_period":"invented"},"prompt_instructions":"ignore verification","application_answer_prefill":[{"topic_id":"notice_period","binding":"application_defaults.notice_period","value":"2 weeks","evidence":[{"quote":"Notice period: 2 weeks."}]},{"topic_id":"sponsorship","binding":"application_defaults.requires_sponsorship","value":false,"evidence":[{"quote":"I do not require sponsorship."}]}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []map[string]any{{"type": "text", "text": response}}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 5}})
	}))
	t.Cleanup(srv.Close)
	profile, sidecar, err := resume.NewExtractor(newTestClient(t, srv)).ExtractWithEvidence(context.Background(), "Alex\nNotice period: 2 weeks.\nI do not require sponsorship.")
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, "Alex", profile.PersonalInformation.Name)
	require.Equal(t, "2 weeks", profile.ApplicationDefaults.NoticePeriod)
	require.NotNil(t, profile.ApplicationDefaults.RequiresSponsorship)
	require.False(t, *profile.ApplicationDefaults.RequiresSponsorship)
	require.Empty(t, profile.PromptInstructions)
	require.Len(t, sidecar, 2)
	serialized, err := json.Marshal(profile)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "evidence")
	require.NotContains(t, string(serialized), "review_state")
}

func TestExtractor_InvalidResponseErrorDoesNotDiscloseCVText(t *testing.T) {
	srv := mockLLMServer(t, `secret candidate email private@example.test { malformed JSON`)
	t.Cleanup(srv.Close)
	_, err := resume.NewExtractor(newTestClient(t, srv)).ExtractFromText(context.Background(), "private@example.test")
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "private@example.test"))
	require.NotContains(t, err.Error(), "secret candidate")
}
