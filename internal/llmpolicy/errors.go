package llmpolicy

import "errors"

// ErrApprovedPolicy indicates an approved system task_model_policies row is invalid.
var ErrApprovedPolicy = errors.New("approved task policy invalid")
