package resume_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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
