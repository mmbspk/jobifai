// Package resume provides resume extraction and generation services.
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

const extractPrompt = `You are an expert resume parser. Extract ALL information from the resume text at the bottom of this message and return it as a single JSON object.

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

Resume text:
`

// Extractor uses an LLM to parse raw resume text into a ResumeProfile.
type Extractor struct {
	client *llm.Client
}

func NewExtractor(client *llm.Client) *Extractor {
	return &Extractor{client: client}
}

// ExtractFromText calls the LLM to parse resume text into a structured profile.
func (e *Extractor) ExtractFromText(ctx context.Context, resumeText string) (*domain.ResumeProfile, error) {
	msgs := []llm.Message{
		{Role: "user", Content: extractPrompt + resumeText},
	}
	raw, err := e.client.Chat(llm.WithTask(ctx, "extract resume"), msgs)
	if err != nil {
		return nil, fmt.Errorf("extract resume: %w", err)
	}

	raw = strings.TrimSpace(raw)
	raw = stripFence(raw)

	var profile domain.ResumeProfile
	if err := json.Unmarshal([]byte(raw), &profile); err != nil {
		return nil, fmt.Errorf("extract resume: invalid JSON from LLM: %w\nraw: %s", err, raw)
	}
	return &profile, nil
}

func stripFence(s string) string {
	for _, prefix := range []string{"```json", "```"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			break
		}
	}
	s = strings.TrimSuffix(s, "```")
	return strings.TrimFunc(s, unicode.IsSpace)
}
