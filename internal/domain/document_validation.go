package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"
)

// ValidateResumeContent rejects empty model output without requiring a particular profession or career history.
func ValidateResumeContent(p *ResumeProfile) error {
	if p == nil {
		return errors.New("resume content is missing")
	}
	if strings.TrimSpace(p.Summary) != "" {
		return nil
	}
	for _, s := range p.Skills {
		if strings.TrimSpace(s) != "" {
			return nil
		}
	}
	for _, e := range p.ExperienceDetails {
		if strings.TrimSpace(e.Position+e.Company) != "" {
			return nil
		}
	}
	for _, e := range p.EducationDetails {
		if strings.TrimSpace(e.Institution+e.FieldOfStudy) != "" {
			return nil
		}
	}
	for _, e := range p.Projects {
		if strings.TrimSpace(e.Name+e.Description) != "" {
			return nil
		}
	}
	return errors.New("resume has no summary, skills, experience, education or projects")
}

// ValidateCoverContent checks that a generated cover contains prose rather than a JSON envelope or empty placeholder.
func ValidateCoverContent(body string) error {
	body = strings.TrimSpace(body)
	if json.Valid([]byte(body)) || strings.Contains(body, "```") || len(strings.Fields(body)) < 3 {
		return errors.New("cover letter must contain prose, not JSON or a placeholder")
	}
	letters := 0
	for _, r := range body {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	if letters < 10 {
		return errors.New("cover letter has insufficient text")
	}
	return nil
}
