package policy

import (
	"encoding/json"
	"strings"
)

const previousPolicyInheritState = "inherit"

// PreviousPolicySnapshot records rollback state; inherit means no task-specific policy row.
type PreviousPolicySnapshot struct {
	State string `json:"state"`
	Task  string `json:"task,omitempty"`
}

// PreviousPolicyInheritJSON is stored in previous_json when the task had no approved policy.
func PreviousPolicyInheritJSON(task string) string {
	b, _ := json.Marshal(PreviousPolicySnapshot{State: previousPolicyInheritState, Task: task})
	return string(b)
}

// IsPreviousPolicyInherit reports whether a previous_json value means inherit defaults.
func IsPreviousPolicyInherit(previousJSON string) bool {
	previousJSON = strings.TrimSpace(previousJSON)
	if previousJSON == "" {
		return false
	}
	var snap PreviousPolicySnapshot
	if err := json.Unmarshal([]byte(previousJSON), &snap); err != nil {
		return false
	}
	return snap.State == previousPolicyInheritState
}
