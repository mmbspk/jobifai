package resume

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
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

const extractPromptHeader = `You are an expert resume parser. Extract ALL information from the resume text at the bottom of this message and return it as a single JSON object.

Return ONLY the JSON, no markdown fences, no explanation, no text before or after.

Extraction rules:
- Phone: if the number starts with a country code like "+61 412 345 678", put "+61" in phone_prefix and "412 345 678" in phone. If there is no country code, put the whole number in phone.
- Headline: extract the professional title or tagline that appears under the candidate's name (e.g. "Senior DevOps Engineer", "Postdoctoral Research Fellow"). If none is present, leave empty.
- City: always extract city even when it is part of a longer address such as "12 Main St, Sydney NSW 2000, Australia" → city = "Sydney".
- Languages: extract every language mentioned with its proficiency level (Native, Fluent, Professional, Intermediate, Basic, A1-C2, etc.).
- employment_period: combine the start and end date into one string e.g. "Jan 2020 – Mar 2023" or "2019 – Present".
- key_responsibilities: split bullet points into separate list items.
- thesis: if an education entry includes a thesis or dissertation title, extract it into the thesis field.
- publications: extract all peer-reviewed papers, books, book chapters, reports, put all authors as a single string, extract year, journal/venue name, DOI or URL if present, and status (Published, In Preparation, Submitted, etc.).
- presentations: extract conference talks, posters, and invited talks, year, full conference name, presentation title, and role (Oral Presenter, Poster Presenter, Invited Speaker, etc.).
- grants: extract research grants and funding awards, year or period, funding body/funder name, project title, and amount if stated.
- certifications: use for professional certifications, licences, professional development courses, and professional memberships.
- interests: use for research interests, areas of expertise, or stated personal interests.
- Omit any key whose value you cannot find, do not include empty strings.

JSON schema (fill every field you can find):
{
  "personal_information": {
    "name": "", "surname": "", "headline": "", "email": "", "phone": "", "phone_prefix": "",
    "country": "", "city": "", "address": "", "zip_code": "", "github": "", "linkedin": ""
  },
  "education_details": [
    { "education_level": "", "institution": "", "field_of_study": "", "thesis": "", "start_date": "", "year_of_completion": "" }
  ],
  "experience_details": [
    { "position": "", "company": "", "employment_period": "", "location": "", "industry": "", "key_responsibilities": [], "skills_acquired": [] }
  ],
  "projects": [
    { "name": "", "description": "", "link": "", "technologies": [] }
  ],
  "certifications": [
    { "name": "", "issuer": "", "date": "", "link": "" }
  ],
  "publications": [
    { "authors": "", "title": "", "journal": "", "year": "", "doi": "", "status": "" }
  ],
  "presentations": [
    { "year": "", "conference": "", "title": "", "role": "" }
  ],
  "grants": [
    { "year": "", "funder": "", "project": "", "amount": "" }
  ],
  "languages": [
    { "language": "", "proficiency": "" }
  ],
  "skills": [],
  "interests": [],
  "summary": ""
}
`

// BuildExtractPrompt returns the production resume extraction prompt including resume text.
func BuildExtractPrompt(resumeText string) string {
	return extractPromptHeader + "\nResume text:\n" + resumeText
}

