package bot

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/user/jobifai/internal/domain"
)

const groundedAnswerVersion = 1
const groundedAnswerVerifier = "grounded-form-answer-v1"

var groundedNumberPattern = regexp.MustCompile(`[0-9]+`)

// ProfileEvidence identifies an exact excerpt of a current profile scalar. The
// model may propose references, but local code resolves and checks every one.
type ProfileEvidence struct {
	Path  string `json:"path"` // RFC 6901 JSON pointer into ResumeProfile
	Quote string `json:"quote"`
}

// groundedAnswerProof is created only after the independent verifier approves a
// locally supported answer. It is deliberately absent from the model's schema:
// JSON flags or a claimed provenance cannot grant fill or cache authority.
type groundedAnswerProof struct {
	profileDigest string
	fieldSig      string
	platform      string
	employer      string
	answerDigest  string
	evidence      []ProfileEvidence
}

type groundedCacheRecord struct {
	Version       int               `json:"version"`
	ProfileDigest string            `json:"profile_digest,omitempty"`
	FieldSig      string            `json:"field_sig"`
	AnswerDigest  string            `json:"answer_digest"`
	Evidence      []ProfileEvidence `json:"evidence,omitempty"`
}

// The public question shape stays compatible with existing review APIs. Cached
// evidence belongs to this candidate/issuer row, never to a shared question.
type groundedMappingConditions struct {
	SnapField
	Grounded *groundedCacheRecord `json:"grounded_answer,omitempty"`
}

func profileDigest(profile *domain.ResumeProfile) string {
	if profile == nil {
		return ""
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func groundedAnswerDigest(intent FieldIntentType, binding, value string) string {
	if intent == FieldIntentProfile {
		value = "" // a direct binding rereads the current candidate fact
	}
	raw := fmt.Sprintf("%d\x00%s\x00%s\x00%s", groundedAnswerVersion, intent, binding, value)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
}

func profileScalar(profile *domain.ResumeProfile, pointer string) (string, bool) {
	if profile == nil || !strings.HasPrefix(pointer, "/") {
		return "", false
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return "", false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	for _, part := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch node := value.(type) {
		case map[string]any:
			var exists bool
			value, exists = node[part]
			if !exists {
				return "", false
			}
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return "", false
			}
			value = node[i]
		default:
			return "", false
		}
	}
	switch scalar := value.(type) {
	case string:
		return scalar, strings.TrimSpace(scalar) != ""
	case bool:
		return strconv.FormatBool(scalar), true
	case float64:
		return strconv.FormatFloat(scalar, 'f', -1, 64), true
	}
	return "", false
}

func profileEvidenceList(profile *domain.ResumeProfile) []ProfileEvidence {
	raw, err := json.Marshal(profile)
	if err != nil {
		return nil
	}
	var root any
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}
	var evidence []ProfileEvidence
	var visit func(any, string)
	visit = func(value any, path string) {
		switch node := value.(type) {
		case map[string]any:
			keys := make([]string, 0, len(node))
			for key := range node {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				escaped := strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
				visit(node[key], path+"/"+escaped)
			}
		case []any:
			for i, item := range node {
				visit(item, path+"/"+strconv.Itoa(i))
			}
		case string:
			if strings.TrimSpace(node) != "" {
				evidence = append(evidence, ProfileEvidence{Path: path, Quote: node})
			}
		case bool:
			evidence = append(evidence, ProfileEvidence{Path: path, Quote: strconv.FormatBool(node)})
		case float64:
			evidence = append(evidence, ProfileEvidence{Path: path, Quote: strconv.FormatFloat(node, 'f', -1, 64)})
		}
	}
	visit(root, "")
	return evidence
}

