package bot

// profile_binding.go — registry of profile-field bindings for the adaptive engine.
//
// When the LLM identifies a form field as matching a known candidate profile field,
// it returns intent_type="profile" and a binding_key such as
// "application_defaults.notice_period". The interpreter calls resolveProfileBinding
// at interpretation time (and again on each warm cache hit) so the answer always
// reflects the candidate's current profile data.
//
// Add entries to profileBindings when the LLM learns to recognise new profile fields.
// Keys are lowercase dotted paths; the value is a function that extracts the
// corresponding string from a ResumeProfile.

import (
	"sort"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

// profileBindings maps dotted binding keys to profile field accessors.
var profileBindings = map[string]func(*domain.ResumeProfile) string{
	"personal_information.full_name": func(p *domain.ResumeProfile) string {
		return strings.TrimSpace(p.PersonalInformation.Name + " " + p.PersonalInformation.Surname)
	},
	"personal_information.name":    func(p *domain.ResumeProfile) string { return p.PersonalInformation.Name },
	"personal_information.surname": func(p *domain.ResumeProfile) string { return p.PersonalInformation.Surname },
	"personal_information.email":   func(p *domain.ResumeProfile) string { return p.PersonalInformation.Email },
	"personal_information.phone":   func(p *domain.ResumeProfile) string { return p.PersonalInformation.Phone },
	"personal_information.country": func(p *domain.ResumeProfile) string { return p.PersonalInformation.Country },
	"personal_information.city":    func(p *domain.ResumeProfile) string { return p.PersonalInformation.City },
	"personal_information.zip_code": func(p *domain.ResumeProfile) string {
		return p.PersonalInformation.ZipCode
	},
	"personal_information.address": func(p *domain.ResumeProfile) string { return p.PersonalInformation.Address },
	"application_defaults.notice_period": func(p *domain.ResumeProfile) string {
		return p.ApplicationDefaults.NoticePeriod
	},
	"application_defaults.salary_expectation": func(p *domain.ResumeProfile) string {
		return currentSalaryExpectation(p)
	},
	"application_defaults.availability":        func(p *domain.ResumeProfile) string { return p.ApplicationDefaults.Availability },
	"application_defaults.earliest_start_date": func(p *domain.ResumeProfile) string { return p.ApplicationDefaults.EarliestStartDate },
	"application_defaults.additional_details":  func(p *domain.ResumeProfile) string { return p.ApplicationDefaults.AdditionalDetails },
	"application_defaults.travel_conditions":   func(p *domain.ResumeProfile) string { return p.ApplicationDefaults.TravelConditions },
	"application_defaults.working_hours":       func(p *domain.ResumeProfile) string { return p.ApplicationDefaults.WorkingHours },
	"application_defaults.commute_conditions":  func(p *domain.ResumeProfile) string { return p.ApplicationDefaults.CommuteConditions },
	"application_defaults.future_requires_sponsorship": func(p *domain.ResumeProfile) string {
		if p.ApplicationDefaults.FutureRequiresSponsorship == nil {
			return ""
		}
		if *p.ApplicationDefaults.FutureRequiresSponsorship {
			return "Yes"
		}
		return "No"
	},
	"application_defaults.relocation.willing": func(p *domain.ResumeProfile) string {
		if p.ApplicationDefaults.Relocation == nil || p.ApplicationDefaults.Relocation.Willing == nil {
			return ""
		}
		if *p.ApplicationDefaults.Relocation.Willing {
			return "Yes"
		}
		return "No"
	},
	"application_defaults.relocation.details": func(p *domain.ResumeProfile) string {
		if p.ApplicationDefaults.Relocation == nil {
			return ""
		}
		return p.ApplicationDefaults.Relocation.Details
	},
	"application_defaults.requires_sponsorship": func(p *domain.ResumeProfile) string {
		if p.ApplicationDefaults.RequiresSponsorship == nil {
			return ""
		}
		if *p.ApplicationDefaults.RequiresSponsorship {
			return "Yes"
		}
		return "No"
	},
}

// knownBindingKeys returns a sorted slice of all registered binding key strings.
// Used by LLMFormInterpreter to build the batch prompt.
func knownBindingKeys() []string {
	keys := make([]string, 0, len(profileBindings))
	for k := range profileBindings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func knownProfileBinding(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if strings.HasPrefix(key, "application_defaults.form_answers.") {
		return strings.TrimPrefix(key, "application_defaults.form_answers.") != ""
	}
	_, ok := profileBindings[strings.ToLower(strings.TrimSpace(key))]
	return ok
}

func normalizeProfileBindingKey(key string) string {
	key = strings.TrimSpace(key)
	const answerPrefix = "application_defaults.form_answers."
	if strings.HasPrefix(strings.ToLower(key), answerPrefix) {
		return answerPrefix + key[len(answerPrefix):] // user-defined keys are case-sensitive
	}
	return strings.ToLower(key)
}

// resolveProfileBinding returns the profile value for key, or ("", false) if unknown or empty.
// key must be a dotted path in the profileBindings registry.
func resolveProfileBinding(key string, profile *domain.ResumeProfile) (string, bool) {
	key = normalizeProfileBindingKey(key)
	if profile == nil || key == "" {
		return "", false
	}
	if strings.HasPrefix(key, "application_defaults.form_answers.") {
		value := profile.ApplicationDefaults.FormAnswers[strings.TrimPrefix(key, "application_defaults.form_answers.")]
		return value, strings.TrimSpace(value) != ""
	}
	fn, ok := profileBindings[strings.ToLower(strings.TrimSpace(key))]
	if !ok {
		return "", false
	}
	v := fn(profile)
	return v, v != ""
}
