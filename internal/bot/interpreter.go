package bot

// interpreter.go — Step interpreter for the adaptive engine (story #131).
//
// InterpretStep resolves form fields in three passes:
//   1. Deterministic handlers (file upload, unambiguous profile lookups)
//   2. Verified mapping lookup (hook for story #137; no-op until implemented)
//   3. Batched model proposals with independent grounding verification
//
// The interpreter never executes browser actions — it only produces proposals.
// Resolution sources and pending reasons are typed so callers can persist them.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/user/jobifai/internal/domain"
)

// ResolutionSource identifies how a field was resolved.
type ResolutionSource string

const (
	// SourceDeterministic: resolved by rule without any model call.
	SourceDeterministic ResolutionSource = "deterministic"
	// SourceProfile: resolved from candidate profile data directly.
	SourceProfile ResolutionSource = "profile"
	// SourcePreferences: resolved from the candidate's saved work preferences.
	SourcePreferences ResolutionSource = "preferences"
	// SourceMapping: resolved from a verified reuse mapping (story #137).
	SourceMapping ResolutionSource = "mapping"
	// SourceLLM: resolved by a batched LLM request.
	SourceLLM ResolutionSource = "llm"
	// SourceReview: could not be resolved; routed to candidate review.
	SourceReview ResolutionSource = "review"
)

// AllowedAction is a closed set of actions the execution layer may perform.
type AllowedAction string

const (
	ActionFill   AllowedAction = "fill"
	ActionUpload AllowedAction = "upload"
	ActionSkip   AllowedAction = "skip"
)

// PendingReason describes why a field could not be fully resolved.
type PendingReason string

const (
	PendingMissingProfile  PendingReason = "missing_profile"
	PendingLLMUnavailable  PendingReason = "llm_unavailable"
	PendingMalformedOutput PendingReason = "malformed_output"
	PendingLimitExceeded   PendingReason = "limit_exceeded"
	PendingAmbiguous       PendingReason = "ambiguous"
	PendingInvalidOption   PendingReason = "invalid_option" // LLM chose an option not in the field's list
	PendingUnsupported     PendingReason = "unsupported"    // field type not supported by any handler
)

// FieldIntentType classifies how a mapping cache entry should be resolved at warm-hit time.
type FieldIntentType string

const (
	// FieldIntentProfile: answer is derived from a named candidate profile field.
	// BindingKey identifies which profile field to read at runtime, so the warm hit
	// always reflects the candidate's current data even after a profile update.
	FieldIntentProfile FieldIntentType = "profile"
	// Preference bindings reread canonical settings, never a copied resume value.
	FieldIntentPreferences FieldIntentType = "preferences"
	// FieldIntentLLM: derived wording requires checked current-profile evidence
	// and independent semantic verification before filling or reuse.
	FieldIntentLLM FieldIntentType = "llm"
)

// VerificationProvenance records how a mapping became trusted enough to be reused.
type VerificationProvenance string

const (
	// ProvenancePending: proposed but not yet verified; unavailable for reuse.
	ProvenancePending VerificationProvenance = "pending"
	// ProvenanceAcceptedByForm: form accepted the value during ExecuteStep.
	ProvenanceAcceptedByForm VerificationProvenance = "accepted_by_form"
	// ProvenanceConfirmedByReviewer: human reviewer explicitly promoted the mapping.
	ProvenanceConfirmedByReviewer VerificationProvenance = "confirmed_by_reviewer"
	// Mechanical form acceptance is not semantic evidence. Local reference
	// bindings have independently known meanings.
	ProvenanceTrustedReference VerificationProvenance = "trusted_reference"
	// ProvenanceVerifiedByModel requires checked profile evidence and a separate
	// semantic verification response. A proposal cannot set this authority.
	ProvenanceVerifiedByModel VerificationProvenance = "verified_by_model"
)

