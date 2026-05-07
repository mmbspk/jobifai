package resume

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
)

var tailorTempl = template.Must(template.New("tailor").Parse(`You are an expert resume writer. Given a candidate's resume profile and a job description, rewrite the resume profile JSON to maximally highlight relevant experience, skills, and achievements for this specific role.
{{if .MarketInstructions}}
MARKET-SPECIFIC INSTRUCTIONS (these override default behaviour, follow exactly):
{{.MarketInstructions}}
{{end}}
{{- if .PromptInstructions}}
ADDITIONAL INSTRUCTIONS FROM CANDIDATE (follow these):
{{.PromptInstructions}}
{{end}}
HUMAN WRITING RULES, apply to all text fields (summary, bullets):
- No em dashes (—) anywhere. Use a comma, full stop, or colon instead.
- Bullet points start with a strong past-tense action verb: Built, Designed, Led, Owned, Shipped, Reduced, Scaled, Migrated, Mentored
- Each bullet is one clear thought, no compound sentences joined by semicolons
- No vague qualifiers like "...to drive better outcomes" or "...for improved performance"
- No filler phrases: "results-driven", "proven track record", "passionate about", "unique blend of"
- Summary must be specific to this candidate's real experience, not a generic template

Rules:
- Keep all factual information accurate, do not invent experience or skills
- Reorder experience bullets to lead with most relevant impact
- Add relevant technologies from the job description only if the candidate actually has them
- Sharpen the summary to directly address the job requirements
- For publications: include all publications, do not remove any
- For presentations: include all conference presentations, do not remove any
- For grants: include all grants and funding entries, do not remove any
- For projects: reorder to lead with most relevant to the job description
- Return ONLY valid JSON with no markdown fencing, matching the exact ResumeProfile shape provided

Candidate profile:
{{.Profile}}

Job description:
{{.JobDescription}}`))

var coverLetterTempl = template.Must(template.New("cover").Parse(`You are an expert cover letter writer. Write a cover letter (3–4 paragraphs) for the candidate.
{{if .MarketInstructions}}
MARKET-SPECIFIC INSTRUCTIONS (follow exactly):
{{.MarketInstructions}}
{{end}}
{{- if .PromptInstructions}}
ADDITIONAL INSTRUCTIONS FROM CANDIDATE (follow these):
{{.PromptInstructions}}
{{end}}
{{if .ExperienceContext}}
FACTUAL CONTEXT, these values are pre-computed and correct; use them exactly, do not recalculate from dates:
{{.ExperienceContext}}
{{end}}
HUMAN WRITING RULES, NON-NEGOTIABLE:
1. No em dashes (—) anywhere. Replace with a comma, full stop, or colon.
2. No AI filler phrases. Delete any of these on sight:
   "I am passionate about", "I thrive in fast-paced environments", "I am excited about the opportunity",
   "With a proven track record", "I am a results-driven professional", "I bring a unique blend of",
   "leveraging my expertise to drive impact", "I would be a great fit", "I look forward to the opportunity",
   "Please find attached", "Thank you for your time and consideration", "As a [job title],"
3. No hollow openers. Never start with generic enthusiasm or restating the job title.
   Wrong: "I am writing to express my interest in the Senior Engineer role."
   Right: Start with something real and specific about the candidate's experience.
4. Write in the candidate's actual voice, first person, contractions where natural (I've, I'm, it's).
5. Be specific. Every claim must reference something real from the profile.
   Wrong: "I have experience with cloud infrastructure at scale."
   Right: "I've run Go microservices across four AWS regions simultaneously."
6. Vary sentence lengths. Short sentences land harder. Avoid perfectly balanced parallel structures.
7. No sign-off padding. End on something genuine, a real statement of interest or intent.
   Wrong closing: "I would welcome the opportunity to discuss. Thank you for considering my application."
   Right closing: One direct sentence that ends the letter without filler.
8. 3–4 paragraphs max. Each paragraph has one job: who you are / why this company / what you bring / close.
9. Do NOT include headers, date lines, address blocks, or subject lines, body paragraphs only.
10. Before writing, check: zero em dashes, no banned phrases, no generic opener, all claims are specific.
11. TENURE: When stating how long the candidate worked somewhere, use the pre-computed values from FACTUAL CONTEXT above, never calculate years from date strings yourself.
12. INTERNAL NAMES: Never name specific internal systems, proprietary tools, project codenames, clients, or team-specific terminology from the profile. Keep all such references generic: "a platform", "our systems", "the product", "internal tooling", "client projects", "key accounts", whatever fits the field.
13. COUNTS: Never state an exact number of systems, projects, clients, products, or similar countable items. If the count is fewer than 10, say "multiple". If 10 or more, say "tens of".

Candidate profile:
{{.Profile}}
{{if .JobDescription}}
Job description:
{{.JobDescription}}
{{- else}}
No job description provided. Write a strong general cover letter suitable for an open or speculative application, showcasing the candidate's strongest experience and value. Do NOT ask for a job description — write the best possible letter using the profile alone.
{{- end}}`))

