package quota

import "errors"

var (
	// ErrExceeded is returned when the user cannot make another LLM call.
	ErrExceeded = errors.New("quota exceeded")
)

// ExceededError carries a stable code for HTTP responses.
type ExceededError struct {
	Code  string // trial_exhausted | period_exhausted | grace_exceeded
	Scope string // trial | period
}

func (e *ExceededError) Error() string { return ErrExceeded.Error() }

func (e *ExceededError) Unwrap() error { return ErrExceeded }