// FieldResolution is the resolution outcome for one form field.
type FieldResolution struct {
	Ref        string                 `json:"ref"`
	Source     ResolutionSource       `json:"source"`
	Action     AllowedAction          `json:"action"`
	Value      string                 `json:"value,omitempty"`
	Pending    bool                   `json:"pending,omitempty"`
	Reason     PendingReason          `json:"reason,omitempty"`
	IntentType FieldIntentType        `json:"intent_type,omitempty"`
	BindingKey string                 `json:"binding_key,omitempty"`
	Provenance VerificationProvenance `json:"provenance,omitempty"`
	grounded   *groundedAnswerProof
	cached     *groundedCacheRecord
}

// StepInterpretation is the result of interpreting all fields in a snapshot step.
type StepInterpretation struct {
	SnapshotSig string            `json:"snapshot_sig"` // from StepSnapshot.SemanticSignature()
	Fields      []FieldResolution `json:"fields"`
	Paused      bool              `json:"paused"` // any field is Pending
	PauseReason PendingReason     `json:"pause_reason,omitempty"`
	LLMUsed     bool              `json:"llm_used"`
}

// LLMFieldProposal is one field's proposal from the LLM batch response.
type LLMFieldProposal struct {
	Ref                  string            `json:"ref"`
	Action               string            `json:"action"` // "fill" | "upload" | "skip"
	Value                string            `json:"value"`
	IntentType           FieldIntentType   `json:"intent_type"` // "profile" | "llm"
	BindingKey           string            `json:"binding_key"` // non-empty when intent_type="profile"
	Evidence             []ProfileEvidence `json:"evidence,omitempty"`
	grounded             *groundedAnswerProof
	verificationRequired bool
}

// StepInterpreterLLM is the LLM call contract for the interpreter.
// Called with only the fields that could not be resolved deterministically.
// Implementations must return one proposal per requested ref.
type StepInterpreterLLM interface {
	InterpretFields(ctx context.Context, profile *domain.ResumeProfile, snap StepSnapshot, fieldRefs []string) ([]LLMFieldProposal, error)
}

// MappingLookup is the hook for field-level verified mapping reuse.
// LookupField looks up a single field by its platform-scoped identity hash and
// the employer key. Returns false on a cold miss or when no trusted mapping exists.
// employerKey is the company name from the job posting; pass "" for a global lookup.
type MappingLookup interface {
	LookupField(fieldSig, employerKey string) (FieldResolution, bool)
}

// nopMappingLookup is a no-op MappingLookup used when no mapping store is wired.
type nopMappingLookup struct{}

func (nopMappingLookup) LookupField(_, _ string) (FieldResolution, bool) {
	return FieldResolution{}, false
}

// StepInterpreter drives the three-pass resolution pipeline.
type StepInterpreter struct {
	LLM          StepInterpreterLLM      // nil → all ambiguous fields become review
	Mapping      MappingLookup           // nil → no mapping lookups
	Preferences  *domain.WorkPreferences // canonical candidate work-arrangement facts
	KnownMissing func(context.Context, StepSnapshot, SnapField, *domain.ResumeProfile) (bool, error)
}

