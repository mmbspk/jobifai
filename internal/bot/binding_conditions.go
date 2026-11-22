package bot

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var bindingCurrencyPattern = regexp.MustCompile(`(?i)(?:^|[^a-z])(AUD|USD|EUR|GBP|NZD|CAD|CHF|INR|JPY)(?:$|[^a-z])`)

// Verification approves a binding, not an incompatible future candidate value.
// Keep unit changes, choice changes and declared control limits local on replay.
func bindingValueCompatible(field SnapField, binding, value string) bool {
	if strings.HasPrefix(binding, "personal_information.") && personalBindingContextIsQualified(field.Label+" "+field.Group+" "+field.HelpText) {
		return false
	}
	if isWorkAuthorizationQuestion(field.Label+" "+field.Group+" "+field.HelpText) && !strings.HasPrefix(binding, "application_defaults.form_answers.") {
		return false
	}
	if field.MaxChars > 0 && utf8.RuneCountInString(value) > field.MaxChars {
		return false
	}
	if field.Type == SnapTypeNumber {
		number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return false
		}
		base := 0.0
		if minimum, err := strconv.ParseFloat(field.MinValue, 64); err == nil {
			if number < minimum {
				return false
			}
			base = minimum
		}
		if maximum, err := strconv.ParseFloat(field.MaxValue, 64); err == nil && number > maximum {
			return false
		}
		// Without an explicit minimum, HTML may derive its step base from the
		// initial applicant value. Keep that private and defer to checkValidity.
		if field.InputType == "number" && field.MinValue != "" && field.StepValue != "any" {
			step, err := strconv.ParseFloat(field.StepValue, 64)
			if err != nil || step <= 0 || math.IsInf(step, 0) || math.IsNaN(step) {
				step = 1
			}
			units := (number - base) / step
			if math.Abs(units-math.Round(units)) > 1e-7 {
				return false
			}
		}
	}
	if field.Type == SnapTypeSelect || field.Type == SnapTypeRadio || field.Type == SnapTypeCheckbox {
		choices := field.Options
		if field.Truncated && field.BrowserField != nil {
			choices = make([]string, len(field.BrowserField.Options))
			for i, option := range field.BrowserField.Options {
				choices[i] = option.Label
			}
		}
		if !containsOptionIgnoreCase(choices, value) {
			return false
		}
	}
	if binding == "application_defaults.salary_expectation" {
		contextText := strings.ToLower(field.Label + " " + field.Group + " " + field.HelpText)
		currency := func(text string) string {
			if match := bindingCurrencyPattern.FindStringSubmatch(text); len(match) > 1 {
				return strings.ToUpper(match[1])
			}
			if strings.Contains(text, "€") {
				return "EUR"
			}
			if strings.Contains(text, "£") {
				return "GBP"
			}
			return ""
		}
		expected, current := currency(contextText), currency(value)
		if expected != "" && current != "" && expected != current {
			return false
		}
		period := func(text string) string {
			switch {
			case matchesAny(text, "per month", "monthly"):
				return "month"
			case matchesAny(text, "per hour", "hourly"):
				return "hour"
			case matchesAny(text, "per week", "weekly"):
				return "week"
			case matchesAny(text, "annual", "yearly", "per year"):
				return "year"
			}
			return ""
		}
		expectedPeriod, currentPeriod := period(contextText), period(strings.ToLower(value))
		if expectedPeriod != "" && currentPeriod != "" && expectedPeriod != currentPeriod {
			return false
		}
	}
	return true
}
