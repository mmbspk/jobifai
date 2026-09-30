package validators

import (
	"regexp"
	"strings"
)

var reYearsPhrase = regexp.MustCompile(`(?i)\b(\d+)\s*(?:years?|yrs?\.?)\b`)

func matchExactOption(got, want string) bool {
	return strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(want))
}

func matchQualification(got, level, field string) bool {
	g := strings.ToLower(strings.TrimSpace(got))
	if level != "" && !strings.Contains(g, strings.ToLower(level)) {
		return false
	}
	if field != "" && !strings.Contains(g, strings.ToLower(field)) {
		return false
	}
	return level != "" || field != ""
}

func matchContainsFact(got, fact string) bool {
	return strings.Contains(strings.ToLower(got), strings.ToLower(strings.TrimSpace(fact)))
}

func matchBooleanYes(got string) bool {
	g := strings.ToLower(strings.TrimSpace(got))
	if g == "yes" || strings.HasPrefix(g, "yes,") || strings.HasPrefix(g, "yes ") {
		return true
	}
	return strings.Contains(g, "require") && strings.Contains(g, "sponsor")
}

func matchBooleanNo(got string) bool {
	g := strings.ToLower(strings.TrimSpace(got))
	if g == "no" || strings.HasPrefix(g, "no,") || strings.HasPrefix(g, "no ") {
		return true
	}
	return strings.Contains(g, "do not require") || strings.Contains(g, "don't require") ||
		strings.Contains(g, "not require") || strings.Contains(g, "no sponsorship")
}

func matchDuration(got, want string) bool {
	wantNorm := normalizeDuration(want)
	g := strings.ToLower(got)
	return strings.Contains(g, wantNorm)
}

func normalizeDuration(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

func matchCurrencyAmount(got, want string) bool {
	wantDigits := digitsOnly(want)
	if wantDigits == "" {
		return false
	}
	return strings.Contains(digitsOnly(got), wantDigits)
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func answerMatchesYearsCount(answer, wantYears string) bool {
	want := strings.TrimSpace(wantYears)
	if want == "" {
		return false
	}
	ans := strings.TrimSpace(answer)
	if ans == want {
		return true
	}
	lower := strings.ToLower(ans)
	if m := reYearsPhrase.FindStringSubmatch(lower); len(m) >= 2 && m[1] == want {
		return true
	}
	if regexp.MustCompile(`^\d{4}$`).MatchString(ans) {
		return false
	}
	return false
}