// Tailor uses an LLM to rewrite a ResumeProfile for a specific job description.
// Three separate clients allow per-task model overrides.
type Tailor struct {
	tailorClient *llm.Client // TailorProfile
	coverClient  *llm.Client // WriteCoverLetter
	formClient   *llm.Client // AnswerFormQuestion + IdentifyFormFields
}

func NewTailor(tailorClient, coverClient, formClient *llm.Client) *Tailor {
	return &Tailor{tailorClient: tailorClient, coverClient: coverClient, formClient: formClient}
}

type promptData struct {
	Profile            string
	JobDescription     string
	MarketInstructions string
	ExperienceContext  string
	PromptInstructions string
}

// TailorProfile rewrites profile JSON targeting the given job description.
// jobDesc may be prefixed with market instructions via the caller.
func (t *Tailor) TailorProfile(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (*domain.ResumeProfile, error) {
	marketInstructions, jobDesc := splitMarketPrefix(jobDesc)
	trimmed := ForTailoring(profile)
	profileJSON, err := json.Marshal(trimmed)
	if err != nil {
		return nil, fmt.Errorf("tailor: marshal profile: %w", err)
	}

	var prompt bytes.Buffer
	if err := tailorTempl.Execute(&prompt, promptData{
		Profile:            string(profileJSON),
		JobDescription:     jobDesc,
		MarketInstructions: marketInstructions,
		PromptInstructions: profile.PromptInstructions,
	}); err != nil {
		return nil, fmt.Errorf("tailor: render prompt: %w", err)
	}

	raw, err := t.tailorClient.Chat(llm.WithTask(ctx, "tailor resume"), []llm.Message{
		{Role: "user", Content: prompt.String()},
	})
	if err != nil {
		return nil, fmt.Errorf("tailor: llm: %w", err)
	}

	raw = stripJSON(raw)
	var tailored domain.ResumeProfile
	if err := json.Unmarshal([]byte(raw), &tailored); err != nil {
		return nil, fmt.Errorf("tailor: parse llm output: %w\nraw: %s", err, raw)
	}
	return &tailored, nil
}

// WriteCoverLetter generates a cover letter body for the profile + job description.
func (t *Tailor) WriteCoverLetter(ctx context.Context, profile *domain.ResumeProfile, jobDesc string) (string, error) {
	marketInstructions, jobDesc := splitMarketPrefix(jobDesc)
	trimmed := ForCoverLetter(profile)
	profileJSON, err := json.Marshal(trimmed)
	if err != nil {
		return "", fmt.Errorf("cover letter: marshal profile: %w", err)
	}

	var prompt bytes.Buffer
	if err := coverLetterTempl.Execute(&prompt, promptData{
		Profile:            string(profileJSON),
		JobDescription:     jobDesc,
		MarketInstructions: marketInstructions,
		ExperienceContext:  computeExperienceContext(profile),
		PromptInstructions: profile.PromptInstructions,
	}); err != nil {
		return "", fmt.Errorf("cover letter: render prompt: %w", err)
	}

	body, err := t.coverClient.Chat(llm.WithTask(ctx, "cover letter"), []llm.Message{
		{Role: "user", Content: prompt.String()},
	})
	if err != nil {
		return "", fmt.Errorf("cover letter: llm: %w", err)
	}
	return strings.TrimFunc(body, unicode.IsSpace), nil
}

// AnswerFormQuestion uses the LLM to pick the best answer for a job-application form field.
// profileJSON is a pre-serialized trimmed profile (caller caches this once per job session).
// options is non-nil for radio/select questions; nil for free-text fields.
func (t *Tailor) AnswerFormQuestion(ctx context.Context, profileJSON []byte, question string, options []string) (string, error) {
	optionsPart := "Provide a concise answer (1–2 sentences, plain text, no punctuation at the end)."
	if len(options) > 0 {
		optionsPart = "Available options, return EXACTLY one of these labels, nothing else: " + strings.Join(options, " | ")
	}

	prompt := fmt.Sprintf(`You are filling out a job application form on behalf of this candidate.
Answer the question accurately and concisely based on their profile.

Candidate profile (JSON):
%s

Form question: %s

%s

Rules:
- Return only the answer, no explanation, no punctuation wrapper
- For yes/no questions about skills in the profile, answer "Yes" if present
- Calculate years of experience from experience_details when asked
- Use application_defaults fields (requires_sponsorship, notice_period, salary_expectation) when relevant`,
		string(profileJSON), question, optionsPart)

	answer, err := t.formClient.Chat(llm.WithTask(ctx, "form question"), []llm.Message{{Role: "user", Content: prompt}})
	if err != nil {
		return "", fmt.Errorf("form answer: llm: %w", err)
	}
	return strings.TrimFunc(answer, unicode.IsSpace), nil
}

// IdentifyFormFields sends a screenshot to Claude and asks it to identify
// visible form fields, returning them as a slice of IdentifiedField.
// Only works when the underlying LLM client supports vision (Claude).
func (t *Tailor) IdentifyFormFields(ctx context.Context, imageBytes []byte) ([]domain.IdentifiedField, error) {
	const prompt = `This is a screenshot of a job application form step.
Return a JSON array of the visible, unanswered form fields. Each element must have:
  "type": one of "radio", "select", or "text"
  "question": the visible label text for the field
  "options": array of option label strings for radio/select; empty array for text fields

Return ONLY the JSON array, no markdown fencing, no explanation.`

	raw, err := t.formClient.ChatWithImage(llm.WithTask(ctx, "identify form fields"), imageBytes, prompt)
	if err != nil {
		return nil, fmt.Errorf("identify fields: %w", err)
	}
	raw = stripJSON(raw)
	var fields []domain.IdentifiedField
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, fmt.Errorf("identify fields: parse response: %w\nraw: %s", err, raw)
	}
	return fields, nil
}

