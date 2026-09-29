package formfill

import "strings"

// QuestionUsesDeterministicHeuristics reports whether production answers this
// question without calling Tailor.AnswerFormQuestion when profile data is available.
func QuestionUsesDeterministicHeuristics(question string, options []string) bool {
	lower := strings.ToLower(question)
	if len(options) == 0 && isContactQuestion(lower) {
		return true
	}
	if containsAny(lower, "sponsor", "visa status", "work permit", "require.*visa", "need.*sponsor") {
		return true
	}
	if len(options) == 0 && containsAny(lower, "how many years", "years of experience", "years experience", "years of work experience") {
		return true
	}
	if containsAny(lower, "authoris", "authoriz", "legally permitted", "eligible to work", "right to work", "legally able", "work without") {
		return true
	}
	return false
}

func isContactQuestion(lower string) bool {
	if containsAny(lower, "phone", "mobile", "cell", "telephone") &&
		!containsAny(lower, "years", "experience", "smartphone") {
		return true
	}
	if containsAny(lower, "email", "e-mail") {
		return true
	}
	if containsAny(lower, "first name", "last name", "full name", "your name") {
		return true
	}
	if containsAny(lower, "city", "suburb", "location", "address", "postcode", "postal", "zip code") &&
		!containsAny(lower, "years", "experience") {
		return true
	}
	return false
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
