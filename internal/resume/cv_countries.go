package resume

import (
	"regexp"
	"sort"
	"strings"
)

// These aliases identify the same named jurisdiction in an explicit claim.
// They do not turn an address, nationality label or work history into rights.
var cvJurisdictions = map[string][]string{
	"Australia":            {"Australia", "Australian"},
	"Austria":              {"Austria", "Austrian"},
	"Bangladesh":           {"Bangladesh", "Bangladeshi"},
	"Belgium":              {"Belgium", "Belgian"},
	"Brazil":               {"Brazil", "Brazilian"},
	"Canada":               {"Canada", "Canadian"},
	"China":                {"China", "Chinese", "People's Republic of China"},
	"Denmark":              {"Denmark", "Danish"},
	"Finland":              {"Finland", "Finnish"},
	"France":               {"France", "French"},
	"Germany":              {"Germany", "German"},
	"India":                {"India", "Indian"},
	"Ireland":              {"Ireland", "Irish"},
	"Italy":                {"Italy", "Italian"},
	"Japan":                {"Japan", "Japanese"},
	"Kenya":                {"Kenya", "Kenyan"},
	"Lithuania":            {"Lithuania", "Lithuanian"},
	"Netherlands":          {"Netherlands", "Dutch"},
	"New Zealand":          {"New Zealand", "New Zealander", "NZ"},
	"Nigeria":              {"Nigeria", "Nigerian"},
	"Norway":               {"Norway", "Norwegian"},
	"Pakistan":             {"Pakistan", "Pakistani"},
	"Poland":               {"Poland", "Polish"},
	"Portugal":             {"Portugal", "Portuguese"},
	"Singapore":            {"Singapore", "Singaporean"},
	"South Africa":         {"South Africa", "South African"},
	"Spain":                {"Spain", "Spanish"},
	"Sweden":               {"Sweden", "Swedish"},
	"Switzerland":          {"Switzerland", "Swiss"},
	"United Arab Emirates": {"United Arab Emirates", "UAE", "Emirati"},
	"United Kingdom":       {"United Kingdom", "UK", "U.K.", "British"},
	"United States":        {"United States", "United States of America", "US", "U.S.", "USA", "U.S.A."},
}

var (
	cvMixedCitizenship  = cvCompoundCitizenshipMatcher(`\s+(?:citizen|permanent[- ]resident)\s+(?:of|in)\s+(?:the\s+)?`)
	cvSharedCitizenship = cvCompoundCitizenshipMatcher(`\s+(?:and|or|/)\s+`, `\s+(?:citizen|permanent[- ]resident)`)
)

func cvCompoundCitizenshipMatcher(between string, suffix ...string) *regexp.Regexp {
	var parts []string
	for _, aliases := range cvJurisdictions {
		for _, alias := range aliases {
			parts = append(parts, regexp.QuoteMeta(alias))
		}
	}
	sort.Slice(parts, func(i, j int) bool { return len(parts[i]) > len(parts[j]) })
	part := `(?:` + strings.Join(parts, "|") + `)`
	ending := ""
	if len(suffix) > 0 {
		ending = suffix[0]
	}
	return regexp.MustCompile(`(?i)(?:^|[^\pL\pN])(` + part + `)` + between + `(` + part + `)` + ending + `(?:$|[^\pL\pN])`)
}

func cvCompoundCitizenshipFor(text, country string, matcher *regexp.Regexp) bool {
	canonical := cvCanonicalCountry(country)
	for _, match := range matcher.FindAllStringSubmatch(text, -1) {
		first, second := cvCanonicalCountry(match[1]), cvCanonicalCountry(match[2])
		if first != second && (first == canonical || second == canonical) {
			return true
		}
	}
	return false
}

func cvCanonicalCountry(country string) string {
	// Accept schema country codes without treating short ambiguous tokens in
	// prose (for example CA as California) as jurisdiction evidence.
	switch strings.ToUpper(strings.TrimSpace(country)) {
	case "AU":
		return "Australia"
	case "CA":
		return "Canada"
	case "DE":
		return "Germany"
	}
	for canonical, aliases := range cvJurisdictions {
		for _, alias := range aliases {
			if strings.EqualFold(strings.TrimSpace(country), alias) {
				return canonical
			}
		}
	}
	return strings.TrimSpace(country)
}

