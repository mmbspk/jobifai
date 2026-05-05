package resume

import "github.com/user/jobifai/internal/domain"

// capExperience returns up to maxRoles entries, each with at most maxBullets
// key responsibilities. Remaining fields are preserved unchanged.
func capExperience(exp []domain.ExperienceDetail, maxRoles, maxBullets int) []domain.ExperienceDetail {
	if len(exp) > maxRoles {
		exp = exp[:maxRoles]
	}
	out := make([]domain.ExperienceDetail, len(exp))
	for i, e := range exp {
		out[i] = e
		if maxBullets >= 0 && len(e.KeyResponsibilities) > maxBullets {
			bullets := make([]string, maxBullets)
			copy(bullets, e.KeyResponsibilities)
			out[i].KeyResponsibilities = bullets
		}
	}
	return out
}

// ForScoring returns a trimmed profile suitable for job suitability scoring.
// Keeps: summary, skills, recent 3 roles (3 bullets each), education level only.
// Drops: certifications, presentations, grants, interests, publications, projects.
func ForScoring(p *domain.ResumeProfile) domain.ResumeProfile {
	if p == nil {
		return domain.ResumeProfile{}
	}
	edu := make([]domain.EducationDetail, len(p.EducationDetails))
	for i, e := range p.EducationDetails {
		edu[i] = domain.EducationDetail{
			EducationLevel: e.EducationLevel,
			FieldOfStudy:   e.FieldOfStudy,
		}
	}
	return domain.ResumeProfile{
		Summary:            p.Summary,
		Skills:             p.Skills,
		ExperienceDetails:  capExperience(p.ExperienceDetails, 3, 3),
		EducationDetails:   edu,
		PromptInstructions: p.PromptInstructions,
	}
}

// ForCoverLetter returns a trimmed profile for cover letter generation.
// Keeps: summary, skills, recent 3 roles (5 bullets each), application_defaults.
// Drops: certifications, presentations, grants, interests, publications, education details.
func ForCoverLetter(p *domain.ResumeProfile) domain.ResumeProfile {
	if p == nil {
		return domain.ResumeProfile{}
	}
	return domain.ResumeProfile{
		PersonalInformation: p.PersonalInformation,
		Summary:             p.Summary,
		Skills:              p.Skills,
		ExperienceDetails:   capExperience(p.ExperienceDetails, 3, 5),
		ApplicationDefaults: p.ApplicationDefaults,
		PromptInstructions:  p.PromptInstructions,
	}
}

// ForTailoring returns a profile with only interests stripped.
// The tailoring prompt explicitly requires publications, grants, and presentations.
func ForTailoring(p *domain.ResumeProfile) domain.ResumeProfile {
	if p == nil {
		return domain.ResumeProfile{}
	}
	out := *p
	out.Interests = nil
	return out
}

// ForFormFilling returns a minimal profile for answering individual form questions.
// Keeps: personal info, application defaults, first 10 skills, exp titles+periods
// (no bullet points), languages.
// This is the most aggressive trim — each field is answered with a 1–5 word reply
// but the full profile would otherwise be re-sent 500+ times per day.
func ForFormFilling(p *domain.ResumeProfile) domain.ResumeProfile {
	if p == nil {
		return domain.ResumeProfile{}
	}
	skills := p.Skills
	if len(skills) > 10 {
		skills = skills[:10]
	}
	exp := make([]domain.ExperienceDetail, len(p.ExperienceDetails))
	for i, e := range p.ExperienceDetails {
		exp[i] = domain.ExperienceDetail{
			Position:         e.Position,
			Company:          e.Company,
			EmploymentPeriod: e.EmploymentPeriod,
			Location:         e.Location,
			Industry:         e.Industry,
		}
	}
	return domain.ResumeProfile{
		PersonalInformation: p.PersonalInformation,
		ApplicationDefaults: p.ApplicationDefaults,
		Skills:              skills,
		ExperienceDetails:   exp,
		Languages:           p.Languages,
		EducationDetails:    p.EducationDetails,
	}
}
