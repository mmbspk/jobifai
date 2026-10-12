package resource

import (
	"errors"

	"github.com/user/jobifai/internal/quota"
)

var (
	// ErrMeteringDisabled is returned by Before when resource metering is globally disabled.
	// Callers should treat this as a no-op (not an error to surface to users).
	ErrMeteringDisabled = errors.New("resource metering disabled")

	// ErrExceeded wraps quota.ErrExceeded for convenience — same semantic as the LLM gate.
	ErrExceeded = quota.ErrExceeded

	// ErrIdempotencyConflict indicates this resource event was already recorded.
	// The caller should treat this as a successful no-op (credits burned exactly once).
	ErrIdempotencyConflict = errors.New("resource usage idempotency key already recorded")
)
