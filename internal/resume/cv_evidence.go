package resume

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/user/jobifai/internal/domain"
)

const cvBindingPrefix = "application_defaults."

var cvTopicBindings = map[string][]string{
	"work_rights":         {"work_rights"},
	"sponsorship":         {"requires_sponsorship", "work_rights"},
	"notice_period":       {"notice_period"},
	"earliest_start_date": {"earliest_start_date", "availability"},
	"salary":              {"salary_range", "salary_expectation"},
	"relocation":          {"relocation"},
	// Contextual answers remain exact statements rather than invented yes/no
	// declarations or claims of a current licence.
	"travel":           {"travel_conditions"},
	"work_arrangement": {"commute_conditions"},
	"working_hours":    {"working_hours"},
	"credentials":      {"form_answers.credentials"},
}

var (
	cvHostileText = regexp.MustCompile(`(?i)ignore\s+(all|any|the|previous|prior)|system\s+prompt|(?:return|output|emit|generate)\s+(?:only\s+)?(?:json|true|false)|(?:invent|fabricate|pretend)|(?:set|populate|override)\s+(?:application|requires|work_rights)|(?:claim|declare|assume|answer|write|say|mark|fill)\s+(?:that\s+|yes|no|true|false|["']|I\s+)|instructions?\s*:`)
	cvUncertain   = regexp.MustCompile(`(?i)\b(may|might|possibly|perhaps|depending|subject to|if|conditional|pending|formerly|previously|expired|historical|until|currently|at present|for now|in future|future|only|unless|or|maximum|minimum|up to|at least)\b`)
	cvNegated     = regexp.MustCompile(`(?i)\b(no|not|never|cannot|can't|unable|without|don't|doesn't|do not)\b`)
	cvDate        = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	cvNotice      = regexp.MustCompile(`(?i)\bnotice\s+(?:period|requirement)|\b(?:weeks?|months?|days?)['’]?\s+notice\b`)
	cvWorkRights  = regexp.MustCompile(`(?i)\b(?:authori[sz]ed|eligible|entitled|permission|right)\s+(?:legally\s+)?(?:to\s+)?work\b|\bwork\s+(?:authori[sz]ation|rights?|permit|visa)\b`)
	cvSalary      = regexp.MustCompile(`(?i)\b(?:salary|pay|compensation|remuneration|rate)\b`)
	cvStart       = regexp.MustCompile(`(?i)\b(?:available|availability)(?:\s+to\s+(?:start|work)|\s+from|\s+immediately|\s*:)|\b(?:start date|start from|can start|able to start)\b`)
	cvCountry     = cvCountryMatcher()
)

// validateCVAnswers treats both the model's value and its citation as untrusted.
// It never uses inferred profile location, employment or qualifications as
// application evidence. Unsupported items are dropped; verified but qualified
// statements remain visible suggestions without hydrating profile defaults.
func validateCVAnswers(text string, rawItems []json.RawMessage, defaults *domain.ApplicationDefaults) []domain.CVAnswerPrefill {
	var out []domain.CVAnswerPrefill
	for i, raw := range rawItems {
		if i >= 20 {
			break
		}
		var item domain.CVAnswerPrefill
		if json.Unmarshal(raw, &item) != nil || !validCVBinding(item.TopicID, item.Binding) || len(item.Value) == 0 || bytes.Equal(bytes.TrimSpace(item.Value), []byte("null")) {
			continue
		}
		evidence, ok := cvEvidence(text, item.Evidence)
		if !ok {
			continue
		}
		item.Evidence = evidence
		statement := evidence[0].Quote
		// Multiple fragments must not create a synthetic declaration. The model
		// cites one complete statement; the app computes its real offsets.
		clear, valid := validateCVValue(item, statement)
		if !valid {
			continue
		}
		item.Value = cvRetainQualifiers(item.Binding, item.Value, statement)
		item.Source = "cv_evidence"
		item.ReviewState = "suggested"
		item.Warning = ""
		item.NeedsConfirmation = !clear || cvHasConflict(text, item, statement)
		if item.NeedsConfirmation {
			item.Source = "model_suggestion"
			item.Warning = "The CV statement has conditions, conflicting details or dates that need confirmation."
		}
		out = append(out, item)
	}
	// Contradictions also include separate values emitted for one scope.
	for i := range out {
		for j := range out {
			if i != j && cvScope(out[i]) == cvScope(out[j]) && !bytes.Equal(out[i].Value, out[j].Value) {
				out[i].NeedsConfirmation = true
				out[i].Source = "model_suggestion"
				out[i].Warning = "The CV contains conflicting answers for this topic."
			}
		}
	}
	for _, item := range out {
		if !item.NeedsConfirmation {
			applyCVValue(defaults, item)
		}
	}
	return out
}