// BuildExtractMessages splits static extraction instructions from resume text (optional Claude cache block).
func BuildExtractMessages(resumeText string) []llm.Message {
	return []llm.Message{
		{Role: "system", Content: extractPromptHeader, CacheEphemeral: true},
		{Role: "user", Content: "Resume text:\n" + resumeText},
	}
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
// BuildTailorPrompt renders the full resume tailoring prompt (single string, tests/diagnostics).
func BuildTailorPrompt(profile *domain.ResumeProfile, jobDesc string) (string, error) {
	msgs, err := BuildTailorMessages(profile, jobDesc)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, m := range msgs {
		parts = append(parts, m.Content)
	}
	return strings.Join(parts, "\n\n"), nil
}

// BuildTailorMessages renders production resume tailoring messages (static system + dynamic user).
func BuildTailorMessages(profile *domain.ResumeProfile, jobDesc string) ([]llm.Message, error) {
	marketInstructions, jobDesc := splitMarketPrefix(jobDesc)
	trimmed := ForTailoring(profile)
	profileJSON, err := json.Marshal(trimmed)
	if err != nil {
		return nil, err
	}
	data := promptData{
		Profile:            string(profileJSON),
		JobDescription:     jobDesc,
		MarketInstructions: marketInstructions,
		PromptInstructions: profile.PromptInstructions,
	}
	var sys, user bytes.Buffer
	if err := tailorSystemTempl.Execute(&sys, data); err != nil {
		return nil, err
	}
	if err := tailorUserTempl.Execute(&user, data); err != nil {
		return nil, err
	}
	return []llm.Message{
		{Role: "system", Content: sys.String(), CacheEphemeral: true},
		{Role: "user", Content: user.String()},
	}, nil
}

// BuildCoverLetterPrompt renders the full cover letter prompt (single string, tests/diagnostics).
func BuildCoverLetterPrompt(profile *domain.ResumeProfile, jobDesc string) (string, error) {
	msgs, err := BuildCoverLetterMessages(profile, jobDesc)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, m := range msgs {
		parts = append(parts, m.Content)
	}
	return strings.Join(parts, "\n\n"), nil
}

// BuildCoverLetterMessages renders production cover letter messages (static system + dynamic user).
func BuildCoverLetterMessages(profile *domain.ResumeProfile, jobDesc string) ([]llm.Message, error) {
	marketInstructions, jobDesc := splitMarketPrefix(jobDesc)
	trimmed := ForCoverLetter(profile)
	profileJSON, err := json.Marshal(trimmed)
	if err != nil {
		return nil, err
	}
	data := promptData{
		Profile:            string(profileJSON),
		JobDescription:     jobDesc,
		MarketInstructions: marketInstructions,
		ExperienceContext:  computeExperienceContext(profile),
		PromptInstructions: profile.PromptInstructions,
	}
	var sys, user bytes.Buffer
	if err := coverLetterSystemTempl.Execute(&sys, data); err != nil {
		return nil, err
	}
	if err := coverLetterUserTempl.Execute(&user, data); err != nil {
		return nil, err
	}
	return []llm.Message{
		{Role: "system", Content: sys.String(), CacheEphemeral: true},
		{Role: "user", Content: user.String()},
	}, nil
}

// BuildApplicationQuestionsPrompt returns system and user messages for application questions.
func BuildApplicationQuestionsPrompt(profile *domain.ResumeProfile, jobContext string, questions []string) (system, user string, err error) {
	trimmed := ForFormFilling(profile)
	profileJSON, err := json.Marshal(trimmed)
	if err != nil {
		return "", "", err
	}
	var sb strings.Builder
	sb.WriteString("## Candidate Profile\n")
	sb.Write(profileJSON)
	sb.WriteString("\n\n## Application defaults (factual)\n")
	sb.WriteString(formatApplicationDefaults(trimmed.ApplicationDefaults))
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

func formatApplicationDefaults(ad domain.ApplicationDefaults) string {
	sponsorship := "No"
	if ad.RequiresSponsorship {
		sponsorship = "Yes"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "- Requires employer sponsorship: %s\n", sponsorship)
	if ad.NoticePeriod != "" {
		fmt.Fprintf(&sb, "- Notice period: %s\n", ad.NoticePeriod)
	}
	if ad.SalaryExpectation != "" {
		fmt.Fprintf(&sb, "- Salary expectation: %s\n", ad.SalaryExpectation)
	}
	return sb.String()
}

const FormVisionIdentifyPrompt = `This is a screenshot of a job application form step.
Return a JSON array of the visible, unanswered form fields. Each element must have:
  "type": one of "radio", "select", or "text"
  "question": the visible label text for the field
  "options": array of option label strings for radio/select; empty array for text fields

Return ONLY the JSON array, no markdown fencing, no explanation.`
