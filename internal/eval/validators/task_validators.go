package validators

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"github.com/user/jobifai/internal/domain"
)

type visionField struct {
	Type     string   `json:"type"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

type visionExpect struct {
	Fields []visionField `json:"fields"`
}

func validateFormVision(output string, expect json.RawMessage, critical bool) Result {
	var exp visionExpect
	_ = json.Unmarshal(expect, &exp)
	res := Result{Metrics: map[string]any{}}
	got, err := parseVisionOutput(output)
	if err != nil {
		res.Errors = append(res.Errors, err.Error())
		if critical {
			res.CriticalFail = true
		}
		return res
	}
	expNorm := normalizeVisionFields(exp.Fields)
	gotNorm := normalizeVisionFields(got)
	tp, fp, fn := matchFields(expNorm, gotNorm)
	prec, rec := precisionRecall(tp, fp, fn)
	res.Metrics["field_tp"] = tp
	res.Metrics["field_fp"] = fp
	res.Metrics["field_fn"] = fn
	res.Metrics["field_precision"] = prec
	res.Metrics["field_recall"] = rec
	res.Metrics["field_type_accuracy"] = typeAccuracy(expNorm, gotNorm)
	res.Pass = fp == 0 && fn == 0
	if !res.Pass && critical {
		res.CriticalFail = true
	}
	if res.Pass {
		res.DeterministicScore = 1
	} else {
		res.DeterministicScore = prec
	}
	return res
}

func parseVisionOutput(output string) ([]visionField, error) {
	raw := stripJSON(output)
	var arr []visionField
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil, fmt.Errorf("invalid vision json array")
	}
	return arr, nil
}

func normalizeVisionFields(in []visionField) []visionField {
	out := make([]visionField, len(in))
	for i, f := range in {
		f.Type = strings.ToLower(strings.TrimSpace(f.Type))
		f.Question = normLabel(f.Question)
		opts := make([]string, len(f.Options))
		for j, o := range f.Options {
			opts[j] = normLabel(o)
		}
		f.Options = opts
		out[i] = f
	}
	return out
}

func normLabel(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.ToLower(s)
}

func matchFields(exp, got []visionField) (tp, fp, fn int) {
	used := map[int]bool{}
	for _, e := range exp {
		found := false
		for i, g := range got {
			if used[i] {
				continue
			}
			if e.Type == g.Type && e.Question == g.Question && optionsMatch(e.Options, g.Options) {
				tp++
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			fn++
		}
	}
	for i := range got {
		if !used[i] {
			fp++
		}
	}
	return tp, fp, fn
}

func optionsMatch(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
	 return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func typeAccuracy(exp, got []visionField) float64 {
	if len(exp) == 0 {
		return 1
	}
	_, _, fn := matchFields(exp, got)
	// approximate: types among matched questions
	matched := len(exp) - fn
	if matched == 0 {
		return 0
	}
	return float64(matched) / float64(len(exp))
}

func precisionRecall(tp, fp, fn int) (float64, float64) {
	prec := 1.0
	if tp+fp > 0 {
		prec = float64(tp) / float64(tp+fp)
	}
	rec := 1.0
	if tp+fn > 0 {
		rec = float64(tp) / float64(tp+fn)
	}
	return prec, rec
}

type extractExpect struct {
	Personal struct {
		FullName string `json:"full_name"`
		Email    string `json:"email"`
	} `json:"personal"`
	Employers []struct {
		Company string `json:"company"`
		Title   string `json:"title"`
		Start   string `json:"start"`
		End     string `json:"end"`
	} `json:"employers"`
	Skills []string `json:"skills"`
}

func validateResumeExtract(output string, expect json.RawMessage, critical bool) Result {
	var exp extractExpect
	_ = json.Unmarshal(expect, &exp)
	res := Result{Metrics: map[string]any{}}
	var prof domain.ResumeProfile
	raw := stripJSON(output)
	if err := json.Unmarshal([]byte(raw), &prof); err != nil {
		res.Errors = append(res.Errors, "invalid ResumeProfile json")
		if critical {
			res.CriticalFail = true
		}
		return res
	}
	missing, wrong, invented := 0, 0, 0
	outText := strings.ToLower(raw)
	if exp.Personal.FullName != "" && !fieldPresent(outText, exp.Personal.FullName) {
		missing++
		res.Errors = append(res.Errors, "missing name")
	}
	if exp.Personal.Email != "" && !fieldPresent(outText, exp.Personal.Email) {
		missing++
		res.Errors = append(res.Errors, "missing email")
	}
	for _, e := range exp.Employers {
		if e.Company != "" && !fieldPresent(outText, e.Company) {
			missing++
			res.Errors = append(res.Errors, "missing employer")
		}
		if e.Title != "" {
			pos := positionAtCompany(prof, e.Company)
			if pos == "" && !fieldPresent(outText, e.Title) {
				missing++
				res.Errors = append(res.Errors, "missing title")
			} else if pos != "" && !titlesAlign(pos, e.Title) {
				wrong++
				res.Errors = append(res.Errors, "incorrect title")
			} else if pos == "" && fieldPresent(outText, e.Title) && !titleMatches(prof, e.Title) {
				wrong++
				res.Errors = append(res.Errors, "incorrect title")
			}
		}
		if e.Start != "" && !fieldPresent(outText, e.Start) {
			wrong++
			res.Errors = append(res.Errors, "incorrect start date")
		}
		if e.End != "" && !fieldPresent(outText, e.End) {
			wrong++
			res.Errors = append(res.Errors, "incorrect end date")
		}
	}
	for _, sk := range exp.Skills {
		if sk != "" && !fieldPresent(outText, sk) {
			missing++
			res.Errors = append(res.Errors, "missing skill")
		}
	}
	for _, ex := range prof.ExperienceDetails {
		if ex.Company != "" && !employerExpected(ex.Company, exp.Employers) {
			invented++
			res.Errors = append(res.Errors, "invented employer: "+ex.Company)
		}
	}
	res.Metrics["missing_facts"] = missing
	res.Metrics["incorrect_facts"] = wrong
	res.Metrics["invented_facts"] = invented
	res.Pass = missing == 0 && wrong == 0 && invented == 0
	if invented > 0 && critical {
		res.CriticalFail = true
	}
	if res.Pass {
		res.DeterministicScore = 1
	}
	return res
}

func employerExpected(company string, exp []struct {
	Company string `json:"company"`
	Title   string `json:"title"`
	Start   string `json:"start"`
	End     string `json:"end"`
}) bool {
	c := strings.ToLower(company)
	for _, e := range exp {
		if strings.Contains(c, strings.ToLower(e.Company)) {
			return true
		}
	}
	return false
}

type tailorExpect struct {
	ForbiddenTerms        []string `json:"forbidden_terms"`
	RequiredEmployers     []string `json:"required_employers"`
	RequiredPublications  []string `json:"required_publications"`
	PreserveName          string   `json:"preserve_name"`
}

func validateResumeTailoring(output string, expect json.RawMessage, critical bool) Result {
	var exp tailorExpect
	_ = json.Unmarshal(expect, &exp)
	res := Result{Metrics: map[string]any{}}
	raw := stripJSON(output)
	var prof domain.ResumeProfile
	if err := json.Unmarshal([]byte(raw), &prof); err != nil {
		res.Errors = append(res.Errors, "invalid ResumeProfile json")
		if critical {
			res.CriticalFail = true
		}
		return res
	}
	low := strings.ToLower(raw)
	for _, term := range exp.ForbiddenTerms {
		if term != "" && strings.Contains(low, strings.ToLower(term)) {
			res.Errors = append(res.Errors, "unsupported claim: "+term)
			if critical {
				res.CriticalFail = true
			}
		}
	}
	if exp.PreserveName != "" && !strings.Contains(low, strings.ToLower(exp.PreserveName)) {
		res.Errors = append(res.Errors, "name not preserved")
	}
	for _, pub := range exp.RequiredPublications {
		if pub != "" && !strings.Contains(low, strings.ToLower(pub)) {
			res.Errors = append(res.Errors, "missing publication: "+pub)
		}
	}
	for _, emp := range exp.RequiredEmployers {
		if emp != "" && !strings.Contains(low, strings.ToLower(emp)) {
			res.Errors = append(res.Errors, "missing employer: "+emp)
		}
	}
	res.Pass = len(res.Errors) == 0
	if res.Pass {
		res.DeterministicScore = 1
	}
	return res
}

type coverExpect struct {
	ForbiddenTerms []string `json:"forbidden_terms"`
	GroundedFacts  []string `json:"grounded_facts"`
	MinParagraphs  int      `json:"min_paragraphs"`
	MaxParagraphs  int      `json:"max_paragraphs"`
	MaxWords       int      `json:"max_words"`
}

var coverBanned = []string{
	"i am writing to apply", "i am excited to apply", "dynamic professional", "results-driven",
}

func validateCoverLetter(output string, expect json.RawMessage, critical bool) Result {
	var exp coverExpect
	_ = json.Unmarshal(expect, &exp)
	res := Result{Metrics: map[string]any{}}
	text := strings.TrimSpace(output)
	low := strings.ToLower(text)
	if strings.Contains(text, "—") || strings.Contains(text, "–") {
		res.Errors = append(res.Errors, "em dash present")
	}
	for _, b := range coverBanned {
		if strings.Contains(low, b) {
			res.Errors = append(res.Errors, "banned phrase: "+b)
		}
	}
	for _, b := range exp.ForbiddenTerms {
		if b != "" && strings.Contains(low, strings.ToLower(b)) {
			res.Errors = append(res.Errors, "forbidden term: "+b)
		}
	}
	for _, f := range exp.GroundedFacts {
		if f != "" && !strings.Contains(low, strings.ToLower(f)) {
			res.Errors = append(res.Errors, "missing grounded fact")
		}
	}
	paras := len(splitParagraphs(text))
	minP, maxP := exp.MinParagraphs, exp.MaxParagraphs
	if minP == 0 {
		minP = 3
	}
	if maxP == 0 {
		maxP = 4
	}
	if paras < minP || paras > maxP {
		res.Errors = append(res.Errors, fmt.Sprintf("expected %d-%d paragraphs, got %d", minP, maxP, paras))
	}
	if exp.MaxWords > 0 && wordCount(text) > exp.MaxWords {
		res.Errors = append(res.Errors, "too long")
	}
	if looksLikeLetterhead(text) {
		res.Errors = append(res.Errors, "header/address block")
	}
	res.Metrics["word_count"] = wordCount(text)
	res.Metrics["paragraphs"] = paras
	res.Pass = len(res.Errors) == 0
	if !res.Pass && critical {
		res.CriticalFail = true
	}
	if res.Pass {
		res.DeterministicScore = 1
	}
	return res
}

func splitParagraphs(s string) []string {
	parts := regexp.MustCompile(`\n\s*\n`).Split(s, -1)
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	if len(out) <= 1 {
		out = []string{s}
	}
	return out
}

func wordCount(s string) int {
	return len(strings.Fields(s))
}

func looksLikeLetterhead(s string) bool {
	low := strings.ToLower(s)
	return strings.HasPrefix(low, "dear hiring") && strings.Contains(low, "street")
}

type appQExpect struct {
	AnswerCount int `json:"answer_count"`
	Answers     []struct {
		Question   string `json:"question"`
		QuestionID string `json:"question_id"`
		Match      string `json:"match"`
		Value      string `json:"value"`
		Contains   string `json:"contains"`
		MaxWords   int    `json:"max_words"`
	} `json:"answers"`
}

func validateApplicationQuestions(output string, expect json.RawMessage, critical bool) Result {
	var exp appQExpect
	_ = json.Unmarshal(expect, &exp)
	res := Result{Metrics: map[string]any{}}
	raw := stripJSON(output)
	var got []struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		res.Errors = append(res.Errors, "invalid answers json")
		if critical {
			res.CriticalFail = true
		}
		return res
	}
	if exp.AnswerCount > 0 && len(got) != exp.AnswerCount {
		res.Errors = append(res.Errors, fmt.Sprintf("answer count %d want %d", len(got), exp.AnswerCount))
	}
	for i, rule := range exp.Answers {
		if i >= len(got) {
			res.Errors = append(res.Errors, "missing answer slot")
			continue
		}
		wantQ := strings.TrimSpace(rule.Question)
		if wantQ == "" {
			wantQ = strings.TrimSpace(rule.QuestionID)
		}
		if wantQ != "" && normLabel(got[i].Question) != normLabel(wantQ) {
			res.Errors = append(res.Errors, "question order/mapping mismatch")
		}
		ans := got[i].Answer
		switch rule.Match {
		case "exact":
			if strings.TrimSpace(ans) != strings.TrimSpace(rule.Value) {
				res.Errors = append(res.Errors, "exact mismatch")
			}
		case "contains":
			if rule.Contains != "" && !strings.Contains(strings.ToLower(ans), strings.ToLower(rule.Contains)) {
				res.Errors = append(res.Errors, "missing grounded fact in answer")
			}
		case "years_count":
			if !answerMatchesYearsCount(ans, rule.Value) {
				res.Errors = append(res.Errors, "years count mismatch")
			}
		}
		if rule.MaxWords > 0 && wordCount(ans) > rule.MaxWords {
			res.Errors = append(res.Errors, "answer too long")
		}
	}
	res.Pass = len(res.Errors) == 0
	if !res.Pass && critical {
		res.CriticalFail = true
	}
	if res.Pass {
		res.DeterministicScore = 1
	}
	return res
}

func fieldPresent(lowText, want string) bool {
	return strings.Contains(lowText, strings.ToLower(strings.TrimSpace(want)))
}

var yearsCountWord = regexp.MustCompile(`(?i)\b(\d+)\s*(?:years?|yrs?\.?)\b`)

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
	if m := yearsCountWord.FindStringSubmatch(lower); len(m) >= 2 && m[1] == want {
		return true
	}
	// Reject start-year answers (e.g. "2019") when the question asks for duration.
	if regexp.MustCompile(`^\d{4}$`).MatchString(ans) {
		return false
	}
	return false
}

func titleMatches(prof domain.ResumeProfile, wantTitle string) bool {
	want := strings.ToLower(strings.TrimSpace(wantTitle))
	for _, ex := range prof.ExperienceDetails {
		if strings.Contains(strings.ToLower(ex.Position), want) || strings.Contains(want, strings.ToLower(ex.Position)) {
			return true
		}
	}
	return false
}

func positionAtCompany(prof domain.ResumeProfile, company string) string {
	c := strings.ToLower(strings.TrimSpace(company))
	for _, ex := range prof.ExperienceDetails {
		if c != "" && strings.Contains(strings.ToLower(ex.Company), c) {
			return ex.Position
		}
	}
	return ""
}

func titlesAlign(got, want string) bool {
	g := strings.ToLower(strings.TrimSpace(got))
	w := strings.ToLower(strings.TrimSpace(want))
	return g == w || strings.Contains(g, w) || strings.Contains(w, g)
}

