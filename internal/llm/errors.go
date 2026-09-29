package llm

import "errors"

// ErrBillingPersistFailed is returned when the provider succeeded but durable billing did not.
var ErrBillingPersistFailed = errors.New("llm billing persistence failed")
