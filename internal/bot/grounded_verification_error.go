package bot

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/quota"
)

// Keep the underlying failure available to classification without placing a
// provider response or a candidate answer in the public application error.
type groundedVerificationError struct {
	cause error
	phase formJSONPhase
}

func (e *groundedVerificationError) malformed() bool {
	var syntax *json.SyntaxError
	var valueType *json.UnmarshalTypeError
	return errors.As(e.cause, &syntax) || errors.As(e.cause, &valueType)
}

func (e *groundedVerificationError) limited() bool {
	return errors.Is(e.cause, quota.ErrExceeded) || errors.Is(e.cause, ErrLLMLimitExceeded)
}

func (e *groundedVerificationError) Error() string {
	label := "semantic verification"
	if e.phase == formJSONProposal {
		label = "form answer proposal"
	}
	if e.limited() {
		return ErrLLMLimitExceeded.Error()
	}
	if e.malformed() {
		return "interpreter: " + label + " returned malformed output"
	}
	if errors.Is(e.cause, context.Canceled) || errors.Is(e.cause, context.DeadlineExceeded) {
		return "interpreter: " + label + " interrupted"
	}
	return "interpreter: LLM unavailable: " + label + " failed"
}

func (e *groundedVerificationError) Unwrap() error { return e.cause }

func (e *groundedVerificationError) Is(target error) bool {
	if target == ErrLLMLimitExceeded {
		return e.limited()
	}
	return target == ErrLLMUnavailable && !e.limited() && !e.malformed() &&
		!errors.Is(e.cause, context.Canceled) && !errors.Is(e.cause, context.DeadlineExceeded) &&
		!errors.Is(e.cause, llm.ErrBillingPersistFailed)
}