func validCVBinding(topic, binding string) bool {
	for _, suffix := range cvTopicBindings[topic] {
		if binding == cvBindingPrefix+suffix {
			return true
		}
	}
	return false
}

func cvEvidence(text string, supplied []domain.CVAnswerEvidence) ([]domain.CVAnswerEvidence, bool) {
	if len(supplied) != 1 {
		return nil, false
	}
	quote := strings.TrimSpace(supplied[0].Quote)
	if len(quote) < 4 || len(quote) > 600 || strings.ContainsAny(quote, "\n\r\f") {
		return nil, false
	}
	start := strings.Index(text, quote)
	if start < 0 {
		return nil, false
	}
	// Expand to the actual source line, preventing clipped citations from
	// hiding a preceding negation or a following country/pay/date qualifier.
	lineStart := strings.LastIndexAny(text[:start], "\n\r\f") + 1
	lineEnd := len(text)
	if end := strings.IndexAny(text[start+len(quote):], "\n\r\f"); end >= 0 {
		lineEnd = start + len(quote) + end
	}
	line := strings.TrimSpace(text[lineStart:lineEnd])
	if len(line) > 600 || cvHostileText.MatchString(line) || strings.Contains(line, "?") {
		return nil, false
	}
	lineStart += strings.Index(text[lineStart:lineEnd], line)
	ref := domain.CVAnswerEvidence{Quote: line, Start: lineStart, End: lineStart + len(line)}
	if strings.Contains(text, "\f") {
		ref.Page = strings.Count(text[:lineStart], "\f") + 1
	}
	return []domain.CVAnswerEvidence{ref}, true
}