func supportedProfileEvidence(profile *domain.ResumeProfile, evidence []ProfileEvidence) bool {
	if len(evidence) == 0 || len(evidence) > 8 {
		return false
	}
	seen := map[string]bool{}
	for _, e := range evidence {
		if strings.HasPrefix(e.Path, "/application_defaults/form_answers/mapping_") {
			return false // exact-question reviewer authority cannot be repurposed
		}
		value, exists := profileScalar(profile, e.Path)
		if !exists || strings.TrimSpace(e.Quote) == "" || len(e.Quote) > 8000 || !strings.Contains(value, e.Quote) || seen[e.Path+"\x00"+e.Quote] {
			return false
		}
		seen[e.Path+"\x00"+e.Quote] = true
	}
	return true
}

func manualDeclaration(field SnapField) bool {
	contextText := strings.ToLower(field.Label + " " + field.Group + " " + field.HelpText)
	return matchesAny(contextText, "terms and conditions", "terms of service", "privacy consent", "privacy policy", "i certify", "i agree", "agree to", "attest", "criminal", "disability", "veteran", "ethnicity", "consent", "declaration")
}

func hasSupplementalFactSource(field SnapField, profile *domain.ResumeProfile) bool {
	if profile == nil {
		return false
	}
	question := strings.ToLower(field.Label)
	for _, evidence := range profileEvidenceList(profile) {
		if evidence.Path != "/prompt_instructions" && evidence.Path != "/summary" && !strings.HasPrefix(evidence.Path, "/application_defaults/") {
			continue
		}
		if strings.HasPrefix(evidence.Path, "/application_defaults/form_answers/mapping_") {
			continue
		}
		text := strings.ToLower(evidence.Path + " " + evidence.Quote)
		switch {
		case matchesAny(question, "sponsor"):
			if strings.Contains(text, "sponsor") {
				return true
			}
		case matchesAny(question, "notice", "availability", "start date"):
			if matchesAny(text, "notice", "available", "availability", "earliest_start_date") {
				return true
			}
		case matchesAny(question, "salary", "compensation"):
			if matchesAny(text, "salary", "compensation") {
				return true
			}
		default:
			return true
		}
	}
	return false
}

// An incompatible exact fact cannot be repaired by inventing a shorter name,
// converting unknown salary units or guessing another option. A supplied
// alternative can still be interpreted and independently checked.
func hasConstraintAlternative(field SnapField, profile *domain.ResumeProfile) bool {
	if profile == nil {
		return false
	}
	text := strings.ToLower(field.Label + " " + field.Group + " " + field.HelpText)
	defaults := profile.ApplicationDefaults
	if field.ProfileRecordPath != "" && (field.Type == SnapTypeSelect || field.Type == SnapTypeRadio) {
		if record, ok := educationRecord(profile, field.ProfileRecordPath); ok && ((strings.Contains(text, "degree") && record.EducationLevel != "") || (strings.Contains(text, "field of study") && record.FieldOfStudy != "")) {
			return true
		}
	}
	// A supplied timing fact may require faithful wording for a fixed choice
	// (for example, Immediate -> Immediately Available). This is semantic
	// formatting, checked by the independent verifier before it can be used.
	if (field.Type == SnapTypeRadio || field.Type == SnapTypeSelect) &&
		matchesAny(text, "notice", "availability", "start date") &&
		(defaults.NoticePeriod != "" || defaults.Availability != "" || defaults.EarliestStartDate != "") {
		return true
	}
	if matchesAny(text, "salary", "compensation", "remuneration") && defaults.SalaryRange != nil {
		return true
	}
	if strings.Contains(text, "availability") && (defaults.Availability != "" || defaults.EarliestStartDate != "") {
		return true
	}
	for _, answer := range defaults.CustomAnswers {
		if normalizeLabel(answer.Question) == normalizeLabel(field.Label) && strings.TrimSpace(answer.Answer) != "" {
			return true
		}
	}
	return false
}