// InterpretStep resolves all fields in snap against profile.
// Fields already Filled=true are skipped (no re-resolution).
func (si *StepInterpreter) InterpretStep(ctx context.Context, snap StepSnapshot, profile *domain.ResumeProfile) (StepInterpretation, error) {
	mapping := si.Mapping
	if mapping == nil {
		mapping = nopMappingLookup{}
	}

	sig := snap.SemanticSignature()
	result := StepInterpretation{SnapshotSig: sig}
	if snap.Truncated {
		result.Paused, result.PauseReason = true, PendingLimitExceeded
		return result, nil
	}
	snap.Fields = append([]SnapField(nil), snap.Fields...)

	var toLLM []string // refs of fields that need LLM resolution
	var missingLookupErr error
	queueForLLM := func(index int, field SnapField) {
		if si.KnownMissing != nil && field.Required {
			known, err := si.KnownMissing(ctx, snap, field, profile)
			if err != nil {
				missingLookupErr = err
				return
			}
			if known {
				result.Fields = append(result.Fields, FieldResolution{Ref: field.Ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingMissingProfile})
				return
			}
		}
		if field.Truncated {
			expanded, ok := expandInterpretationOptions(field)
			if !ok {
				result.Fields = append(result.Fields, FieldResolution{Ref: field.Ref, Source: SourceReview, Action: ActionSkip, Pending: field.Required, Reason: PendingLimitExceeded})
				return
			}
			snap.Fields[index] = expanded
		}
		toLLM = append(toLLM, field.Ref)
	}
	counts := make(map[string]int)
	for _, f := range snap.Fields {
		counts[f.FieldSignature(snap.Platform)]++
	}

	for fieldIndex, f := range snap.Fields {
		if counts[f.FieldSignature(snap.Platform)] > 1 {
			result.Fields = append(result.Fields, FieldResolution{Ref: f.Ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingAmbiguous})
			continue
		}
		if f.Truncated && f.BrowserField == nil {
			result.Fields = append(result.Fields, FieldResolution{Ref: f.Ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingLimitExceeded})
			continue
		}
		if f.Truncated {
			f.Options = make([]string, len(f.BrowserField.Options))
			for i, option := range f.BrowserField.Options {
				f.Options[i] = option.Label
			}
		}
		if f.Filled {
			// Already has a value — skip; don't re-fill.
			continue
		}

		// Pass 1: deterministic
		var missingLocal *FieldResolution
		local, localOK := workPreferenceResolution(f, si.Preferences, profile)
		if !localOK {
			local, localOK = compositePhoneResolution(f, snap, profile)
		}
		if !localOK {
			local, localOK = deterministicResolve(f, profile)
		}
		if res, ok := local, localOK; ok {
			if res.Action == ActionFill && !bindingValueCompatible(f, res.BindingKey, res.Value) {
				res = FieldResolution{Ref: f.Ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingInvalidOption}
			}
			optionalFacts := hasOptionalContactFacts(f.Label, profile) ||
				(matchesAny(strings.ToLower(f.Label), "notice", "availability", "start date") && hasSupplementalFactSource(f, profile))
			learnablePhoneShape := res.Pending && res.Reason == PendingAmbiguous && phoneCallingCodeFallbackAvailable(f, profile)
			if isPhoneCallingCodeField(f) && res.Action != ActionFill && !learnablePhoneShape {
				result.Fields = append(result.Fields, res)
				continue
			}
			if learnablePhoneShape || (res.Pending || (res.Reason == PendingMissingProfile && optionalFacts)) && (res.Reason == PendingMissingProfile || res.Reason == PendingInvalidOption) {
				// Try verified question mappings and explicit supplementary facts
				// before requiring a missing profile fact.
				missingLocal = &res
			} else {
				result.Fields = append(result.Fields, res)
				continue
			}
		}

		// Pass 2: field-level mapping lookup.
		// fieldSig encodes platform + group + label + type + options — unique per field identity.
		if res, ok := mapping.LookupField(f.FieldSignature(snap.Platform), snap.EmployerKey); ok {
			res.Ref = f.Ref
			res.Source = SourceMapping
			// Profile-bound entries: resolve the current value from the live profile so
			// the warm hit always reflects the candidate's latest data.
			if res.Provenance == ProvenanceVerifiedByModel && !reusableGroundedCache(res.cached, profile, snap, f, res) {
				queueForLLM(fieldIndex, f)
				continue
			}
			usableIntent := res.IntentType == FieldIntentProfile || res.IntentType == FieldIntentPreferences || (res.IntentType == FieldIntentLLM && res.Provenance == ProvenanceVerifiedByModel)
			if !usableIntent {
				queueForLLM(fieldIndex, f)
				continue
			}
			if res.IntentType == FieldIntentPreferences {
				current, supported := workPreferenceResolution(f, si.Preferences, profile)
				if !knownWorkPreferenceBinding(res.BindingKey) || !supported || current.BindingKey != res.BindingKey {
					result.Fields = append(result.Fields, FieldResolution{Ref: f.Ref, Source: SourceReview, Action: ActionSkip, Pending: f.Required, Reason: PendingMissingProfile})
					continue
				}
				res.Value = current.Value
			}
			if res.IntentType == FieldIntentProfile {
				if !knownProfileBinding(res.BindingKey) {
					queueForLLM(fieldIndex, f)
					continue
				}
				v, resolved := resolveProfileBindingForField(res.BindingKey, profile, f)
				if !resolved || v == "" {
					// Candidate cleared this profile field — do not reuse the stale answer.
					result.Fields = append(result.Fields, FieldResolution{Ref: f.Ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingMissingProfile})
					continue
				}
				res.Value = v
			}
			var evidence []ProfileEvidence
			if res.cached != nil {
				evidence = res.cached.Evidence
			}
			if !currentWorkRightsTimeValid(profile, f, res.Value, evidence, time.Now()) {
				queueForLLM(fieldIndex, f)
				continue
			}
			compatible := bindingValueCompatible(f, res.BindingKey, res.Value)
			if res.Provenance == ProvenanceVerifiedByModel {
				compatible = groundedResolutionCompatible(f, res.BindingKey, res.Value)
			}
			if !compatible {
				result.Fields = append(result.Fields, FieldResolution{Ref: f.Ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingInvalidOption})
				continue
			}
			// For radio/select: reject warm-hit values that no longer appear in the
			// current option list (form may have changed its option set).
			if res.Action == ActionFill && (f.Type == SnapTypeRadio || f.Type == SnapTypeSelect) && len(f.Options) > 0 {
				if !containsOptionIgnoreCase(f.Options, res.Value) {
					result.Fields = append(result.Fields, FieldResolution{Ref: f.Ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingInvalidOption})
					continue
				}
			}
			result.Fields = append(result.Fields, res)
			continue
		}

		if missingLocal != nil && (si.LLM == nil ||
			(missingLocal.Reason == PendingMissingProfile && !hasSupplementalFactSource(f, profile)) ||
			(missingLocal.Reason == PendingInvalidOption && !hasConstraintAlternative(f, profile))) {
			result.Fields = append(result.Fields, *missingLocal)
			continue
		}
		// Not resolved yet — queue for LLM.
		queueForLLM(fieldIndex, f)
	}

	// Pass 3: batch LLM for remaining fields.
	if missingLookupErr != nil {
		return result, missingLookupErr
	}
	if len(toLLM) > 0 {
		if si.LLM == nil {
			// No LLM configured — route everything to review.
			for _, ref := range toLLM {
				result.Fields = append(result.Fields, FieldResolution{
					Ref:     ref,
					Source:  SourceReview,
					Action:  ActionSkip,
					Pending: true,
					Reason:  PendingLLMUnavailable,
				})
			}
		} else {
			proposals, err := si.LLM.InterpretFields(ctx, profile, snap, toLLM)
			if err != nil {
				// Provider, quota and cancellation failures are operational failures.
				// They cannot become a request for candidate answer confirmation.
				return result, err
			}
			llmResolutions := validateLLMProposals(proposals, toLLM, snap, profile)
			result.Fields = append(result.Fields, llmResolutions...)
			result.LLMUsed = true
		}
	}

	// A genuinely missing optional fact can remain blank. Structural/provider
	// failures still retain their operational handling.
	for i := range result.Fields {
		r := &result.Fields[i]
		if f, ok := snap.FieldByRef(r.Ref); ok && !f.Required && r.Pending && r.Reason == PendingMissingProfile {
			r.Pending = false
			r.Action = ActionSkip
		}
	}
	// Set Paused if any field is pending.
	for _, r := range result.Fields {
		if r.Pending {
			result.Paused = true
			result.PauseReason = r.Reason
			break
		}
	}

	return result, nil
}