// splitMarketPrefix extracts the market instructions block (everything before
// the first "Job URL:" or "LinkedIn:" line) from the combined jobDesc string.
// Returns (marketInstructions, remainder).
func splitMarketPrefix(s string) (string, string) {
	markers := []string{"Job URL:", "LinkedIn:", "GitHub:"}
	for _, marker := range markers {
		if idx := strings.Index(s, marker); idx > 0 {
			return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx:])
		}
	}
	return "", s
}

func stripJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimFunc(s, unicode.IsSpace)
	// Extract just the JSON object, discarding any prose the model appended after the closing brace.
	if start := strings.Index(s, "{"); start >= 0 {
		if end := strings.LastIndex(s, "}"); end > start {
			s = s[start : end+1]
		}
	}
	return s
}

var reYear = regexp.MustCompile(`\b(19|20)\d{2}\b`)

// parseYearsFromPeriod extracts start/end years from strings like
// "Jan 2019 - Mar 2023", "2019 - Present", "2020 - 2022".
func parseYearsFromPeriod(period string) (start, end int) {
	matches := reYear.FindAllString(period, -1)
	lower := strings.ToLower(period)
	isPresent := strings.Contains(lower, "present") ||
		strings.Contains(lower, "current") ||
		strings.Contains(lower, "now")
	now := time.Now().Year()
	switch len(matches) {
	case 0:
		if isPresent {
			return now, now
		}
		return 0, 0
	case 1:
		y, _ := strconv.Atoi(matches[0])
		if isPresent {
			return y, now
		}
		return y, y
	default:
		s, _ := strconv.Atoi(matches[0])
		e, _ := strconv.Atoi(matches[len(matches)-1])
		if isPresent {
			e = now
		}
		return s, e
	}
}

// computeExperienceContext builds a factual tenure summary injected into the
// cover letter prompt so the LLM uses pre-calculated values instead of
// deriving years from raw date strings.
func computeExperienceContext(profile *domain.ResumeProfile) string {
	if profile == nil || len(profile.ExperienceDetails) == 0 {
		return ""
	}
	var lines []string
	for _, exp := range profile.ExperienceDetails {
		if exp.Company == "" || exp.EmploymentPeriod == "" {
			continue
		}
		start, end := parseYearsFromPeriod(exp.EmploymentPeriod)
		if start == 0 {
			continue
		}
		years := end - start
		if years < 1 {
			years = 1
		}
		lines = append(lines, fmt.Sprintf("- %s: %d years", exp.Company, years))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}
