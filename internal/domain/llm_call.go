package domain

// LLMCallContext carries billing-safe attribution for one logical LLM operation.
type LLMCallContext struct {
	Task             string
	UserID           string
	JobID            string
	ApplicationID    string
	AutomationRunID  string
	CorrelationID    string
	OperationID      string // stable idempotency key for one logical LLM operation
	Attempt          int
	// BypassReuse skips generation cache lookup (explicit fresh provider call).
	BypassReuse bool
}

// TaskModelPolicyState for production model workflow.
type TaskModelPolicyState string

const (
	PolicyStateCurrent      TaskModelPolicyState = "current"
	PolicyStateCandidate    TaskModelPolicyState = "candidate"
	PolicyStateRecommended  TaskModelPolicyState = "recommended"
	PolicyStateApproved     TaskModelPolicyState = "approved"
	PolicyStateRetired      TaskModelPolicyState = "retired"
)

// TaskModelPolicyRow is persisted admin policy per task (optional override of user settings).
type TaskModelPolicyRow struct {
	Task            string               `json:"task"`
	State           TaskModelPolicyState `json:"state"`
	Provider        string               `json:"provider"`
	Model           string               `json:"model"`
	FallbackModels  []string             `json:"fallback_models"`
	Mode            string               `json:"mode"`
	MaxTokens       int                  `json:"max_tokens"`
	Effort          string               `json:"effort,omitempty"`
	MaxCostUSD      float64              `json:"max_cost_usd,omitempty"`
	TimeoutSec      int                  `json:"timeout_sec,omitempty"`
	ApprovedAt      string               `json:"approved_at,omitempty"`
	ApprovedBy      string               `json:"approved_by,omitempty"`
	EvalRunID       string               `json:"eval_run_id,omitempty"`
}