// deterministicResolve handles fields that can be resolved without an LLM call.
func deterministicResolve(f SnapField, profile *domain.ResumeProfile) (FieldResolution, bool) {
	// Unsupported field types never resolved deterministically.
	if f.Type == SnapTypeUnsupported {
		return FieldResolution{
			Ref:     f.Ref,
			Source:  SourceReview,
			Action:  ActionSkip,
			Pending: true,
			Reason:  PendingUnsupported,
		}, true
	}

	// File upload: always ActionUpload regardless of question text.
	if f.Type == SnapTypeFile {
		return FieldResolution{
			Ref:    f.Ref,
			Source: SourceDeterministic,
			Action: ActionUpload,
		}, true
	}

	label := strings.ToLower(strings.TrimSpace(f.Label))
	if strings.Trim(countryWords(label), " ") == "location city" {
		label = "city"
	}
	contextText := strings.ToLower(f.Label + " " + f.Group + " " + f.HelpText)
	if manualDeclaration(f) {
		return FieldResolution{Ref: f.Ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingAmbiguous}, true
	}
	if resolved, ok := resolveEducationField(f, profile); ok {
		return resolved, true
	}
	if isPhoneCallingCodeField(f) && !personalBindingContextIsQualified(contextText) && !isJobDependentQuestion(contextText) {
		return resolvePhoneCallingCode(f, profile), true
	}
	if label == "middle name" && !f.Required && !hasOptionalContactFacts(label, profile) {
		return FieldResolution{Ref: f.Ref, Source: SourceReview, Action: ActionSkip, Reason: PendingMissingProfile}, true
	}
	// Sponsorship is not legal work authorization. The profile's sponsorship
	// preference cannot answer a country-specific eligibility declaration.
	if isWorkAuthorizationQuestion(contextText) || isJobDependentQuestion(contextText) {
		return FieldResolution{}, false
	}
	if matchesAny(contextText, "total compensation", "monthly", "hourly", "weekly", "base salary", "per month", "per hour") && matchesAny(label, "salary", "compensation", "pay") {
		return FieldResolution{}, false // qualifiers require an independently verified mapping
	}
	// Substrings cannot establish whose contact details or which declaration is
	// requested. Only simple, affirmative candidate questions resolve locally.
	if personalBindingContextIsQualified(contextText) || matchesAny(contextText, "not require", "do not need", "without sponsorship") || !simpleCandidateQuestion(label) {
		return FieldResolution{}, false
	}

	if profile == nil {
		if needsProfile(label) {
			return FieldResolution{
				Ref:     f.Ref,
				Source:  SourceReview,
				Action:  ActionSkip,
				Pending: true,
				Reason:  PendingMissingProfile,
			}, true
		}
		return FieldResolution{}, false
	}

	pi := profile.PersonalInformation
	ad := profile.ApplicationDefaults

	// Name fields.
	if matchesAny(label, "first name", "given name", "forename") && pi.Name != "" {
		return makeText(f.Ref, pi.Name), true
	}
	if matchesAny(label, "last name", "surname", "family name") && pi.Surname != "" {
		return makeText(f.Ref, pi.Surname), true
	}
	if matchesAny(label, "full name", "your name") && pi.Name != "" && pi.Surname != "" {
		return makeText(f.Ref, pi.Name+" "+pi.Surname), true
	}

	// Contact fields.
	if matchesAny(label, "phone", "mobile", "telephone", "phone number") && pi.Phone != "" {
		return makeText(f.Ref, pi.Phone), true
	}
	if matchesAny(label, "email", "email address") && pi.Email != "" {
		return makeText(f.Ref, pi.Email), true
	}

	// Location.
	if matchesAny(label, "city", "town") && pi.City != "" {
		return makeText(f.Ref, pi.City), true
	}
	if matchesAny(label, "country") && pi.Country != "" {
		return makeText(f.Ref, pi.Country), true
	}
	if matchesAny(label, "zip", "postcode", "postal code") && pi.ZipCode != "" {
		return makeText(f.Ref, pi.ZipCode), true
	}
	if matchesAny(label, "address", "street address") && !strings.Contains(label, "email") && pi.Address != "" {
		return makeText(f.Ref, pi.Address), true
	}

	// Application defaults.
	if label == "earliest start date" && ad.EarliestStartDate != "" {
		res := makeText(f.Ref, ad.EarliestStartDate)
		res.BindingKey = "application_defaults.earliest_start_date"
		return res, true
	}
	if matchesAny(label, "availability") && ad.Availability != "" {
		res := makeText(f.Ref, ad.Availability)
		res.BindingKey = "application_defaults.availability"
		return res, true
	}
	if matchesAny(label, "notice period", "notice") && ad.NoticePeriod != "" {
		return makeText(f.Ref, ad.NoticePeriod), true
	}
	if matchesAny(label, "salary", "expected salary", "salary expectation", "desired salary", "compensation") && ad.SalaryRange == nil && currentSalaryExpectation(profile) != "" {
		res := makeText(f.Ref, currentSalaryExpectation(profile))
		res.BindingKey = "application_defaults.salary_expectation"
		return res, true
	}

	// Sponsorship (yes/no radio/select).
	if (f.Type == SnapTypeRadio || f.Type == SnapTypeSelect) && matchesAny(label,
		"require sponsorship", "need sponsorship", "visa sponsorship") {
		sponsorship, _, resolved := currentSponsorship(f, profile)
		if !resolved {
			return FieldResolution{Ref: f.Ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingMissingProfile}, true
		}
		ans := "No"
		if *sponsorship {
			ans = "Yes"
		}
		if containsOptionIgnoreCase(f.Options, ans) {
			return FieldResolution{
				Ref:        f.Ref,
				Source:     SourceProfile,
				Action:     ActionFill,
				Value:      ans,
				BindingKey: "application_defaults.requires_sponsorship",
			}, true
		}
		// Options don't contain the standard Yes/No — let the LLM handle it.
		return FieldResolution{}, false
	}

	// Missing profile data for a field that looked deterministic.
	if needsProfile(label) && profileValueForLabel(label, profile) == "" {
		return FieldResolution{
			Ref:     f.Ref,
			Source:  SourceReview,
			Action:  ActionSkip,
			Pending: f.Required,
			Reason:  PendingMissingProfile,
		}, true
	}

	return FieldResolution{}, false
}