func cvCountryPattern(country string) string {
	aliases := cvJurisdictions[cvCanonicalCountry(country)]
	if len(aliases) == 0 {
		aliases = []string{strings.TrimSpace(country)}
	}
	return cvAliasPattern(aliases)
}

func cvAliasPattern(aliases []string) string {
	parts := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		parts = append(parts, regexp.QuoteMeta(alias))
	}
	sort.Slice(parts, func(i, j int) bool { return len(parts[i]) > len(parts[j]) })
	return `(?:^|[^\pL\pN])(?:` + strings.Join(parts, "|") + `)(?:$|[^\pL\pN])`
}

func cvCountryIn(text, country string) bool {
	return strings.TrimSpace(country) != "" && regexp.MustCompile(`(?i)`+cvCountryPattern(country)).MatchString(text)
}

func cvCountryMatcher() *regexp.Regexp {
	var aliases []string
	for _, values := range cvJurisdictions {
		aliases = append(aliases, values...)
	}
	return regexp.MustCompile(`(?i)` + cvAliasPattern(aliases))
}

func cvCountryCount(text string) int {
	count := 0
	for canonical := range cvJurisdictions {
		if cvCountryIn(text, canonical) {
			count++
		}
	}
	return count
}

// cvCitizenshipClaim requires the country to qualify citizen/permanent resident
// directly. A separate address or another country's citizen clause cannot be
// used to manufacture a claim for the requested jurisdiction.
func cvCitizenshipClaim(text, country string) (negative, known bool) {
	aliases, knownCountry := cvJurisdictions[cvCanonicalCountry(country)]
	if !knownCountry {
		return false, false
	}
	if cvCompoundCitizenshipFor(text, country, cvMixedCitizenship) {
		return false, false
	}
	var parts []string
	for _, alias := range aliases {
		parts = append(parts, regexp.QuoteMeta(alias))
	}
	sort.Slice(parts, func(i, j int) bool { return len(parts[i]) > len(parts[j]) })
	alias := `(?:` + strings.Join(parts, "|") + `)`
	claim := regexp.MustCompile(`(?i)(?:^|[^\pL\pN])(?:` + alias + `\s+(?:citizen|permanent[- ]resident)|citizen\s+of\s+(?:the\s+)?` + alias + `|permanent[- ]resident\s+(?:of|in)\s+(?:the\s+)?` + alias + `)(?:$|[^\pL\pN])`)
	bounds := claim.FindAllStringIndex(text, -1)
	if len(bounds) != 1 {
		return false, false
	}
	// Negation is evaluated within the claim's clause, so an explicit No for
	// Germany does not negate a separate Australian citizen declaration.
	prefix := text[:bounds[0][0]]
	clauseStart := strings.LastIndexAny(prefix, ";,.") + 1
	joins := regexp.MustCompile(`(?i)\b(?:and|but)\b`).FindAllStringIndex(prefix, -1)
	if len(joins) > 0 && joins[len(joins)-1][1] > clauseStart {
		clauseStart = joins[len(joins)-1][1]
	}
	clause := text[clauseStart:bounds[0][1]]
	if regexp.MustCompile(`(?i)\b(?:spouse|partner|parent|mother|father|wife|husband|family|child|children|clients?)\b`).MatchString(clause) {
		return false, false
	}
	negative = cvNegated.MatchString(clause) || regexp.MustCompile(`(?i)\b(?:neither|nor|non[- ])`).MatchString(clause)
	return negative, true
}

func cvExplicitRightStatus(statement, status, country string) bool {
	if !cvExact(statement, status) || !cvCountryIn(status, country) || cvCountryCount(status) > 1 {
		return false
	}
	if statusNegative, citizenship := cvCitizenshipClaim(status, country); citizenship {
		sourceNegative, sourceKnown := cvCitizenshipClaim(statement, country)
		return sourceKnown && statusNegative == sourceNegative
	}
	return cvWorkRights.MatchString(status) && cvNegated.MatchString(statement) == cvNegated.MatchString(status)
}
