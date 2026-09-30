package preflight

import (
	"context"
	"strings"

	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/dataset"
)

// Status of a candidate configuration before/during eval.
type Status struct {
	State              string
	Reason             string
	ProviderErrorCode  string
}

const (
	StateRunnable           = "runnable"
	StateUnavailableModel   = "unavailable_model"
	StateMissingCredential  = "missing_credential"
	StateCapabilityMismatch = "capability_mismatch"
	StatePricingUnresolved  = "pricing_unresolved"
)

// CaseRunner executes one eval case (same as engine.Runner).
type CaseRunner interface {
	RunCase(ctx context.Context, task string, spec candidate.Spec, c dataset.Case) (output string, err error)
}

// ClassifyProviderError maps provider errors to candidate status.
func ClassifyProviderError(err error) Status {
	if err == nil {
		return Status{State: StateRunnable}
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "invalid_model"), strings.Contains(lower, "invalid model"):
		return Status{State: StateUnavailableModel, Reason: msg, ProviderErrorCode: "INVALID_MODEL"}
	case strings.Contains(lower, "credential"), strings.Contains(lower, "api key"), strings.Contains(lower, "unauthorized"):
		return Status{State: StateMissingCredential, Reason: msg}
	case strings.Contains(lower, "unsupported"), strings.Contains(lower, "capability"), strings.Contains(lower, "vision"):
		return Status{State: StateCapabilityMismatch, Reason: msg}
	default:
		return Status{State: StateRunnable, Reason: msg}
	}
}
