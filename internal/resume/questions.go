package resume

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

const questionsSystemPrompt = `You are helping a candidate write concise, human-sounding answers to job application questions.

Rules:
- Answer each question in 3–6 lines maximum. Be direct and specific.
- Write in first person, plain conversational English. No corporate or AI filler.
- Draw only on real details from the candidate's profile — do not invent facts.
- No bullet points, no dashes (—), no section dividers, no markdown formatting.
- No openers like "Great question", "Certainly", "Absolutely", "I would say".
- No closers like "I look forward to", "I am confident that", "this opportunity".
- Sound like a person writing a thoughtful but brief response, not an AI essay.

Return ONLY a valid JSON array, no markdown fences:
[{"question":"...","answer":"..."},...]`

// stripJSONArray extracts a JSON array from a raw LLM response,
// stripping markdown fences and any prose outside the [...] brackets.
func stripJSONArray(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimFunc(s, unicode.IsSpace)
	if start := strings.Index(s, "["); start >= 0 {
		if end := strings.LastIndex(s, "]"); end > start {
			return s[start : end+1]
		}
	}
	return s
}

// QuestionAnswerer answers a list of application/interview questions in one LLM call.
type QuestionAnswerer struct {
	client *llm.Client
}

func NewQuestionAnswerer(client *llm.Client) *QuestionAnswerer {
	return &QuestionAnswerer{client: client}
}

// AnswerQuestions sends all questions in a single LLM call and returns individual answers.
func (qa *QuestionAnswerer) AnswerQuestions(ctx context.Context, profile *domain.ResumeProfile, jobContext string, questions []string) ([]domain.QuestionAnswer, error) {
	trimmed := ForScoring(profile)
	profileJSON, err := json.Marshal(trimmed)
	if err != nil {
		return nil, fmt.Errorf("questions: marshal profile: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("## Candidate Profile\n")
	sb.Write(profileJSON)
	sb.WriteString("\n\n## Job Details\n")
	sb.WriteString(jobContext)
	if profile.PromptInstructions != "" {
		sb.WriteString("\n\nADDITIONAL INSTRUCTIONS FROM CANDIDATE (follow these):\n")
		sb.WriteString(profile.PromptInstructions)
	}
	sb.WriteString("\n\n## Questions\n")
	for i, q := range questions {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, strings.TrimSpace(q))
	}

	msgs := []llm.Message{
		{Role: "system", Content: questionsSystemPrompt},
		{Role: "user", Content: sb.String()},
	}
	raw, err := qa.client.Chat(llm.WithTask(ctx, "answer questions"), msgs)
	if err != nil {
		return nil, fmt.Errorf("questions: llm: %w", err)
	}
	raw = stripJSONArray(raw)

	var answers []domain.QuestionAnswer
	if err := json.Unmarshal([]byte(raw), &answers); err != nil {
		return nil, fmt.Errorf("questions: parse response: %w\nraw: %s", err, raw)
	}
	return answers, nil
}
