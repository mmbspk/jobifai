package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
	"github.com/user/jobifai/internal/resume"
	"github.com/user/jobifai/internal/testutil/mockllm"
)

func newResumeRouter(t *testing.T) (http.Handler, *handler.Services, string) {
	t.Helper()
	svc, _ := newTestServices(t)
	llm, eval, halal, answerer := stubLLMFactories()
	svc.Renderer = stubRenderer{}
	svc.LLMFactory = llm
	svc.EvaluatorFactory = eval
	svc.HalalCheckerFactory = halal
	svc.QuestionAnswererFactory = answerer
	svc.FileToText = noopFileToText
	svc.FetchJobPage = func(_ context.Context, url string) (string, error) {
		return "Job description for " + url, nil
	}
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, t.Name()+"@example.com", "password123")
	return router, svc, token
}

func TestResume_Generate_NoRenderer_Returns422(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "resume-norender@example.com", "password123")
	saveResumeProfile(t, router, token, sampleResumeProfile())

	w := authPostMultipart(t, router, "/api/resume/generate", token, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestResume_Generate_NoProfile_Returns422(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.Renderer = stubRenderer{}
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "resume-noprofile@example.com", "password123")

	w := authPostMultipart(t, router, "/api/resume/generate", token, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestResume_Generate_ReturnsPDF(t *testing.T) {
	router, _, token := newResumeRouter(t)
	saveResumeProfile(t, router, token, sampleResumeProfile())

	w := authPostMultipart(t, router, "/api/resume/generate", token, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/pdf", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "%PDF-resume-stub")
}

func TestResume_Evaluate_ReturnsScore(t *testing.T) {
	router, _, token := newResumeRouter(t)
	saveResumeProfile(t, router, token, sampleResumeProfile())

	w := authPostMultipart(t, router, "/api/resume/evaluate", token, map[string]string{
		"job_description": "Looking for an operations coordinator.",
		"skip_url_fetch":  "true",
	})
	assert.Equal(t, http.StatusOK, w.Code)

	var score domain.JobScore
	require.NoError(t, json.NewDecoder(w.Body).Decode(&score))
	assert.Equal(t, 8, score.Score)
	assert.NotEmpty(t, score.Reasoning)
}

func TestResume_Evaluate_NoLLM_Returns422(t *testing.T) {
	svc, _ := newTestServices(t)
	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, "resume-no-llm@example.com", "password123")
	saveResumeProfile(t, router, token, sampleResumeProfile())

	w := authPostMultipart(t, router, "/api/resume/evaluate", token, map[string]string{
		"job_description": "Role details",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestResume_CheckHalal_ReturnsVerdict(t *testing.T) {
	router, _, token := newResumeRouter(t)

	w := authPostMultipart(t, router, "/api/resume/check-halal", token, map[string]string{
		"job_description": "Ethical retail role.",
		"title":           "Store Manager",
		"company":         "Halal Foods",
		"skip_url_fetch":  "true",
	})
	assert.Equal(t, http.StatusOK, w.Code)

	var verdict domain.HalalVerdict
	require.NoError(t, json.NewDecoder(w.Body).Decode(&verdict))
	assert.Equal(t, "HALAL", verdict.Verdict)
}

func TestResume_AnswerQuestions_ReturnsAnswers(t *testing.T) {
	router, _, token := newResumeRouter(t)
	saveResumeProfile(t, router, token, sampleResumeProfile())

	body := map[string]any{
		"job_description": "Operations role.",
		"questions":       []string{"Why this role?", "Availability?"},
	}
	w := authPost(t, router, "/api/resume/answer-questions", token, body)
	assert.Equal(t, http.StatusOK, w.Code)

	var answers []domain.QuestionAnswer
	require.NoError(t, json.NewDecoder(w.Body).Decode(&answers))
	require.Len(t, answers, 2)
	assert.Equal(t, "Why this role?", answers[0].Question)
}

func newResumeRouterWithMockLLM(t *testing.T, llmResponse string) (http.Handler, *handler.Services, string) {
	t.Helper()
	srv := mockllm.ClaudeServer(t, llmResponse)
	t.Cleanup(srv.Close)

	svc, _ := newTestServices(t)
	svc.Renderer = stubRenderer{}
	svc.FetchJobPage = func(_ context.Context, url string) (string, error) {
		return "Job listing text from " + url, nil
	}
	svc.EvaluatorFactory = func(uid string) handler.JobEvaluator {
		if !svc.Secrets.Has(uid, "llm_api_key") {
			return nil
		}
		return resume.NewScorer(mockllm.NewClaudeClient(t, srv))
	}
	svc.LLMFactory = func(uid string) (handler.ResumeExtractor, handler.ResumeTailor) {
		if !svc.Secrets.Has(uid, "llm_api_key") {
			return nil, nil
		}
		client := mockllm.NewClaudeClient(t, srv)
		tailor := resume.NewTailor(client, client, client)
		return resume.NewExtractor(client), tailor
	}

	router := handler.NewRouter(svc)
	token := registerAndLogin(t, router, t.Name()+"@example.com", "password123")
	wireMockLLMUser(t, svc, userIDFromToken(t, router, token), srv.URL)
	saveResumeProfile(t, router, token, sampleResumeProfile())

	return handler.NewRouter(svc), svc, token
}

func TestResume_Evaluate_MockLLMHTTP_ProductionFactoryShape(t *testing.T) {
	scoreBody := `{"score": 7, "reasoning": "Good overlap with listed skills."}`
	router, _, token := newResumeRouterWithMockLLM(t, scoreBody)

	w := authPostMultipart(t, router, "/api/resume/evaluate", token, map[string]string{
		"job_description": "Role requires planning and client communication.",
		"skip_url_fetch":  "true",
	})
	require.Equal(t, http.StatusOK, w.Code)

	var score domain.JobScore
	require.NoError(t, json.NewDecoder(w.Body).Decode(&score))
	assert.Equal(t, 7, score.Score)
	assert.Contains(t, score.Reasoning, "overlap")
}

func TestResume_GenerateTailored_MockLLMAndFetchJobPage(t *testing.T) {
	profileJSON := `{"personal_information":{"name":"Sam"},"summary":"Experienced professional"}`
	router, _, token := newResumeRouterWithMockLLM(t, profileJSON)

	w := authPostMultipart(t, router, "/api/resume/generate-tailored", token, map[string]string{
		"job_url": "https://example.com/jobs/123",
	})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/pdf", w.Header().Get("Content-Type"))
}

func TestResume_Generate_RequiresAuth(t *testing.T) {
	svc, _ := newTestServices(t)
	svc.Renderer = stubRenderer{}
	router := handler.NewRouter(svc)

	w := authPostMultipart(t, router, "/api/resume/generate", "", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
