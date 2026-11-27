package bot

import (
	"strconv"
	"strings"
)

func workRightEvidencePath(entry ProfileEvidence) (string, string, bool) {
	parts := strings.Split(strings.TrimPrefix(entry.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "application_defaults" || parts[1] != "work_rights" {
		return "", "", false
	}
	index, err := strconv.Atoi(parts[2])
	if err != nil || index < 0 {
		return "", "", false
	}
	return "/application_defaults/work_rights/" + parts[2], parts[3], true
}

func workRightWords(value string) string {
	return countryWords(strings.ReplaceAll(value, "_", " "))
}

func compatibleResidentAuthorityStatus(value string) bool {
	switch workRightWords(value) {
	case "yes", "true", "authorised", "authorized", "permanent resident", "permanent residency", "australian permanent resident":
		return true
	}
	return false
}

func permanentResidentVisaValue(value string) bool {
	switch workRightWords(value) {
	case "pr", "p r", "permanent resident", "permanent residency", "permanent resident visa":
		return true
	}
	return false
}

func structuredPermanentResidentVisa(entry ProfileEvidence) bool {
	_, field, known := workRightEvidencePath(entry)
	if !known || field != "visa_type" {
		return false
	}
	return permanentResidentVisaValue(entry.Quote)
}

func permanentResidentStatusValue(value string) bool {
	switch workRightWords(value) {
	case "permanent resident", "permanent residency", "australian permanent resident":
		return true
	}
	return false
}

func typedWorkRightStatusQuote(value string) bool {
	switch workRightWords(value) {
	case "yes", "true", "no", "false", "citizen", "australian citizen", "permanent resident", "permanent residency", "australian permanent resident", "nz citizen", "new zealand citizen":
		return true
	}
	return false
}

func sameWorkRightStatusFact(actual, quote string, field SnapField) bool {
	actual, quote = workRightWords(actual), workRightWords(quote)
	if actual == quote {
		return true
	}
	if permanentResidentStatusValue(actual) && permanentResidentStatusValue(quote) {
		return (!strings.Contains(actual, "australian") && !strings.Contains(quote, "australian")) || questionCountryMatches(field, "Australia")
	}
	if actual == "australian citizen" && quote == "citizen" {
		return questionCountryMatches(field, "Australia")
	}
	return actual == "new zealand citizen" && quote == "nz citizen" || actual == "nz citizen" && quote == "new zealand citizen"
}

func hasStructuredPermanentResidentEvidence(evidence []ProfileEvidence) bool {
	for _, entry := range evidence {
		_, key, known := workRightEvidencePath(entry)
		if structuredPermanentResidentVisa(entry) || known && key == "status" && permanentResidentStatusValue(entry.Quote) {
			return true
		}
	}
	return false
}

func hasNZCitizenEvidence(evidence []ProfileEvidence) bool {
	for _, entry := range evidence {
		_, key, known := workRightEvidencePath(entry)
		if known && key == "status" {
			value := workRightWords(entry.Quote)
			if value == "nz citizen" || value == "new zealand citizen" {
				return true
			}
		}
	}
	return false
}

func hasAustralianCitizenEvidence(evidence []ProfileEvidence) bool {
	for _, entry := range evidence {
		_, key, known := workRightEvidencePath(entry)
		if known && key == "status" {
			value := workRightWords(entry.Quote)
			if value == "citizen" || value == "australian citizen" {
				return true
			}
		}
	}
	return false
}

func selectableWorkRightChoice(field SnapField, answer string) string {
	if (field.Type != SnapTypeSelect && field.Type != SnapTypeRadio) || !questionCountryMatches(field, "Australia") {
		return ""
	}
	label, unique := currentOptionTypography(field, answer)
	if !unique {
		return ""
	}
	label = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(label, "’", "'")))
	for _, prefix := range []string{"i'm a ", "i'm an ", "i am a ", "i am an ", "a "} {
		if strings.HasPrefix(label, prefix) {
			label = strings.TrimPrefix(label, prefix)
			break
		}
	}
	return strings.Join(strings.Fields(label), " ")
}

func workRightAlternativeKind(field SnapField, answer string) string {
	switch selectableWorkRightChoice(field, answer) {
	case "permanent resident and/or nz citizen", "permanent resident or nz citizen", "permanent resident and/or new zealand citizen", "permanent resident or new zealand citizen":
		return "resident_or_nz"
	case "australian citizen or permanent resident", "australian citizen or a permanent resident", "australian citizen and/or permanent resident", "australian citizen and/or a permanent resident":
		return "citizen_or_resident"
	}
	return ""
}

// Selecting one supported branch of these exact current categories does not
// assert the other branch. No AND-only or free-text claim receives this rule.
func supportedWorkRightAlternativeChoice(field SnapField, answer string, evidence []ProfileEvidence) bool {
	switch workRightAlternativeKind(field, answer) {
	case "resident_or_nz":
		return hasStructuredPermanentResidentEvidence(evidence) || hasNZCitizenEvidence(evidence)
	case "citizen_or_resident":
		return hasStructuredPermanentResidentEvidence(evidence) || hasAustralianCitizenEvidence(evidence)
	}
	return false
}

func unsupportedResidentConjunction(field SnapField, answer string, evidence []ProfileEvidence) bool {
	switch selectableWorkRightChoice(field, answer) {
	case "permanent resident and nz citizen", "permanent resident and new zealand citizen":
		return !hasStructuredPermanentResidentEvidence(evidence) || !hasNZCitizenEvidence(evidence)
	}
	return false
}

func residentAnswerCompatible(field SnapField, answer string, evidence []ProfileEvidence) bool {
	words := workRightWords(answer)
	if words == "yes" || words == "no" {
		return true
	}
	if supportedWorkRightAlternativeChoice(field, answer, evidence) {
		return true
	}
	return strings.Contains(words, "permanent resident") && !matchesAny(words,
		"citizen", "not a permanent", "not an australian permanent", "temporary",
		"family", "partner", "graduate", "holiday", "sponsorship", "student", "protection", "regional")
}

func structuredAuthorizationEvidence(entry ProfileEvidence, field SnapField, answer string, evidence []ProfileEvidence) bool {
	_, key, known := workRightEvidencePath(entry)
	if !known || key != "status" {
		return false
	}
	// A generic authorization flag supports a plain authorization answer, not
	// an invented visa, residency, citizenship or restriction category.
	value := workRightWords(entry.Quote)
	if value == "yes" || value == "true" || value == "no" || value == "false" {
		answer = workRightWords(answer)
		return answer == "yes" || answer == "no"
	}
	return (value == "permanent resident" || value == "permanent residency" || value == "australian permanent resident") && residentAnswerCompatible(field, answer, evidence)
}
