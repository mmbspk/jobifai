package domain

import "errors"

// Shared sentinel errors used across packages.
// Concrete packages (browser, config) wrap these with %w so callers
// can use errors.Is against these domain-level values without importing
// the concrete implementation package.
var (
	ErrNotFound         = errors.New("not found")
	ErrAlreadyOpen      = errors.New("already open")
	ErrSessionOwnership = errors.New("session belongs to a different user")
	ErrSessionNotFound  = errors.New("session not found")
)
