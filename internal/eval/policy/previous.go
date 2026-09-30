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
// FormatPolicyStateLabel turns stored previous/new JSON into admin-readable text.
func FormatPolicyStateLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "—"
	}
	if IsPreviousPolicyInherit(raw) {
		return "Inherited global configuration"
	}
	var row struct {
		Model    string `json:"model"`
		Provider string `json:"provider"`
		Mode     string `json:"mode"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		return raw
	}
	if row.Model == "" {
		return raw
	}
	if row.Provider != "" {
		return row.Provider + " / " + row.Model
	}
	return row.Model
}

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