func validateCVValue(item domain.CVAnswerPrefill, statement string) (clear, valid bool) {
	clear = !cvUncertain.MatchString(statement)
	suffix := strings.TrimPrefix(item.Binding, cvBindingPrefix)
	switch suffix {
	case "requires_sponsorship":
		var value bool
		temporalRemoved := strings.NewReplacer("in the future", "", "in future", "", "for now", "").Replace(strings.ToLower(statement))
		if json.Unmarshal(item.Value, &value) != nil || cvCountry.MatchString(statement) || regexp.MustCompile(`(?i)\b(?:in|for|within|outside|except|from|to|across)\s+`).MatchString(temporalRemoved) {
			return false, false // a country declaration cannot create a global answer
		}
		actual, known := cvSponsorship(statement)
		return clear, known && actual == value
	case "work_rights":
		right, ok := cvSingleWorkRight(item.Value)
		if !ok || !cvCountryIn(statement, right.Country) || len(right.Country) < 2 {
			return false, false
		}
		if item.TopicID == "work_rights" && !cvExplicitRightStatus(statement, right.Status, right.Country) {
			return false, false
		}
		if right.Status != "" && !cvExplicitRightStatus(statement, right.Status, right.Country) {
			return false, false
		}
		if negative, citizenship := cvCitizenshipClaim(statement, right.Country); citizenship {
			// A denial of citizenship/PR does not establish denied work rights.
			// Historical, conditional or third-party claims also need review.
			if negative || cvCompoundCitizenshipFor(statement, right.Country, cvSharedCitizenship) || regexp.MustCompile(`(?i)\b(?:former|applied|applying|application|prospective|expected|lost|renounced|revoked|ceased)\b`).MatchString(statement) {
				clear = false
			}
		}
		for _, value := range []string{right.VisaType, right.VisaExpiryDate, right.WorkRestrictions, right.Details} {
			if value != "" && !cvExact(statement, value) {
				return false, false
			}
		}
		if right.RequiresSponsorship != nil {
			if cvCountryCount(statement) > 1 || regexp.MustCompile(`(?i)\b(?:but|except|or)\b`).MatchString(statement) {
				return false, false
			}
			actual, known := cvSponsorship(statement)
			if !known || actual != *right.RequiresSponsorship {
				return false, false
			}
		} else if item.TopicID == "sponsorship" {
			return false, false
		}
		if right.FutureRequiresSponsorship != nil {
			actual, known := cvSponsorship(statement)
			if !strings.Contains(strings.ToLower(statement), "future") || !known || actual != *right.FutureRequiresSponsorship {
				return false, false
			}
			clear = false
		}
		if right.VisaExpiryDate != "" {
			if _, err := time.Parse("2006-01-02", right.VisaExpiryDate); err != nil {
				return false, false
			}
		}
		if right.VisaExpiryDate != "" && !cvFutureDate(right.VisaExpiryDate) {
			clear = false
		}
		if cvDate.MatchString(statement) && (right.VisaExpiryDate == "" || cvDate.FindString(statement) != right.VisaExpiryDate) {
			clear = false
		}
		if regexp.MustCompile(`(?i)\b(?:restricted|restriction|hours?|limited)\b`).MatchString(statement) && right.WorkRestrictions == "" {
			clear = false
		}
		// Date/restriction language must remain visible, including details the
		// model omitted from its proposed structured value.
		return clear, true
	case "salary_range":
		var salary domain.ApplicationSalaryRange
		if json.Unmarshal(item.Value, &salary) != nil || !cvSalary.MatchString(statement) || (salary.Minimum == nil && salary.Maximum == nil) {
			return false, false
		}
		if !cvSinglePayScope(statement, salary) {
			return false, false
		}
		for _, amount := range []*float64{salary.Minimum, salary.Maximum} {
			if amount != nil && (*amount < 0 || !cvAmount(statement, *amount)) {
				return false, false
			}
		}
		if salary.Minimum != nil && salary.Maximum != nil && *salary.Minimum > *salary.Maximum {
			return false, false
		}
		if !cvExact(statement, salary.Currency) || len(salary.Currency) != 3 || !cvPayPeriod(statement, salary.PayPeriod) || !cvExact(statement, salary.Basis) || (salary.Basis != "base" && salary.Basis != "total") {
			return false, false
		}
		for _, value := range []string{salary.BenefitsDetails, salary.Details} {
			if value != "" && !cvExact(statement, value) {
				return false, false
			}
		}
		if salary.IncludesBenefits != nil {
			inclusion := strings.Contains(strings.ToLower(statement), "including") || strings.Contains(strings.ToLower(statement), "inclusive")
			exclusion := strings.Contains(strings.ToLower(statement), "excluding") || strings.Contains(strings.ToLower(statement), "exclusive")
			if inclusion == exclusion || inclusion != *salary.IncludesBenefits || salary.BenefitsDetails == "" {
				return false, false
			}
		}
		return clear, true
	case "relocation":
		var relocation domain.ApplicationRelocation
		if json.Unmarshal(item.Value, &relocation) != nil || relocation.Willing == nil {
			return false, false
		}
		actual, known := cvRelocation(statement)
		if !known || actual != *relocation.Willing {
			return false, false
		}
		for _, value := range append(relocation.Locations, relocation.Details) {
			if value != "" && !cvExact(statement, value) {
				return false, false
			}
		}
		return clear, true
	default:
		var value string
		if json.Unmarshal(item.Value, &value) != nil || !cvExact(statement, value) || cvNegated.MatchString(statement) != cvNegated.MatchString(value) {
			return false, false
		}
		switch suffix {
		case "notice_period":
			return clear, cvNotice.MatchString(statement) && regexp.MustCompile(`(?i)\b(?:\d+\s+(?:days?|weeks?|months?)|immediate(?:ly)?)\b`).MatchString(value)
		case "earliest_start_date":
			_, err := time.Parse("2006-01-02", value)
			return clear && cvFutureDate(value), err == nil && cvStart.MatchString(statement) && cvDate.MatchString(value) && len(value) == 10
		case "availability":
			return clear, cvStart.MatchString(statement) && cvFullStatement(value, statement)
		case "salary_expectation":
			// Unstructured pay remains an exact statement, retaining every unit.
			return clear, cvSalary.MatchString(statement) && cvFullStatement(value, statement)
		case "travel_conditions":
			return clear, regexp.MustCompile(`(?i)\b(?:willing|unwilling|able|unable|available|prepared)\s+to\s+travel|\btravel\s+(?:availability|willingness|preferences?)\s*:`).MatchString(statement) && cvFullStatement(value, statement)
		case "commute_conditions":
			return clear, regexp.MustCompile(`(?i)\b(remote|hybrid|on.site|office)\b`).MatchString(statement) && cvPreference(statement) && cvFullStatement(value, statement)
		case "working_hours":
			return clear, regexp.MustCompile(`(?i)\b(hours|shifts?|weekends?|part.time|full.time)\b`).MatchString(statement) && cvPreference(statement) && cvFullStatement(value, statement)
		case "form_answers.credentials":
			return false, regexp.MustCompile(`(?i)\b(licen[cs]e|certifi|registration|registered|accredit)`).MatchString(statement) && cvFullStatement(value, statement)
		}
	}
	return false, false
}

