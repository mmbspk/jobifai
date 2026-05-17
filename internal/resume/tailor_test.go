package resume_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/resume"
)

func TestTailor_TailorProfile_Valid(t *testing.T) {
	profileJSON := `{"personal_information":{"name":"Sam"},"skills":["Python","Django"]}`
	srv := mockLLMServer(t, profileJSON)
	t.Cleanup(srv.Close)

	client := newTestClient(t, srv)
	tailor := resume.NewTailor(client, client, client)

	profile := &domain.ResumeProfile{
		PersonalInformation: domain.PersonalInformation{Name: "Sam"},
	}
	out, err := tailor.TailorProfile(context.Background(), profile, "Django developer role")
	require.NoError(t, err)
	assert.Equal(t, "Sam", out.PersonalInformation.Name)
	assert.Contains(t, out.Skills, "Python")
}

func TestTailor_TailorProfile_LLMError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	client := newTestClient(t, srv)
	tailor := resume.NewTailor(client, client, client)
	_, err := tailor.TailorProfile(context.Background(), &domain.ResumeProfile{}, "job desc")
	require.Error(t, err)
}

func TestTailor_WriteCoverLetter_Valid(t *testing.T) {
	letterText := "Dear Hiring Manager, I am an excellent candidate..."
	srv := mockLLMServer(t, letterText)
	t.Cleanup(srv.Close)

	client := newTestClient(t, srv)
	tailor := resume.NewTailor(client, client, client)

	profile := &domain.ResumeProfile{Summary: "Experienced professional"}
	out, err := tailor.WriteCoverLetter(context.Background(), profile, "Senior role at Acme")
	require.NoError(t, err)
	assert.Contains(t, out, "Dear Hiring Manager")
}

func TestTailor_WriteCoverLetter_LLMError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "error", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	client := newTestClient(t, srv)
	tailor := resume.NewTailor(client, client, client)
	_, err := tailor.WriteCoverLetter(context.Background(), &domain.ResumeProfile{}, "job desc")
	require.Error(t, err)
}

func TestTailor_AnswerFormQuestion_Valid(t *testing.T) {
	srv := mockLLMServer(t, "Yes")
	t.Cleanup(srv.Close)

	client := newTestClient(t, srv)
	tailor := resume.NewTailor(client, client, client)

	profileJSON := []byte(`{"skills":["Python"]}`)
	answer, err := tailor.AnswerFormQuestion(context.Background(), profileJSON, "Do you know Python?", []string{"Yes", "No"})
	require.NoError(t, err)
	assert.Equal(t, "Yes", answer)
}

func TestTailor_AnswerFormQuestion_LLMError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "error", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	client := newTestClient(t, srv)
	tailor := resume.NewTailor(client, client, client)
	_, err := tailor.AnswerFormQuestion(context.Background(), []byte(`{}`), "question?", nil)
	require.Error(t, err)
}
