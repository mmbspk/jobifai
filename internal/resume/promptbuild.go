package resume

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

// promptbuild reuses unexported templates from tailor.go and questions.go in the same package.

// BuildJobScoringPrompt renders the production job-scoring user prompt.
func BuildJobScoringPrompt(profile *domain.ResumeProfile, jobDesc string) (string, error) {
	trimmed := ForScoring(profile)
	profileJSON, err := json.Marshal(trimmed)
	if err != nil {
		return "", fmt.Errorf("scoring prompt: marshal: %w", err)
	}
	var prompt bytes.Buffer
	if err := scoreTempl.Execute(&prompt, promptData{
		Profile:            string(profileJSON),
		JobDescription:     jobDesc,
		PromptInstructions: profile.PromptInstructions,
	}); err != nil {
		return "", err
	}
	return prompt.String(), nil
}

// BuildHalalPrompt renders the production employment-ethics user prompt.
func BuildHalalPrompt(title, company, description string) (string, error) {
	if len(description) > 2000 {
		description = description[:2000]
	}
	var prompt bytes.Buffer
	if err := halalTempl.Execute(&prompt, halalData{
		Title: title, Company: company, Description: description,
	}); err != nil {
		return "", err
	}
	return prompt.String(), nil
}

// BuildExtractPrompt returns the production resume extraction prompt including resume text.
func BuildExtractPrompt(resumeText string) string {
	return extractPrompt + resumeText
}

// BuildFormAnswerPrompt renders the production form-answer user prompt.
func BuildFormAnswerPrompt(profileJSON []byte, question string, options []string) string {
	optionsPart := "Provide a concise answer (1–2 sentences, plain text, no punctuation at the end)."
	if len(options) > 0 {
		optionsPart = "Available options, return EXACTLY one of these labels, nothing else: " + strings.Join(options, " | ")
	}
	return fmt.Sprintf(`You are filling in a job application form. You ARE the applicant — write every answer in first person (I, me, my) as if you are providing the information directly. Never say "the candidate" or use third person.

Your profile (JSON):
%s

Form question: %s

%s

Rules:
- Return only the answer, no explanation, no punctuation wrapper
- Always use first person: "I have...", "I am...", "My experience..." — never "The candidate..."
- For yes/no questions about skills listed in your profile, answer "Yes"
- For "how many years of experience/exposure/knowledge with [X]" questions, return a COUNT (integer, e.g. "9"), NEVER a calendar year (e.g. "2017"). Calculate: current year minus the start year from experience_details.
- Calculate years of experience from experience_details when asked
- Use application_defaults fields (requires_sponsorship, notice_period, salary_expectation) when relevant
- For phone number fields, return ONLY digits (and a leading + for international numbers) — no words or sentences
- For email fields, return ONLY the email address — no words or sentences
- If asked about working in a city or location other than your current one, respond politely that you are currently based in [your city/country] but are enthusiastic about the role and happy to relocate for the right opportunity. Never say you cannot or are unable to work there.`,
		string(profileJSON), question, optionsPart)
}

// FormVisionIdentifyPrompt is the production form-vision user prompt.
// BuildTailorPrompt renders the production resume tailoring user prompt.
func BuildTailorPrompt(profile *domain.ResumeProfile, jobDesc string) (string, error) {
	marketInstructions, jobDesc := splitMarketPrefix(jobDesc)
	trimmed := ForTailoring(profile)
	profileJSON, err := json.Marshal(trimmed)
	if err != nil {
		return "", err
	}
	var prompt bytes.Buffer
	if err := tailorTempl.Execute(&prompt, promptData{
		Profile: string(profileJSON), JobDescription: jobDesc,
		MarketInstructions: marketInstructions, PromptInstructions: profile.PromptInstructions,
	}); err != nil {
		return "", err
	}
	return prompt.String(), nil
}

// BuildCoverLetterPrompt renders the production cover letter user prompt.
func BuildCoverLetterPrompt(profile *domain.ResumeProfile, jobDesc string) (string, error) {
	marketInstructions, jobDesc := splitMarketPrefix(jobDesc)
	trimmed := ForCoverLetter(profile)
	profileJSON, err := json.Marshal(trimmed)
	if err != nil {
		return "", err
	}
	var prompt bytes.Buffer
	if err := coverLetterTempl.Execute(&prompt, promptData{
		Profile: string(profileJSON), JobDescription: jobDesc,
		MarketInstructions: marketInstructions, ExperienceContext: computeExperienceContext(profile),
		PromptInstructions: profile.PromptInstructions,
	}); err != nil {
		return "", err
	}
	return prompt.String(), nil
}

// BuildApplicationQuestionsPrompt returns system and user messages for application questions.
func BuildApplicationQuestionsPrompt(profile *domain.ResumeProfile, jobContext string, questions []string) (system, user string, err error) {
	trimmed := ForScoring(profile)
	profileJSON, err := json.Marshal(trimmed)
	if err != nil {
		return "", "", err
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
	return questionsSystemPrompt, sb.String(), nil
}

const FormVisionIdentifyPrompt = `This is a screenshot of a job application form step.
Return a JSON array of the visible, unanswered form fields. Each element must have:
  "type": one of "radio", "select", or "text"
  "question": the visible label text for the field
  "options": array of option label strings for radio/select; empty array for text fields

Return ONLY the JSON array, no markdown fencing, no explanation.`
