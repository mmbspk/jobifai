package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/user/jobifai/internal/domain"
)

func currentSalaryExpectation(profile *domain.ResumeProfile) string {
	if profile == nil {
		return ""
	}
	r := profile.ApplicationDefaults.SalaryRange
	if r == nil {
		return profile.ApplicationDefaults.SalaryExpectation
	}
	if r.Minimum == nil && r.Maximum == nil {
		return strings.TrimSpace(r.Details)
	}
	var amount string
	number := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	switch {
	case r.Minimum != nil && r.Maximum != nil:
		amount = number(*r.Minimum)
		if *r.Minimum != *r.Maximum {
			amount += "–" + number(*r.Maximum)
		}
	case r.Minimum != nil:
		amount = "at least " + number(*r.Minimum)
	case r.Maximum != nil:
		amount = "up to " + number(*r.Maximum)
	}
	var parts []string
	if r.Currency != "" {
		parts = append(parts, strings.ToUpper(r.Currency))
	}
	if amount != "" {
		parts = append(parts, amount)
	}
	if r.PayPeriod != "" {
		parts = append(parts, "per "+r.PayPeriod)
	}
	if r.Basis != "" {
		parts = append(parts, r.Basis+" compensation")
	}
	if r.IncludesBenefits != nil {
		benefits := strings.TrimSpace(r.BenefitsDetails)
		if benefits == "" {
			benefits = "benefits"
		}
		if *r.IncludesBenefits {
			parts = append(parts, "including "+benefits)
		} else {
			parts = append(parts, "excluding "+benefits)
		}
	} else if r.BenefitsDetails != "" {
		parts = append(parts, r.BenefitsDetails)
	}
	if r.Details != "" {
		parts = append(parts, r.Details)
	}
	return strings.Join(parts, " ")
}

// Select a declared country exception only from the question/vacancy context.
// An unknown jurisdiction cannot borrow a conflicting general default.
func currentSponsorship(field SnapField, profile *domain.ResumeProfile) (*bool, int, bool) {
	timing := sponsorshipTiming(field)
	if timing == "both" {
		now, ni, nok := scopedSponsorship(field, profile, false)
		future, fi, fok := scopedSponsorship(field, profile, true)
		if !nok || !fok {
			return nil, -1, false
		}
		value := *now || *future
		if ni != fi {
			return &value, -1, true
		}
		return &value, ni, true
	}
	return scopedSponsorship(field, profile, timing == "future")
}

func sponsorshipTiming(field SnapField) string {
	text := strings.ToLower(field.Label + " " + field.HelpText)
	future := matchesAny(text, "future", "will you", "at any time", "eventually")
	if future && matchesAny(text, "now", "current", "presently", "today") {
		return "both"
	}
	if future {
		return "future"
	}
	return "current"
}

func sponsorshipEvidenceCompatible(field SnapField, evidence []ProfileEvidence) bool {
	current, future := false, false
	for _, e := range evidence {
		if strings.HasSuffix(e.Path, "/future_requires_sponsorship") {
			future = true
			continue
		}
		if strings.HasSuffix(e.Path, "/requires_sponsorship") {
			current = true
			continue
		}
		quote := strings.ToLower(e.Quote)
		if !strings.Contains(quote, "sponsor") {
			continue
		}
		scope := sponsorshipTiming(SnapField{Label: quote})
		current = current || scope == "current" || scope == "both"
		future = future || scope == "future" || scope == "both"
	}
	switch sponsorshipTiming(field) {
	case "both":
		return current && future
	case "future":
		return future
	default:
		return current
	}
}

func scopedSponsorship(field SnapField, profile *domain.ResumeProfile, future bool) (*bool, int, bool) {
	if profile == nil {
		return nil, -1, false
	}
	index := -1
	for i, row := range profile.ApplicationDefaults.WorkRights {
		if !questionCountryMatches(field, row.Country) {
			continue
		}
		if index != -1 {
			return nil, -1, false
		}
		index = i
	}
	valueFor := func(row domain.ApplicationWorkRight) *bool {
		if future {
			return row.FutureRequiresSponsorship
		}
		return row.RequiresSponsorship
	}
	if index >= 0 && valueFor(profile.ApplicationDefaults.WorkRights[index]) != nil {
		return valueFor(profile.ApplicationDefaults.WorkRights[index]), index, true
	}
	general := profile.ApplicationDefaults.RequiresSponsorship
	if future {
		general = profile.ApplicationDefaults.FutureRequiresSponsorship
	}
	if general == nil {
		return nil, -1, false
	}
	knownCountry := index >= 0
	for country := range applicationCountryAliases {
		knownCountry = knownCountry || questionCountryMatches(field, country)
	}
	if !knownCountry {
		for _, row := range profile.ApplicationDefaults.WorkRights {
			if value := valueFor(row); value != nil && *value != *general {
				return nil, -1, false
			}
		}
	}
	return general, -1, true
}

