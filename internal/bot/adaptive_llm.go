package bot

// adaptive_llm.go — production StepInterpreterLLM adapter for the adaptive engine.
//
// LLMFormInterpreter wraps a *llm.Client configured for the form_filling task
// and implements StepInterpreterLLM. Each InterpretFields call issues one
// LLM Chat call per unresolved field using BuildFormAnswerPrompt.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/quota"
	"github.com/user/jobifai/internal/resume"
)

// LLMFormInterpreter adapts *llm.Client to StepInterpreterLLM for the adaptive engine.
type LLMFormInterpreter struct {
	client *llm.Client
}

// NewLLMFormInterpreter returns a LLMFormInterpreter that uses c for form_filling calls.
// c must be pre-configured for the "form_filling" task (task model override applied).
func NewLLMFormInterpreter(c *llm.Client) *LLMFormInterpreter {
	return &LLMFormInterpreter{client: c}
}

// InterpretFields issues one LLM call per unresolved field and returns proposals.
// Profile is serialized once and reused across the batch. All LLM errors are
// mapped to ErrLLMLimitExceeded (quota) or ErrLLMUnavailable (all others) so
// the interpreter can produce typed pending reasons without retrying.
func (a *LLMFormInterpreter) InterpretFields(ctx context.Context, profile *domain.ResumeProfile, snap StepSnapshot, fieldRefs []string) ([]LLMFieldProposal, error) {
	if a == nil || a.client == nil {
		return nil, ErrLLMUnavailable
	}

	profileJSON, err := json.Marshal(profile)
	if err != nil {
		return nil, ErrLLMUnavailable
	}

	var proposals []LLMFieldProposal
	for _, ref := range fieldRefs {
		f, ok := snap.FieldByRef(ref)
		if !ok {
			continue
		}

		answer, callErr := a.callField(ctx, profileJSON, f)
		if callErr != nil {
			if errors.Is(callErr, quota.ErrExceeded) {
				return nil, ErrLLMLimitExceeded
			}
			return nil, fmt.Errorf("%w: %s", ErrLLMUnavailable, callErr.Error())
		}

		action := string(ActionFill)
		if f.Type == SnapTypeFile {
			action = string(ActionUpload)
		}
		proposals = append(proposals, LLMFieldProposal{
			Ref:    ref,
			Action: action,
			Value:  answer,
		})
	}
	return proposals, nil
}

// callField calls the LLM for a single form field and returns the trimmed answer.
func (a *LLMFormInterpreter) callField(ctx context.Context, profileJSON []byte, f SnapField) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"profile_json": json.RawMessage(profileJSON),
		"question":     f.Label,
		"options":      f.Options,
	})
	if err != nil {
		return "", err
	}
	msgs, err := resume.ProviderMessages(domain.TaskFormAnswer, payload)
	if err != nil {
		return "", err
	}
	answer, err := a.client.Chat(ctx, msgs)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(answer), nil
}