func cvSponsorship(statement string) (bool, bool) {
	s := strings.ToLower(statement)
	if regexp.MustCompile(`(?i)\b(?:company|employer|client|role|position|applicants)\s+(?:requires?|needs?)\b`).MatchString(s) {
		return false, false
	}
	negative := regexp.MustCompile(`(?:do not|don't|does not|doesn't|will not|won't|not|never)\s+(?:currently\s+)?(?:require|need)\s+(?:visa\s+|employer\s+)?sponsorship|no\s+(?:visa\s+|employer\s+)?sponsorship\s+(?:required|needed)|without\s+(?:visa\s+|employer\s+)?sponsorship`).MatchString(s)
	positive := regexp.MustCompile(`(?:require|need)\s+(?:visa\s+|employer\s+)?sponsorship|sponsorship\s+(?:is\s+)?(?:required|needed)`).MatchString(s)
	if negative {
		return false, true
	}
	return true, positive && !cvNegated.MatchString(s)
}

func cvPreference(statement string) bool {
	return regexp.MustCompile(`(?i)\b(?:prefer|preference|willing|unwilling|available|seeking|open to|work arrangement|working hours)\b`).MatchString(statement)
}

func cvRelocation(statement string) (bool, bool) {
	negative := cvNegated.MatchString(statement) || strings.Contains(strings.ToLower(statement), "unwilling")
	return !negative, regexp.MustCompile(`(?i)\b(?:willing|unwilling|open|able|unable|prepared)\s+(?:to\s+)?relocat|\b(?:can|cannot|can't|will|won't|would|wouldn't)\s+relocat`).MatchString(statement)
}

func cvExact(statement, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	pattern := regexp.QuoteMeta(value)
	if unicode.IsLetter([]rune(value)[0]) || unicode.IsDigit([]rune(value)[0]) {
		pattern = `(?:^|[^\pL\pN])` + pattern
	}
	runes := []rune(value)
	if unicode.IsLetter(runes[len(runes)-1]) || unicode.IsDigit(runes[len(runes)-1]) {
		pattern += `(?:$|[^\pL\pN])`
	}
	return regexp.MustCompile(`(?i)` + pattern).MatchString(statement)
}

func cvFullStatement(value, statement string) bool {
	return strings.TrimSpace(value) == strings.TrimSpace(statement)
}

func cvFutureDate(value string) bool {
	date, err := time.Parse("2006-01-02", value)
	return err == nil && !date.Before(time.Now().UTC().Truncate(24*time.Hour))
}

func cvPayPeriod(statement, period string) bool {
	patterns := map[string]string{"hour": `(?i)\b(?:per hour|hourly|an hour)\b|/h(?:our)?\b`, "week": `(?i)\b(?:per week|weekly|a week)\b|/week\b`, "month": `(?i)\b(?:per month|monthly|a month)\b|/month\b`, "year": `(?i)\b(?:per year|yearly|annually|annual|per annum|a year)\b|/year\b`}
	pattern := patterns[period]
	return pattern != "" && regexp.MustCompile(pattern).MatchString(statement)
}

