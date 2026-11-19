package bot

// adaptive_llm.go — production StepInterpreterLLM adapter for the adaptive engine.
//
// LLMFormInterpreter wraps a *llm.Client and implements StepInterpreterLLM.
// Each InterpretFields call issues a SINGLE structured JSON request for all
// unresolved fields, rather than one call per field. This reduces API round-trips
// proportionally to the number of fields on the step.
//
// Response format: JSON array, one object per field:
//
//	[{"ref":"f0","action":"fill","intent_type":"profile",
//	  "binding_key":"application_defaults.notice_period","value":""},
//	 {"ref":"f1","action":"fill","intent_type":"llm","binding_key":"","value":"5"}]
//
// When intent_type="profile" and binding_key is a known registry key, the value
// is resolved from the candidate's current profile by validateLLMProposals.
// Unknown binding keys degrade silently to intent_type="llm".

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/quota"
)

// LLMFormInterpreter adapts *llm.Client to StepInterpreterLLM for the adaptive engine.
type LLMFormInterpreter struct {
	client *llm.Client
}

// NewLLMFormInterpreter returns a LLMFormInterpreter that uses c for form-answer calls.
func NewLLMFormInterpreter(c *llm.Client) *LLMFormInterpreter {
	return &LLMFormInterpreter{client: c}
}

// InterpretFields issues ONE LLM call for all unresolved fields and returns proposals.
// Profile is serialized once and included in the single batch prompt. All LLM errors are
// mapped to ErrLLMLimitExceeded (quota) or ErrLLMUnavailable (all others).
func (a *LLMFormInterpreter) InterpretFields(ctx context.Context, profile *domain.ResumeProfile, snap StepSnapshot, fieldRefs []string) ([]LLMFieldProposal, error) {
	if a == nil || a.client == nil {
		return nil, ErrLLMUnavailable
	}
	if len(fieldRefs) == 0 {
		return nil, nil
	}

	profileJSON, err := json.Marshal(profile)
	if err != nil {
		return nil, ErrLLMUnavailable
	}

	// Build field descriptors for the batch prompt.
	type fieldDesc struct {
		Ref      string   `json:"ref"`
		Label    string   `json:"label"`
		Group    string   `json:"group,omitempty"`
		HelpText string   `json:"help_text,omitempty"`
		Type     string   `json:"type"`
		Options  []string `json:"options,omitempty"`
	}
	fields := make([]fieldDesc, 0, len(fieldRefs))
	for _, ref := range fieldRefs {
		f, ok := snap.FieldByRef(ref)
		if !ok {
			continue
		}
		fields = append(fields, fieldDesc{
			Ref:      ref,
			Label:    f.Label,
			Group:    f.Group,
			HelpText: f.HelpText,
			Type:     string(f.Type),
			Options:  f.Options,
		})
	}

	fieldsJSON, err := json.Marshal(fields)
	if err != nil {
		return nil, ErrLLMUnavailable
	}

	prompt := buildBatchFormPrompt(profileJSON, fieldsJSON)
	msgs := []llm.Message{{Role: "user", Content: prompt}}

	raw, callErr := a.client.Chat(ctx, msgs)
	if callErr != nil {
		if errors.Is(callErr, quota.ErrExceeded) {
			return nil, ErrLLMLimitExceeded
		}
		return nil, fmt.Errorf("%w: %s", ErrLLMUnavailable, callErr.Error())
	}

	// Extract JSON array from the response (may be wrapped in markdown code fences).
	jsonText := extractJSONArray(raw)
	var proposals []LLMFieldProposal
	if err := json.Unmarshal([]byte(jsonText), &proposals); err != nil {
		return nil, fmt.Errorf("%w: batch response parse error: %s", ErrLLMUnavailable, err.Error())
	}
	return proposals, nil
}

// buildBatchFormPrompt constructs the single prompt for all unresolved fields.
func buildBatchFormPrompt(profileJSON, fieldsJSON []byte) string {
	keys := knownBindingKeys()
	sort.Strings(keys)
	bindingList := strings.Join(keys, "\n  ")

	return fmt.Sprintf(`You are filling in a job application form. You ARE the applicant — write every answer in first person (I, me, my) as if providing information directly. Never say "the candidate" or use third person.

Your profile (JSON):
%s

For each field below, return ONE JSON object in a JSON array. Use this exact schema:
{"ref":"...","action":"fill"|"skip","intent_type":"profile"|"llm","binding_key":"...","value":"..."}

Rules:
- action="fill" for text, radio, select, textarea fields; action="skip" only if truly unanswerable.
- If a field maps directly to a known profile binding key, set intent_type="profile", binding_key to the key, and value="" (the system resolves it at runtime from your profile).
- Known binding keys:
  %s
- For all other fields, set intent_type="llm", binding_key="", and value=your answer.
- For radio/select fields, value must be EXACTLY one of the declared options.
- For yes/no fields about skills in your profile, answer "Yes".
- For "how many years experience with [X]", return a COUNT (integer), never a calendar year.
- Use application_defaults fields (notice_period, salary_expectation, requires_sponsorship) when relevant.
- Return ONLY a valid JSON array — no prose, no markdown, no code fences.

Fields to interpret:
%s`, string(profileJSON), bindingList, string(fieldsJSON))
}

// extractJSONArray finds the first '[' ... ']' block in raw, tolerating markdown fences.
func extractJSONArray(raw string) string {
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start == -1 || end == -1 || end < start {
		return raw // return as-is; json.Unmarshal will error
	}
	return raw[start : end+1]
}