func makeText(ref, value string) FieldResolution {
	return FieldResolution{
		Ref:    ref,
		Source: SourceProfile,
		Action: ActionFill,
		Value:  value,
	}
}

func simpleCandidateQuestion(label string) bool {
	label = strings.Trim(strings.Join(strings.Fields(label), " "), " *?:.")
	switch label {
	case "first name", "given name", "forename", "last name", "surname", "family name", "full name", "your name",
		"phone", "phone number", "mobile", "mobile number", "mobile phone number", "cell phone", "cell phone number", "telephone", "telephone number", "email", "email address",
		"city", "town", "city/town", "country", "country of residence", "zip", "zip code", "postcode", "postal code", "address", "street address",
		"notice period", "availability", "earliest start date", "salary", "expected salary", "salary expectation", "desired salary",
		"require sponsorship", "need sponsorship", "visa sponsorship", "do you require sponsorship", "do you need sponsorship",
		"will you require sponsorship", "do you now or in the future require sponsorship":
		return true
	}
	return false
}

// validateLLMProposals validates LLM proposals against the snapshot and builds FieldResolutions.
// Fields in toLLM that have no valid proposal become review/pending.
// Profile-intent proposals are resolved from the live profile at validate time.
func validateLLMProposals(proposals []LLMFieldProposal, toLLM []string, snap StepSnapshot, profile *domain.ResumeProfile) []FieldResolution {
	// Index proposals by ref.
	byRef := make(map[string]LLMFieldProposal, len(proposals))
	duplicates := make(map[string]bool)
	for _, p := range proposals {
		if _, exists := byRef[p.Ref]; exists {
			duplicates[p.Ref] = true
		}
		byRef[p.Ref] = p
	}

	var out []FieldResolution
	for _, ref := range toLLM {
		f, fieldOK := snap.FieldByRef(ref)
		p, propOK := byRef[ref]

		if !propOK || !fieldOK || duplicates[ref] {
			out = append(out, FieldResolution{
				Ref:     ref,
				Source:  SourceReview,
				Action:  ActionSkip,
				Pending: true,
				Reason:  PendingMalformedOutput,
			})
			continue
		}

		action := AllowedAction(p.Action)
		if action != ActionFill && action != ActionUpload && action != ActionSkip {
			out = append(out, FieldResolution{
				Ref:     ref,
				Source:  SourceReview,
				Action:  ActionSkip,
				Pending: true,
				Reason:  PendingMalformedOutput,
			})
			continue
		}

		// Resolve profile-bound proposals at interpretation time so the value is
		// available for ExecuteStep. Store both intent and binding key so the mapping
		// cache can re-resolve from the live profile on subsequent warm hits.
		intentType := p.IntentType
		bindingKey := p.BindingKey
		value := p.Value
		if action == ActionSkip {
			out = append(out, FieldResolution{Ref: ref, Source: SourceReview, Action: ActionSkip, Pending: f.Required, Reason: PendingMissingProfile})
			continue
		}
		if action != ActionFill || manualDeclaration(f) {
			out = append(out, FieldResolution{Ref: ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingAmbiguous})
			continue
		}
		if intentType == FieldIntentProfile && knownProfileBinding(bindingKey) {
			if v, ok := resolveProfileBindingForField(bindingKey, profile, f); ok {
				value = v
			} else {
				out = append(out, FieldResolution{Ref: ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingMissingProfile})
				continue
			}
		}
		verified := validGroundedProof(p.grounded, profile, snap, f, intentType, bindingKey, value)
		if !currentWorkRightsTimeValid(profile, f, value, p.Evidence, time.Now()) {
			out = append(out, FieldResolution{Ref: ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingAmbiguous})
			continue
		}
		if p.verificationRequired && !verified {
			out = append(out, FieldResolution{Ref: ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingAmbiguous})
			continue
		}
		usableIntent := (intentType == FieldIntentProfile && knownProfileBinding(bindingKey)) || (intentType == FieldIntentLLM && bindingKey == "" && verified)
		if !usableIntent {
			out = append(out, FieldResolution{Ref: ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingAmbiguous})
			continue
		}
		if !verified && modelBindingNeedsReview(f, bindingKey) {
			out = append(out, FieldResolution{Ref: ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingAmbiguous})
			continue
		}
		if !verified && isWorkAuthorizationQuestion(f.Label+" "+f.Group+" "+f.HelpText) && !strings.HasPrefix(bindingKey, "application_defaults.form_answers.") {
			out = append(out, FieldResolution{Ref: ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingMissingProfile})
			continue
		}
		compatible := bindingValueCompatible(f, bindingKey, value)
		if verified {
			compatible = groundedResolutionCompatible(f, bindingKey, value)
		}
		if strings.TrimSpace(value) == "" || !compatible {
			out = append(out, FieldResolution{Ref: ref, Source: SourceReview, Action: ActionSkip, Pending: true, Reason: PendingInvalidOption})
			continue
		}

		// For radio/select: value must match one of the declared options.
		if action == ActionFill && (f.Type == SnapTypeRadio || f.Type == SnapTypeSelect) {
			if !containsOptionIgnoreCase(f.Options, value) {
				out = append(out, FieldResolution{
					Ref:     ref,
					Source:  SourceReview,
					Action:  ActionSkip,
					Pending: true,
					Reason:  PendingInvalidOption,
				})
				continue
			}
		}

		provenance := ProvenancePending
		if verified {
			provenance = ProvenanceVerifiedByModel
		}
		out = append(out, FieldResolution{
			Ref:        ref,
			Source:     SourceLLM,
			Action:     action,
			Value:      value,
			IntentType: intentType,
			BindingKey: bindingKey,
			Provenance: provenance,
			grounded:   p.grounded,
		})
	}
	return out
}