func cvAmount(statement string, value float64) bool {
	// No k/m expansion, conversions or ranges inferred from unrelated numbers.
	numbers := regexp.MustCompile(`\d[\d,]*(?:\.\d+)?`).FindAllStringIndex(statement, -1)
	for _, bounds := range numbers {
		negative := bounds[0] > 0 && statement[bounds[0]-1] == '-' && (bounds[0] < 2 || !unicode.IsDigit(rune(statement[bounds[0]-2])))
		next, _ := utf8.DecodeRuneInString(statement[bounds[1]:])
		if negative || (bounds[0] > 0 && strings.ContainsRune("0123456789.,", rune(statement[bounds[0]-1]))) || (bounds[1] < len(statement) && (unicode.IsLetter(next) || strings.ContainsRune("0123456789.,", next))) {
			continue
		}
		number := statement[bounds[0]:bounds[1]]
		actual, err := strconv.ParseFloat(strings.ReplaceAll(number, ",", ""), 64)
		if err == nil && actual == value {
			return true
		}
	}
	return false
}

func cvSinglePayScope(statement string, salary domain.ApplicationSalaryRange) bool {
	for _, period := range []string{"hour", "week", "month", "year"} {
		if period != salary.PayPeriod && cvPayPeriod(statement, period) {
			return false
		}
	}
	// Mixed amounts/bases/currencies belong in one exact-text suggestion,
	// rather than pairing a number from one offer with another offer's units.
	if cvExact(statement, "base") && cvExact(statement, "total") {
		return false
	}
	currencies := regexp.MustCompile(`(?i)\b(?:USD|EUR|GBP|AUD|CAD|NZD|INR|PKR|CHF|SEK|NOK|DKK|SGD|AED|PLN|ZAR|JPY|CNY)\b`).FindAllString(statement, -1)
	for _, currency := range currencies {
		if !strings.EqualFold(currency, salary.Currency) {
			return false
		}
	}
	amounts := regexp.MustCompile(`\d[\d,]*(?:\.\d+)?`).FindAllString(statement, -1)
	expected := 0
	if salary.Minimum != nil {
		expected++
	}
	if salary.Maximum != nil {
		expected++
	}
	return len(amounts) == expected
}

func cvScope(item domain.CVAnswerPrefill) string {
	if strings.HasSuffix(item.Binding, "work_rights") {
		right, _ := cvSingleWorkRight(item.Value)
		return item.TopicID + "/" + strings.ToLower(cvCanonicalCountry(right.Country))
	}
	return item.TopicID
}

func cvHasConflict(text string, item domain.CVAnswerPrefill, statement string) bool {
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' || r == '\f' }) {
		line = strings.TrimSpace(line)
		if line == statement || len(line) > 600 || cvHostileText.MatchString(line) {
			continue
		}
		switch item.TopicID {
		case "sponsorship":
			if strings.HasSuffix(item.Binding, "work_rights") {
				right, _ := cvSingleWorkRight(item.Value)
				if !cvCountryIn(line, right.Country) {
					continue
				}
			}
			a, knownA := cvSponsorship(statement)
			b, knownB := cvSponsorship(line)
			if knownA && knownB && a != b {
				return true
			}
		case "notice_period":
			var value string
			_ = json.Unmarshal(item.Value, &value)
			if cvNotice.MatchString(line) && !cvExact(line, value) {
				return true
			}
		case "earliest_start_date":
			if cvStart.MatchString(line) && cvDate.MatchString(line) && cvDate.FindString(line) != cvDate.FindString(statement) {
				return true
			}
		case "salary":
			if cvSalary.MatchString(line) {
				return true
			}
		case "relocation":
			a, knownA := cvRelocation(statement)
			b, knownB := cvRelocation(line)
			if knownA && knownB && a != b {
				return true
			}
		case "work_rights":
			right, _ := cvSingleWorkRight(item.Value)
			if negative, known := cvCitizenshipClaim(line, right.Country); known {
				statusNegative, statusKnown := cvCitizenshipClaim(right.Status, right.Country)
				if statusKnown && negative != statusNegative {
					return true
				}
			}
			if cvWorkRights.MatchString(line) && cvCountryIn(line, right.Country) && cvNegated.MatchString(line) != cvNegated.MatchString(right.Status) {
				return true
			}
		}
	}
	return false
}

