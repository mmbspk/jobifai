package bot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nyaruka/phonenumbers"
	"github.com/user/jobifai/internal/domain"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

var phoneOptionRegion = regexp.MustCompile(`^([A-Z]{2,3})_([1-9][0-9]{0,3})$`)
var phoneOptionISORegion = regexp.MustCompile(`^[A-Z]{2,3}$`)
var phoneOptionDial = regexp.MustCompile(`\+([1-9][0-9]{0,3})\b`)

func isPhoneCallingCodeField(field SnapField) bool {
	label := strings.Trim(countryWords(field.Label), " ")
	switch label {
	case "phone country code", "telephone country code", "mobile country code", "country calling code", "phone calling code", "calling code", "phone dial code", "dialling code", "dial code", "phone prefix":
		return field.Type == SnapTypeSelect || field.Type == SnapTypeRadio
	}
	return false
}

func optionPhoneRegion(option formFieldOption) (string, int, bool) {
	value := strings.ToUpper(strings.TrimSpace(option.Value))
	match := phoneOptionRegion.FindStringSubmatch(value)
	regionValue, codeValue := value, ""
	if len(match) == 3 {
		regionValue, codeValue = match[1], match[2]
	} else if !phoneOptionISORegion.MatchString(value) {
		return "", 0, false
	}
	region, err := language.ParseRegion(regionValue)
	if err != nil || !region.IsCountry() {
		return "", 0, false
	}
	code := phonenumbers.GetCountryCodeForRegion(region.String())
	if code == 0 {
		return "", 0, false
	}
	if codeValue != "" && codeValue != strconv.Itoa(code) {
		return "", 0, false
	}
	visible := phoneOptionDial.FindAllStringSubmatch(option.Label, -1)
	// ISO-only values identify a country; the visible dial code establishes
	// that this option represents its phone prefix, rather than residence.
	if len(visible) == 0 && codeValue == "" {
		return "", 0, false
	}
	for _, dial := range visible {
		if dial[1] != strconv.Itoa(code) {
			return "", 0, false
		}
	}
	return region.String(), code, true
}

func profilePhoneRegion(country string, options []formFieldOption) string {
	if region, err := language.ParseRegion(strings.TrimSpace(country)); err == nil && region.IsCountry() {
		return region.String()
	}
	regions := map[string]bool{}
	for _, option := range options {
		region, _, valid := optionPhoneRegion(option)
		if valid && questionCountryMatches(SnapField{Label: option.Label}, country) {
			regions[region] = true
		}
	}
	if len(regions) == 1 {
		for region := range regions {
			return region
		}
	}
	for region := range phonenumbers.GetSupportedRegions() {
		parsed, _ := language.ParseRegion(region)
		if questionCountryMatches(SnapField{Label: display.English.Regions().Name(parsed)}, country) {
			regions[region] = true
		}
	}
	if len(regions) == 1 {
		for region := range regions {
			return region
		}
	}
	return ""
}

// A national number is interpreted using its declared contact country, then
// validated against phone metadata. An explicit international number/prefix
// determines its own region, including shared calling codes such as +1.
func parseProfilePhone(profile *domain.ResumeProfile, options []formFieldOption) (*phonenumbers.PhoneNumber, error) {
	if profile == nil {
		return nil, fmt.Errorf("saved phone information is missing")
	}
	pi := profile.PersonalInformation
	raw := strings.TrimSpace(pi.Phone)
	if raw == "" {
		return nil, fmt.Errorf("saved phone number is missing")
	}
	prefix := strings.TrimSpace(pi.PhonePrefix)
	if prefix != "" && !strings.HasPrefix(raw, "+") {
		raw = prefix + " " + raw
	}
	number, err := phonenumbers.Parse(raw, profilePhoneRegion(pi.Country, options))
	if err != nil || !phonenumbers.IsValidNumber(number) {
		return nil, fmt.Errorf("saved phone number cannot be validated with its contact country")
	}
	if prefix != "" {
		expected := strings.TrimPrefix(strings.ReplaceAll(prefix, " ", ""), "+")
		if expected != strconv.Itoa(int(number.GetCountryCode())) {
			return nil, fmt.Errorf("saved phone prefix conflicts with the phone number")
		}
	}
	return number, nil
}

func resolvePhoneCallingCode(field SnapField, profile *domain.ResumeProfile) FieldResolution {
	pending := FieldResolution{Ref: field.Ref, Source: SourceReview, Action: ActionSkip, Pending: field.Required, Reason: PendingMissingProfile}
	var options []formFieldOption
	if field.BrowserField != nil {
		options = field.BrowserField.Options
	} else {
		for i, label := range field.Options {
			value := ""
			if i < len(field.OptionValues) {
				value = field.OptionValues[i]
			}
			options = append(options, formFieldOption{Label: label, Value: value})
		}
	}
	number, err := parseProfilePhone(profile, options)
	if err != nil {
		return pending
	}
	region := phonenumbers.GetRegionCodeForNumber(number)
	var matches []formFieldOption
	for _, option := range options {
		candidate, code, valid := optionPhoneRegion(option)
		if valid && candidate == region && code == int(number.GetCountryCode()) {
			matches = append(matches, option)
		}
	}
	if len(matches) != 1 {
		pending.Reason = PendingAmbiguous
		return pending
	}
	return FieldResolution{Ref: field.Ref, Source: SourceProfile, Action: ActionFill, Value: matches[0].Label, IntentType: FieldIntentProfile, BindingKey: "personal_information.phone"}
}

func compositePhoneResolution(field SnapField, snap StepSnapshot, profile *domain.ResumeProfile) (FieldResolution, bool) {
	label := strings.Trim(countryWords(field.Label), " ")
	if !simpleCandidateQuestion(label) || !matchesAny(label, "phone", "mobile", "telephone") || field.Type != SnapTypeText || manualDeclaration(field) || personalBindingContextIsQualified(field.Label+" "+field.Group+" "+field.HelpText) {
		return FieldResolution{}, false
	}
	for _, other := range snap.Fields {
		if !isPhoneCallingCodeField(other) {
			continue
		}
		var options []formFieldOption
		if other.BrowserField != nil {
			options = other.BrowserField.Options
		}
		number, err := parseProfilePhone(profile, options)
		if err != nil || number.GetExtension() != "" {
			return FieldResolution{}, false
		}
		return FieldResolution{Ref: field.Ref, Source: SourceProfile, Action: ActionFill, Value: phonenumbers.GetNationalSignificantNumber(number), IntentType: FieldIntentProfile, BindingKey: "personal_information.phone"}, true
	}
	return FieldResolution{}, false
}
