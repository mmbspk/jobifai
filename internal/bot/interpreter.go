package bot

// interpreter.go — Step interpreter for the adaptive engine (story #131).
//
// InterpretStep resolves form fields in three passes:
//   1. Deterministic handlers (file upload, unambiguous profile lookups)
//   2. Verified mapping lookup (hook for story #137; no-op until implemented)
//   3. Single batched LLM request for all remaining fields
//
// The interpreter never executes browser actions — it only produces proposals.
// Resolution sources and pending reasons are typed so callers can persist them.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

// ResolutionSource identifies how a field was resolved.
type ResolutionSource string

const (
	// SourceDeterministic: resolved by rule without any model call.
	SourceDeterministic ResolutionSource = "deterministic"
	// SourceProfile: resolved from candidate profile data directly.
	SourceProfile ResolutionSource = "profile"
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
	// FieldIntentLLM: answer is an opaque string from the LLM, stored verbatim.
	FieldIntentLLM FieldIntentType = "llm"
)

// VerificationProvenance records how a mapping became trusted enough to be reused.
type VerificationProvenance string

const (
	// ProvenancePending: proposed but not yet verified — never written to the DB.
	ProvenancePending VerificationProvenance = "pending"
	// ProvenanceAcceptedByForm: form accepted the value during ExecuteStep.
	ProvenanceAcceptedByForm VerificationProvenance = "accepted_by_form"
	// ProvenanceConfirmedByReviewer: human reviewer explicitly promoted the mapping.
	ProvenanceConfirmedByReviewer VerificationProvenance = "confirmed_by_reviewer"
)

// FieldResolution is the resolution outcome for one form field.
type FieldResolution struct {
	Ref        string                `json:"ref"`
	Source     ResolutionSource      `json:"source"`
	Action     AllowedAction         `json:"action"`
	Value      string                `json:"value,omitempty"`
	Pending    bool                  `json:"pending,omitempty"`
	Reason     PendingReason         `json:"reason,omitempty"`
	IntentType FieldIntentType       `json:"intent_type,omitempty"`
	BindingKey string                `json:"binding_key,omitempty"`
	Provenance VerificationProvenance `json:"provenance,omitempty"`
}

// StepInterpretation is the result of interpreting all fields in a snapshot step.
type StepInterpretation struct {
	SnapshotSig string            `json:"snapshot_sig"` // from StepSnapshot.SemanticSignature()
	Fields      []FieldResolution `json:"fields"`
	Paused      bool              `json:"paused"`       // any field is Pending
	PauseReason PendingReason     `json:"pause_reason,omitempty"`
	LLMUsed     bool              `json:"llm_used"`
}