func hasOptionalContactFacts(label string, profile *domain.ResumeProfile) bool {
	if profile == nil {
		return false
	}
	terms := []string{"middle name", "other given name", "additional given name"}
	if matchesAny(strings.ToLower(label), "postal", "postcode", "zip") {
		terms = []string{"postal", "postcode", "zip code"}
	}
	for _, answer := range profile.ApplicationDefaults.CustomAnswers {
		if matchesAny(strings.ToLower(answer.Question), terms...) {
			return true
		}
	}
	return matchesAny(strings.ToLower(profile.ApplicationDefaults.AdditionalDetails+" "+profile.PromptInstructions), terms...)
}

// Country-scoped candidate facts cannot borrow a different jurisdiction from
// model approval. UI language and residential address are not jurisdiction proof.
func evidenceCountryCompatible(profile *domain.ResumeProfile, field SnapField, evidence []ProfileEvidence) bool {
	for _, entry := range evidence {
		parts := strings.Split(strings.TrimPrefix(entry.Path, "/"), "/")
		if len(parts) < 4 || parts[0] != "application_defaults" || (parts[1] != "work_rights" && parts[1] != "custom_answers") {
			continue
		}
		prefix := "/application_defaults/" + parts[1] + "/" + parts[2]
		country, exists := profileScalar(profile, prefix+"/country")
		if !exists && parts[1] == "custom_answers" {
			continue
		}
		if !exists || !questionCountryMatches(field, country) {
			return false
		}
		if structuredPermanentResidentVisa(entry) {
			// Legacy AU records may encode status=Yes and visa_type=PR. The
			// abbreviation is meaningful only with current affirmative authority
			// in that same country record, never a pending/negative visa claim.
			status, known := profileScalar(profile, prefix+"/status")
			visa, visaKnown := profileScalar(profile, prefix+"/visa_type")
			quote := workRightWords(entry.Quote)
			if !known || !compatibleResidentAuthorityStatus(status) || !visaKnown || !permanentResidentVisaValue(visa) || (quote == "pr" || quote == "p r") && !questionCountryMatches(field, "Australia") {
				return false
			}
		}
		if parts[1] == "work_rights" && parts[3] == "status" && typedWorkRightStatusQuote(entry.Quote) {
			status, known := profileScalar(profile, prefix+"/status")
			if !known || !sameWorkRightStatusFact(status, entry.Quote, field) {
				return false
			}
		}
	}
	return true
}

var applicationCountryAliases = map[string][]string{
	"australia":      {"australia", "australian", "au", "aus"},
	"new zealand":    {"new zealand", "nz", "new zealander"},
	"united states":  {"united states", "united states of america", "usa", "u s a"},
	"united kingdom": {"united kingdom", "uk", "u k", "britain", "british", "england", "scotland", "wales"},
	"canada":         {"canada", "canadian"},
	"germany":        {"germany", "german", "deutschland"},
	"ireland":        {"ireland", "irish"},
	"france":         {"france", "french"},
	"singapore":      {"singapore", "singaporean"},
}

func countryWords(value string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }), " ")
}

func questionCountryMatches(field SnapField, country string) bool {
	canonical := countryWords(country)
	for name, aliases := range applicationCountryAliases {
		for _, alias := range aliases {
			if canonical == alias {
				canonical = name
			}
		}
	}
	aliases := applicationCountryAliases[canonical]
	if len(aliases) == 0 {
		aliases = []string{canonical}
	}
	contextText := " " + countryWords(field.Label+" "+field.Group+" "+field.HelpText+" "+field.JobLocation) + " "
	matched := false
	for _, alias := range aliases {
		if alias != "" && strings.Contains(contextText, " "+alias+" ") {
			matched = true
		}
	}
	if !matched {
		return false
	}
	for name, others := range applicationCountryAliases {
		if name == canonical {
			continue
		}
		for _, alias := range others {
			if strings.Contains(contextText, " "+alias+" ") {
				return false
			}
		}
	}
	return true
}