func modelBindingNeedsReview(field SnapField, binding string) bool {
	// These open-ended facts need a checked question meaning and qualifiers,
	// even when a proposed answer exactly matches the saved scalar.
	if binding == "application_defaults.travel_conditions" || binding == "application_defaults.working_hours" || binding == "application_defaults.commute_conditions" {
		return true
	}
	contextText := strings.ToLower(field.Label + " " + field.Group + " " + field.HelpText)
	if isJobDependentQuestion(contextText) {
		return true
	}
	// A question-specific reviewed answer is authority only for its verified
	// question/scope. The mapping lookup handles that; a model cannot repurpose it.
	if strings.HasPrefix(binding, "application_defaults.form_answers.mapping_") {
		return true
	}
	if strings.HasPrefix(binding, "personal_information.") && personalBindingContextIsQualified(contextText) {
		return true
	}
	if binding == "application_defaults.requires_sponsorship" && matchesAny(contextText, "not require", "do not need", "without sponsorship", "no sponsorship") {
		return true
	}
	if binding == "application_defaults.salary_expectation" && matchesAny(contextText, "monthly", "per month", "hourly", "per hour", "weekly", "per week", "base salary", "total compensation", "previous salary", "current salary", "currency", "aud", "usd", "eur", "gbp", "nzd", "€", "£", "$") {
		return true
	}
	return false
}

