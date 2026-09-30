package validators

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	reYearsPhrase = regexp.MustCompile(`(?i)\b(\d+)\s*(?:years?|yrs?\.?)\b`)
	reDuration    = regexp.MustCompile(`(?i)\b(\d+|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\s*(weeks?|months?|days?)\b`)
	reAmounts     = regexp.MustCompile(`\d{1,3}(?:,\d{3})+|\d+`)
)

type sponsorshipClass int

const (
	sponsorshipUnknown sponsorshipClass = iota
	sponsorshipYes
	sponsorshipNo
	sponsorshipContradictory
)

func normalizeAnswer(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "’", "'")
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' || r == ',' || r == '.' || r == '$' || r == '-' || r == '\'' {
			b.WriteRune(r)
			prevSpace = r == ' '
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func classifySponsorship(got string) sponsorshipClass {
	g := normalizeAnswer(got)
	if g == "" {
		return sponsorshipUnknown
	}
	yesSignals := sponsorshipYesSignals(g)
	noSignals := sponsorshipNoSignals(g)
	if yesSignals && noSignals {
		return sponsorshipContradictory
	}
	if noSignals {
		return sponsorshipNo
	}
	if yesSignals {
		return sponsorshipYes
	}
	return sponsorshipUnknown
}

func sponsorshipYesSignals(g string) bool {
	if g == "yes" || strings.HasPrefix(g, "yes,") || strings.HasPrefix(g, "yes ") {
		return true
	}
	if strings.Contains(g, "will require") && strings.Contains(g, "sponsor") {
		return true
	}
	if strings.Contains(g, "would need") && strings.Contains(g, "sponsor") {
		return true
	}
	if strings.Contains(g, "require") && strings.Contains(g, "sponsor") && !sponsorshipNegatedRequire(g) {
		return true
	}
	if strings.Contains(g, "need") && strings.Contains(g, "sponsor") && !sponsorshipNegatedRequire(g) {
		return true
	}
	return false
}

func sponsorshipNoSignals(g string) bool {
	if g == "no" || strings.HasPrefix(g, "no,") || strings.HasPrefix(g, "no ") {
		return true
	}
	if sponsorshipNegatedRequire(g) {
		return true
	}
	if strings.Contains(g, "no sponsorship") {
		return true
	}
	return false
}

func sponsorshipNegatedRequire(g string) bool {
	patterns := []string{
		"do not require", "don't require", "does not require", "did not require",
		"will not require", "would not require", "cannot require", "can't require",
		"do not need", "don't need", "does not need", "will not need", "would not need",
		"not require sponsorship", "not require employer sponsorship",
		"no sponsorship is required", "no sponsorship required",
	}
	for _, p := range patterns {
		if strings.Contains(g, p) {
			return true
		}
	}
	return false
}

func matchBooleanYes(got string) bool {
	return classifySponsorship(got) == sponsorshipYes
}

func matchBooleanNo(got string) bool {
	return classifySponsorship(got) == sponsorshipNo
}

type durationValue struct {
	n    int
	unit string
}

func wordNumber(s string) (int, bool) {
	switch strings.ToLower(s) {
	case "one":
		return 1, true
	case "two":
		return 2, true
	case "three":
		return 3, true
	case "four":
		return 4, true
	case "five":
		return 5, true
	case "six":
		return 6, true
	case "eight":
		return 8, true
	case "seven":
		return 7, true
	case "nine":
		return 9, true
	case "ten":
		return 10, true
	case "eleven":
		return 11, true
	case "twelve":
		return 12, true
	default:
		n, err := strconv.Atoi(s)
		return n, err == nil
	}
}

func parseDuration(s string) (durationValue, bool) {
	g := normalizeAnswer(s)
	m := reDuration.FindStringSubmatch(g)
	if len(m) < 3 {
		return durationValue{}, false
	}
	n, ok := wordNumber(m[1])
	if !ok {
		return durationValue{}, false
	}
	unit := strings.TrimSuffix(strings.ToLower(m[2]), "s")
	switch unit {
	case "week", "month", "day":
	default:
		return durationValue{}, false
	}
	return durationValue{n: n, unit: unit}, true
}

func matchDuration(got, want string) bool {
	wantD, ok := parseDuration(want)
	if !ok {
		return false
	}
	gotD, ok := parseDuration(got)
	if !ok {
		return false
	}
	return gotD.n == wantD.n && gotD.unit == wantD.unit
}

func parseAmountValues(s string) []int64 {
	var out []int64
	for _, m := range reAmounts.FindAllString(s, -1) {
		d := digitsOnly(m)
		if d == "" {
			continue
		}
		n, err := strconv.ParseInt(d, 10, 64)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}

func primaryAmount(want string) (int64, bool) {
	vals := parseAmountValues(want)
	if len(vals) == 0 {
		return 0, false
	}
	return vals[0], true
}

func matchCurrencyAmount(got, want string) bool {
	wantVal, ok := primaryAmount(want)
	if !ok {
		return false
	}
	for _, v := range parseAmountValues(got) {
		if v == wantVal {
			return true
		}
	}
	return false
}

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