// High-impact facts need an explicit relevant source. Residence, employment
// history or a sponsorship preference cannot establish legal work rights. The
// independent verifier still checks entailment, qualifiers and negation.
func sensitiveEvidenceCompatible(field SnapField, answer string, evidence []ProfileEvidence) bool {
	question := strings.ToLower(field.Label + " " + field.Group + " " + field.HelpText)
	var text strings.Builder
	for _, e := range evidence {
		text.WriteString(strings.ToLower(e.Path + " " + e.Quote))
		text.WriteByte('\n')
	}
	sources := text.String()
	if isWorkAuthorizationQuestion(question) {
		explicit := false
		for _, e := range evidence {
			if strings.HasPrefix(e.Path, "/personal_information/") || e.Path == "/application_defaults/requires_sponsorship" {
				continue
			}
			if matchesAny(strings.ToLower(e.Quote), "permanent resident", "citizen", "right to work", "work rights", "working rights", "work permit", "work visa", "work authori", "work restrictions") || structuredAuthorizationEvidence(e, field, answer, evidence) || structuredPermanentResidentVisa(e) && residentAnswerCompatible(field, answer, evidence) {
				explicit = true
			}
		}
		if !explicit {
			return false
		}
		if workRightAlternativeKind(field, answer) != "" && !supportedWorkRightAlternativeChoice(field, answer, evidence) || unsupportedResidentConjunction(field, answer, evidence) {
			return false
		}
		if strings.Contains(strings.ToLower(answer), "citizen") && (!strings.Contains(sources, "citizen") || matchesAny(sources, "not a citizen", "not an australian citizen", "not citizen")) && !supportedWorkRightAlternativeChoice(field, answer, evidence) {
			return false
		}
		// Visa subclasses, expiry dates and numeric restrictions cannot be added
		// to an explicit residency statement without their own numeric evidence.
		for _, number := range groundedNumberPattern.FindAllString(answer, -1) {
			if !strings.Contains(sources, number) {
				return false
			}
		}
	} else if matchesAny(question, "visa expiry", "visa expiration", "visa subclass", "visa type", "visa restrictions") {
		if !matchesAny(sources, "visa_expiry", "visa_type", "visa_subclass", "visa_restrictions", "visa expires", "visa expiry", "visa type", "visa subclass", "visa restrictions") {
			return false
		}
	}
	if matchesAny(question, "sponsor") && !matchesAny(sources, "requires_sponsorship", "sponsor") {
		return false
	}
	if matchesAny(question, "sponsor") && !sponsorshipEvidenceCompatible(field, evidence) {
		return false
	}
	if matchesAny(question, "salary", "compensation", "remuneration") && !matchesAny(sources, "salary_expectation", "salary", "compensation", "remuneration") {
		return false
	}
	if matchesAny(question, "notice period", "notice must", "give notice") && !matchesAny(sources, "notice_period", "notice") {
		return false
	}
	return !manualDeclaration(field)
}

func newGroundedProof(profile *domain.ResumeProfile, snap StepSnapshot, field SnapField, proposal LLMFieldProposal, value string) *groundedAnswerProof {
	return &groundedAnswerProof{
		profileDigest: profileDigest(profile), fieldSig: field.FieldSignature(snap.Platform),
		platform: snap.Platform, employer: snap.EmployerKey,
		answerDigest: groundedAnswerDigest(proposal.IntentType, proposal.BindingKey, value),
		evidence:     append([]ProfileEvidence(nil), proposal.Evidence...),
	}
}