func isJobDependentQuestion(contextText string) bool {
	return matchesAny(strings.ToLower(contextText), "this role", "this position", "this job", "this opportunity", "our company", "our team", "join us", "why are you", "why do you", "why would you", "why should we", "relevant experience", "required skills", "relocat")
}

func isWorkAuthorizationQuestion(contextText string) bool {
	return matchesAny(strings.ToLower(contextText), "work authorization", "authorized to work", "authorised to work", "right to work", "legally allowed to work", "legally entitled to work", "eligible to work", "legally able to work", "permission to work", "work permit", "work visa", "working rights", "unrestricted work rights")
}

func personalBindingContextIsQualified(contextText string) bool {
	return matchesAny(strings.ToLower(contextText), "previous employer", "emergency contact", "reference contact", "referee", "recruiter", "references", "citizenship", "nationality", "country of birth", "place of birth", "birth name", "education", "institution", "school", "university", "company address", "company information", "employer details", "office location")
}

// ErrLLMUnavailable signals the LLM provider could not fulfil the request.
var ErrLLMUnavailable = errors.New("interpreter: LLM unavailable")

// ErrLLMLimitExceeded signals the request exceeded the LLM's budget or token limit.
var ErrLLMLimitExceeded = errors.New("interpreter: LLM limit exceeded")
var errAdaptiveQuestionReview = errors.New("application deferred: an answer needs confirmation in Settings → Profile → Application answers")