// LLMFieldProposal is one field's proposal from the LLM batch response.
type LLMFieldProposal struct {
	Ref        string          `json:"ref"`
	Action     string          `json:"action"`      // "fill" | "upload" | "skip"
	Value      string          `json:"value"`
	IntentType FieldIntentType `json:"intent_type"` // "profile" | "llm"
	BindingKey string          `json:"binding_key"` // non-empty when intent_type="profile"
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

func (nopMappingLookup) LookupField(_, _ string) (FieldResolution, bool) { return FieldResolution{}, false }

// StepInterpreter drives the three-pass resolution pipeline.
type StepInterpreter struct {
	LLM     StepInterpreterLLM // nil → all ambiguous fields become review
	Mapping MappingLookup      // nil → no mapping lookups
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

	var toLLM []string // refs of fields that need LLM resolution

	for _, f := range snap.Fields {
		if f.Filled {
			// Already has a value — skip; don't re-fill.
			continue
		}

		// Pass 1: deterministic
		if res, ok := deterministicResolve(f, profile); ok {
			result.Fields = append(result.Fields, res)
			continue
		}

		// Pass 2: field-level mapping lookup.
		// fieldSig encodes platform + group + label + type + options — unique per field identity.
		if res, ok := mapping.LookupField(f.FieldSignature(snap.Platform), snap.EmployerKey); ok {
			res.Ref = f.Ref
			res.Source = SourceMapping
			// Profile-bound entries: resolve the current value from the live profile so
			// the warm hit always reflects the candidate's latest data.
			if res.IntentType == FieldIntentProfile && res.BindingKey != "" && profile != nil {
				if v, resolved := resolveProfileBinding(res.BindingKey, profile); resolved {
					res.Value = v
				}
			}
			result.Fields = append(result.Fields, res)
			continue
		}

		// Not resolved yet — queue for LLM.
		toLLM = append(toLLM, f.Ref)
	}

	// Pass 3: batch LLM for remaining fields.
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
				reason := pendingReasonFromErr(err)
				for _, ref := range toLLM {
					result.Fields = append(result.Fields, FieldResolution{
						Ref:     ref,
						Source:  SourceReview,
						Action:  ActionSkip,
						Pending: true,
						Reason:  reason,
					})
				}
			} else {
				llmResolutions := validateLLMProposals(proposals, toLLM, snap, profile)
				result.Fields = append(result.Fields, llmResolutions...)
				result.LLMUsed = true
			}
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
	if matchesAny(label, "notice period", "notice", "availability") && ad.NoticePeriod != "" {
		return makeText(f.Ref, ad.NoticePeriod), true
	}
	if matchesAny(label, "salary", "expected salary", "salary expectation", "desired salary", "compensation") && ad.SalaryExpectation != "" {
		return makeText(f.Ref, ad.SalaryExpectation), true
	}

	// Sponsorship (yes/no radio/select).
	if (f.Type == SnapTypeRadio || f.Type == SnapTypeSelect) && matchesAny(label,
		"require sponsorship", "need sponsorship", "visa sponsorship", "work authorization",
		"authorized to work", "right to work") {
		ans := "No"
		if ad.RequiresSponsorship {
			ans = "Yes"
		}
		if containsOptionIgnoreCase(f.Options, ans) {
			return FieldResolution{
				Ref:    f.Ref,
				Source: SourceProfile,
				Action: ActionFill,
				Value:  ans,
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
			Pending: true,
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

// validateLLMProposals validates LLM proposals against the snapshot and builds FieldResolutions.
// Fields in toLLM that have no valid proposal become review/pending.
// Profile-intent proposals are resolved from the live profile at validate time.
func validateLLMProposals(proposals []LLMFieldProposal, toLLM []string, snap StepSnapshot, profile *domain.ResumeProfile) []FieldResolution {
	// Index proposals by ref.
	byRef := make(map[string]LLMFieldProposal, len(proposals))
	for _, p := range proposals {
		byRef[p.Ref] = p
	}

	var out []FieldResolution
	for _, ref := range toLLM {
		f, fieldOK := snap.FieldByRef(ref)
		p, propOK := byRef[ref]

		if !propOK || !fieldOK {
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
		if intentType == FieldIntentProfile && bindingKey != "" {
			if v, ok := resolveProfileBinding(bindingKey, profile); ok {
				value = v
			} else {
				// Unknown binding key — degrade to opaque LLM intent.
				intentType = FieldIntentLLM
				bindingKey = ""
			}
		}
		if intentType == "" {
			intentType = FieldIntentLLM
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

		out = append(out, FieldResolution{
			Ref:        ref,
			Source:     SourceLLM,
			Action:     action,
			Value:      value,
			IntentType: intentType,
			BindingKey: bindingKey,
		})
	}
	return out
}

// ErrLLMUnavailable signals the LLM provider could not fulfil the request.
var ErrLLMUnavailable = errors.New("interpreter: LLM unavailable")

// ErrLLMLimitExceeded signals the request exceeded the LLM's budget or token limit.
var ErrLLMLimitExceeded = errors.New("interpreter: LLM limit exceeded")

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
		"city", "town", "country", "zip", "postcode", "address",
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
		return p.ApplicationDefaults.SalaryExpectation
	}
	return ""
}
