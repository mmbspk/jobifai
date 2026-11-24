package bot

import (
	"strconv"
	"strings"

	"github.com/nyaruka/phonenumbers"
	"github.com/user/jobifai/internal/domain"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

func currentPhoneOptions(field SnapField) []formFieldOption {
	if field.BrowserField != nil {
		return field.BrowserField.Options
	}
	options := make([]formFieldOption, len(field.Options))
	for i, label := range field.Options {
		options[i].Label = label
		if i < len(field.OptionValues) {
			options[i].Value = field.OptionValues[i]
		}
	}
	return options
}

func visiblePhoneOptionRegion(label string) (string, bool) {
	match := ""
	for region := range phonenumbers.GetSupportedRegions() {
		parsed, _ := language.ParseRegion(region)
		if questionCountryMatches(SnapField{Label: label}, display.English.Regions().Name(parsed)) {
			if match != "" && match != region {
				return "", false
			}
			match = region
		}
	}
	return match, match != ""
}

func declaredPhoneOptionRegion(value string) (string, bool) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if match := phoneOptionRegion.FindStringSubmatch(value); len(match) == 3 {
		value = match[1]
	} else if !phoneOptionISORegion.MatchString(value) {
		return "", false
	}
	region, err := language.ParseRegion(value)
	if err != nil || !region.IsCountry() || phonenumbers.GetCountryCodeForRegion(region.String()) == 0 {
		return "", false
	}
	return region.String(), true
}

// An unfamiliar option value is a learnable structure, rather than a missing
// applicant fact. The model may map its public label only when phone metadata
// establishes one country/code choice and no recognizable contradiction exists.
func phoneCallingCodeFallbackChoice(field SnapField, profile *domain.ResumeProfile) (string, bool) {
	contextText := field.Label + " " + field.Group + " " + field.HelpText
	if !isPhoneCallingCodeField(field) || manualDeclaration(field) || personalBindingContextIsQualified(contextText) || isJobDependentQuestion(contextText) {
		return "", false
	}
	options := currentPhoneOptions(field)
	number, err := parseProfilePhone(profile, options)
	if err != nil {
		return "", false
	}
	targetRegion, targetCode := phonenumbers.GetRegionCodeForNumber(number), strconv.Itoa(int(number.GetCountryCode()))
	choice, candidates, unfamiliar := "", 0, false
	for _, option := range options {
		declared, declaredOK := declaredPhoneOptionRegion(option.Value)
		visible, visibleOK := visiblePhoneOptionRegion(option.Label)
		dials := phoneOptionDial.FindAllStringSubmatch(option.Label, -1)
		if declaredOK && visibleOK && declared != visible {
			return "", false
		}
		region := visible
		if declaredOK {
			region = declared
		}
		if region == "" {
			continue
		}
		expectedCode := strconv.Itoa(phonenumbers.GetCountryCodeForRegion(region))
		if composite := phoneOptionRegion.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(option.Value))); len(composite) == 3 && composite[2] != expectedCode {
			return "", false
		}
		for _, dial := range dials {
			if dial[1] != expectedCode {
				return "", false
			}
		}
		if region != targetRegion || expectedCode != targetCode {
			continue
		}
		_, _, standard := optionPhoneRegion(option)
		if !standard && (!visibleOK || len(dials) == 0) {
			return "", false
		}
		candidates++
		choice, unfamiliar = option.Label, unfamiliar || !standard
	}
	return choice, candidates == 1 && unfamiliar
}

func phoneCallingCodeFallbackAvailable(field SnapField, profile *domain.ResumeProfile) bool {
	_, ok := phoneCallingCodeFallbackChoice(field, profile)
	return ok
}

func phoneCallingCodeEvidenceCompatible(field SnapField, profile *domain.ResumeProfile, value string, evidence []ProfileEvidence) bool {
	if !isPhoneCallingCodeField(field) {
		return true
	}
	choice, ok := phoneCallingCodeFallbackChoice(field, profile)
	if !ok || !strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(choice)) {
		return false
	}
	phone, country, prefix := false, false, false
	for _, entry := range evidence {
		switch entry.Path {
		case "/personal_information/phone":
			phone = true
		case "/personal_information/country":
			country = true
		case "/personal_information/phone_prefix":
			prefix = true
		}
	}
	if !phone {
		return false
	}
	pi := profile.PersonalInformation
	return strings.HasPrefix(strings.TrimSpace(pi.Phone), "+") || (strings.TrimSpace(pi.PhonePrefix) != "" && prefix) || country
}