func applyCVValue(defaults *domain.ApplicationDefaults, item domain.CVAnswerPrefill) {
	suffix := strings.TrimPrefix(item.Binding, cvBindingPrefix)
	switch suffix {
	case "requires_sponsorship":
		_ = json.Unmarshal(item.Value, &defaults.RequiresSponsorship)
	case "notice_period":
		_ = json.Unmarshal(item.Value, &defaults.NoticePeriod)
	case "earliest_start_date":
		_ = json.Unmarshal(item.Value, &defaults.EarliestStartDate)
	case "availability":
		_ = json.Unmarshal(item.Value, &defaults.Availability)
	case "travel_conditions":
		_ = json.Unmarshal(item.Value, &defaults.TravelConditions)
	case "commute_conditions":
		_ = json.Unmarshal(item.Value, &defaults.CommuteConditions)
	case "working_hours":
		_ = json.Unmarshal(item.Value, &defaults.WorkingHours)
	case "salary_expectation":
		_ = json.Unmarshal(item.Value, &defaults.SalaryExpectation)
	case "salary_range":
		_ = json.Unmarshal(item.Value, &defaults.SalaryRange)
		defaults.SalaryRange.Details = item.Evidence[0].Quote
	case "relocation":
		_ = json.Unmarshal(item.Value, &defaults.Relocation)
		defaults.Relocation.Details = item.Evidence[0].Quote
	case "work_rights":
		right, _ := cvSingleWorkRight(item.Value)
		right.Details = item.Evidence[0].Quote
		for i := range defaults.WorkRights {
			current := &defaults.WorkRights[i]
			if !strings.EqualFold(cvCanonicalCountry(current.Country), cvCanonicalCountry(right.Country)) {
				continue
			}
			if right.Status != "" {
				current.Status = right.Status
			}
			if right.RequiresSponsorship != nil {
				current.RequiresSponsorship = right.RequiresSponsorship
			}
			if right.VisaType != "" {
				current.VisaType = right.VisaType
			}
			if right.VisaExpiryDate != "" {
				current.VisaExpiryDate = right.VisaExpiryDate
			}
			if right.WorkRestrictions != "" {
				current.WorkRestrictions = right.WorkRestrictions
			}
			if current.Details != right.Details {
				current.Details += " " + right.Details
			}
			return
		}
		defaults.WorkRights = append(defaults.WorkRights, right)
	default:
		key := strings.TrimPrefix(suffix, "form_answers.")
		if key != suffix {
			if defaults.FormAnswers == nil {
				defaults.FormAnswers = make(map[string]string)
			}
			var value string
			_ = json.Unmarshal(item.Value, &value)
			defaults.FormAnswers[key] = strings.TrimFunc(value, unicode.IsSpace)
		}
	}
}

func cvSingleWorkRight(raw json.RawMessage) (domain.ApplicationWorkRight, bool) {
	var rights []domain.ApplicationWorkRight
	if json.Unmarshal(raw, &rights) != nil || len(rights) != 1 {
		return domain.ApplicationWorkRight{}, false
	}
	return rights[0], true
}

func cvRetainQualifiers(binding string, raw json.RawMessage, statement string) json.RawMessage {
	var value any
	switch strings.TrimPrefix(binding, cvBindingPrefix) {
	case "work_rights":
		right, _ := cvSingleWorkRight(raw)
		right.Country = cvCanonicalCountry(right.Country)
		right.Details = statement
		value = []domain.ApplicationWorkRight{right}
	case "salary_range":
		var salary domain.ApplicationSalaryRange
		_ = json.Unmarshal(raw, &salary)
		salary.Details = statement
		value = salary
	case "relocation":
		var relocation domain.ApplicationRelocation
		_ = json.Unmarshal(raw, &relocation)
		relocation.Details = statement
		value = relocation
	default:
		return raw
	}
	normalized, _ := json.Marshal(value)
	return normalized
}