func resolveProfileBindingForField(key string, profile *domain.ResumeProfile, field SnapField) (string, bool) {
	key = normalizeProfileBindingKey(key)
	if key != "application_defaults.requires_sponsorship" && key != "application_defaults.future_requires_sponsorship" {
		return resolveProfileBinding(key, profile)
	}
	// An old current-sponsorship mapping never authorizes a future declaration.
	if key == "application_defaults.future_requires_sponsorship" && sponsorshipTiming(field) == "current" {
		return "", false
	}
	value, _, ok := currentSponsorship(field, profile)
	if !ok {
		return "", false
	}
	if *value {
		return "Yes", true
	}
	return "No", true
}

func bindingEvidenceForField(profile *domain.ResumeProfile, binding string, field SnapField) []ProfileEvidence {
	binding = normalizeProfileBindingKey(binding)
	if binding == "application_defaults.requires_sponsorship" || binding == "application_defaults.future_requires_sponsorship" {
		_, _, ok := currentSponsorship(field, profile)
		if !ok {
			return nil
		}
		var evidence []ProfileEvidence
		for _, future := range []bool{false, true} {
			timing := sponsorshipTiming(field)
			if timing == "future" && !future || timing == "current" && future {
				continue
			}
			_, index, _ := scopedSponsorship(field, profile, future)
			key, base := "requires_sponsorship", "/application_defaults/"
			if future {
				key = "future_requires_sponsorship"
			}
			if index >= 0 {
				base = fmt.Sprintf("/application_defaults/work_rights/%d/", index)
				if value, exists := profileScalar(profile, base+"country"); exists {
					evidence = append(evidence, ProfileEvidence{Path: base + "country", Quote: value})
				}
			}
			if value, exists := profileScalar(profile, base+key); exists {
				evidence = append(evidence, ProfileEvidence{Path: base + key, Quote: value})
			}
		}
		return evidence
	}
	if binding == "application_defaults.salary_expectation" && profile != nil && profile.ApplicationDefaults.SalaryRange != nil {
		var evidence []ProfileEvidence
		for _, key := range []string{"minimum", "maximum", "currency", "pay_period", "basis", "includes_benefits", "benefits_details", "details"} {
			path := "/application_defaults/salary_range/" + key
			if value, exists := profileScalar(profile, path); exists && strings.TrimSpace(value) != "" {
				evidence = append(evidence, ProfileEvidence{Path: path, Quote: value})
			}
		}
		return evidence
	}
	return bindingEvidence(profile, binding)
}

func currentWorkRightsTimeValid(profile *domain.ResumeProfile, field SnapField, value string, evidence []ProfileEvidence, now time.Time) bool {
	if profile == nil || !isWorkAuthorizationQuestion(field.Label+" "+field.Group+" "+field.HelpText) {
		return true
	}
	answer := strings.ToLower(strings.TrimSpace(value))
	if strings.Trim(answer, " .!") == "no" {
		return true
	}
	for i, row := range profile.ApplicationDefaults.WorkRights {
		if row.VisaExpiryDate == "" {
			continue
		}
		relevant := questionCountryMatches(field, row.Country)
		prefix := fmt.Sprintf("/application_defaults/work_rights/%d/", i)
		for _, entry := range evidence {
			relevant = relevant || strings.HasPrefix(entry.Path, prefix)
		}
		if !relevant {
			continue
		}
		expires, err := time.Parse("2006-01-02", row.VisaExpiryDate)
		// A date-only expiry has no exact cutoff/time zone. Refresh affirmative
		// authority at its declared date boundary rather than assuming more time.
		if err != nil || !now.UTC().Before(expires) {
			return false
		}
	}
	return true
}

func currentFactPromptNote() string {
	return "Current date (UTC): " + time.Now().UTC().Format("2006-01-02") + ". Structured salary_range is authoritative over legacy salary_expectation when supplied. Country-specific sponsorship answers override the general default for that country. Current and future sponsorship are distinct facts; neither implies the other. Do not claim current work authorization from an expired visa or invent a missing expiry. Preserve currency, pay period, base/total and benefit qualifiers.\n\n"
}