func validGroundedProof(proof *groundedAnswerProof, profile *domain.ResumeProfile, snap StepSnapshot, field SnapField, intent FieldIntentType, binding, value string) bool {
	return proof != nil && proof.profileDigest != "" && proof.profileDigest == profileDigest(profile) &&
		proof.platform == snap.Platform && proof.employer == snap.EmployerKey && proof.fieldSig == field.FieldSignature(snap.Platform) &&
		proof.answerDigest == groundedAnswerDigest(intent, binding, value) &&
		supportedProfileEvidence(profile, proof.evidence) && customAnswerScopeCompatible(profile, snap, field, proof.evidence) && evidenceCountryCompatible(profile, field, proof.evidence) && educationEvidenceCompatible(field, proof.evidence) && phoneCallingCodeEvidenceCompatible(field, profile, value, proof.evidence) && sensitiveEvidenceCompatible(field, value, proof.evidence) && currentWorkRightsTimeValid(profile, field, value, proof.evidence, time.Now())
}

func groundedResolutionCompatible(field SnapField, binding, value string) bool {
	// The independent verifier handles work-authorization semantics against
	// explicit evidence; the existing local control/unit checks still apply.
	copy := field
	if isWorkAuthorizationQuestion(field.Label + " " + field.Group + " " + field.HelpText) {
		copy.Label, copy.Group, copy.HelpText = "", "", ""
	}
	return bindingValueCompatible(copy, binding, value)
}

func directBindingIndependentOfValue(field SnapField, binding string) bool {
	binding = normalizeProfileBindingKey(binding)
	return knownProfileBinding(binding) && binding != "application_defaults.salary_expectation" && binding != "application_defaults.requires_sponsorship" && binding != "application_defaults.future_requires_sponsorship" &&
		!modelBindingNeedsReview(field, binding) && !isWorkAuthorizationQuestion(field.Label+" "+field.Group+" "+field.HelpText)
}

func reusableGroundedCache(record *groundedCacheRecord, profile *domain.ResumeProfile, snap StepSnapshot, field SnapField, r FieldResolution) bool {
	return reusableGroundedCacheAt(record, profile, snap, field, r, time.Now())
}

func reusableGroundedCacheAt(record *groundedCacheRecord, profile *domain.ResumeProfile, snap StepSnapshot, field SnapField, r FieldResolution, now time.Time) bool {
	if record == nil || record.Version != groundedAnswerVersion || record.FieldSig != field.FieldSignature(snap.Platform) ||
		record.AnswerDigest != groundedAnswerDigest(r.IntentType, r.BindingKey, r.Value) || manualDeclaration(field) {
		return false
	}
	if record.ProfileDigest == "" {
		return r.IntentType == FieldIntentProfile && directBindingIndependentOfValue(field, r.BindingKey)
	}
	return record.ProfileDigest == profileDigest(profile) && supportedProfileEvidence(profile, record.Evidence) && customAnswerScopeCompatible(profile, snap, field, record.Evidence) && evidenceCountryCompatible(profile, field, record.Evidence) && educationEvidenceCompatible(field, record.Evidence) && phoneCallingCodeEvidenceCompatible(field, profile, r.Value, record.Evidence) && sensitiveEvidenceCompatible(field, r.Value, record.Evidence) && currentWorkRightsTimeValid(profile, field, r.Value, record.Evidence, now)
}

func customAnswerScopeCompatible(profile *domain.ResumeProfile, snap StepSnapshot, field SnapField, evidence []ProfileEvidence) bool {
	const prefix = "/application_defaults/custom_answers/"
	for _, e := range evidence {
		if !strings.HasPrefix(e.Path, prefix) {
			continue
		}
		index, err := strconv.Atoi(strings.Split(strings.TrimPrefix(e.Path, prefix), "/")[0])
		if err != nil || profile == nil || index < 0 || index >= len(profile.ApplicationDefaults.CustomAnswers) {
			return false
		}
		a := profile.ApplicationDefaults.CustomAnswers[index]
		if a.Employer != "" && !strings.EqualFold(a.Employer, snap.EmployerKey) {
			return false
		}
		if a.JobID != "" && (field.JobID == "" || a.JobID != field.JobID) {
			return false
		}
	}
	return true
}
