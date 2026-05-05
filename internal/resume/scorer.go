package resume

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

var scoreTempl = template.Must(template.New("score").Parse(`You are an expert recruiter evaluating how well a candidate's profile matches a job posting.

Score the match from 0 to 10 where:
  10 = perfect fit, nearly every requirement met, ideal background
   7 = strong fit, most requirements met, minor gaps
   5 = moderate fit, relevant background but notable gaps
   3 = weak fit, some overlap but significant misalignment
   0 = no fit, fundamentally wrong background or location

Evaluate across: required skills & tech stack, years of experience, seniority level,
domain/industry relevance, location/remote eligibility.

Return ONLY valid JSON, no markdown:
{"score": <int 0-10>, "reasoning": "<2-3 sentence explanation of key strengths and gaps>"}
{{- if .PromptInstructions}}

ADDITIONAL INSTRUCTIONS FROM CANDIDATE (follow these):
{{.PromptInstructions}}
{{- end}}

Candidate profile:
{{.Profile}}

Job description:
{{.JobDescription}}`))

// Scorer evaluates job-profile fit using an LLM.
type Scorer struct {
	client *llm.Client
}

func NewScorer(client *llm.Client) *Scorer { return &Scorer{client: client} }

// EvaluateJob scores how well the candidate's profile matches the given job description.
func (s *Scorer) EvaluateJob(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (domain.JobScore, error) {
	trimmed := ForScoring(profile)
	profileJSON, err := json.Marshal(trimmed)
	if err != nil {
		return domain.JobScore{}, fmt.Errorf("scorer: marshal: %w", err)
	}
	var prompt bytes.Buffer
	if err := scoreTempl.Execute(&prompt, promptData{
		Profile:            string(profileJSON),
		JobDescription:     jobDesc,
		PromptInstructions: profile.PromptInstructions,
	}); err != nil {
		return domain.JobScore{}, fmt.Errorf("scorer: template: %w", err)
	}
	raw, err := s.client.Chat(llm.WithTask(ctx, "evaluate job"), []llm.Message{{Role: "user", Content: prompt.String()}})
	if err != nil {
		return domain.JobScore{}, fmt.Errorf("scorer: llm: %w", err)
	}
	raw = stripJSON(raw)
	var result domain.JobScore
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return domain.JobScore{}, fmt.Errorf("scorer: parse: %w\nraw: %s", err, raw)
	}
	if result.Score < 0 {
		result.Score = 0
	}
	if result.Score > 10 {
		result.Score = 10
	}
	return result, nil
}