func pendingReasonFromErr(err error) PendingReason {
	if errors.Is(err, ErrLLMLimitExceeded) {
		return PendingLimitExceeded
	}
	if errors.Is(err, ErrLLMUnavailable) {
		return PendingLLMUnavailable
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return PendingMalformedOutput
	}
	var valueTypeErr *json.UnmarshalTypeError
	if errors.As(err, &valueTypeErr) {
		return PendingMalformedOutput
	}
	return PendingLLMUnavailable
}

// matchesAny returns true when label contains any of the needles (case-insensitive substring).
func matchesAny(label string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(label, n) {
			return true
		}
	}
	return false
}

// containsOptionIgnoreCase checks whether value matches any option in a case-insensitive way.
func containsOptionIgnoreCase(options []string, value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	for _, o := range options {
		if strings.ToLower(strings.TrimSpace(o)) == v {
			return true
		}
	}
	return false
}

// needsProfile returns true when the label suggests the value must come from the candidate's profile.
func needsProfile(label string) bool {
	return matchesAny(label,
		"first name", "last name", "full name", "surname", "given name", "forename", "family name",
		"phone", "mobile", "telephone", "email",
		"city", "town", "country", "zip", "postcode", "postal code", "address",
		"notice", "salary", "compensation", "sponsorship", "visa",
	)
}

// profileValueForLabel returns the profile value for a known label, or "".
func profileValueForLabel(label string, p *domain.ResumeProfile) string {
	if p == nil {
		return ""
	}
	pi := p.PersonalInformation
	switch {
	case matchesAny(label, "first name", "given name", "forename"):
		return pi.Name
	case matchesAny(label, "last name", "surname", "family name"):
		return pi.Surname
	case matchesAny(label, "phone", "mobile", "telephone"):
		return pi.Phone
	case matchesAny(label, "email"):
		return pi.Email
	case matchesAny(label, "city", "town"):
		return pi.City
	case matchesAny(label, "country"):
		return pi.Country
	case matchesAny(label, "zip", "postcode", "address"):
		return pi.Address
	case matchesAny(label, "notice", "availability"):
		return p.ApplicationDefaults.NoticePeriod
	case matchesAny(label, "salary", "compensation"):
		return currentSalaryExpectation(p)
	}
	return ""
}
