package candidate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Spec identifies one eval configuration (model + effort + limits).
type Spec struct {
	Provider   string  `json:"provider"`
	Model      string  `json:"model"`
	Effort     string  `json:"effort,omitempty"`
	MaxTokens  int     `json:"max_tokens,omitempty"`
	TimeoutSec int     `json:"timeout_sec,omitempty"`
}

func (s Spec) ID() string {
	e := strings.ToLower(strings.TrimSpace(s.Effort))
	if e == "" {
		e = "default"
	}
	return fmt.Sprintf("%s/%s/effort=%s", s.Provider, s.Model, e)
}

// ParseList parses JSON array of candidate specs.
func ParseList(raw string) ([]Spec, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var list []Spec
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	for i, s := range list {
		if s.Provider == "" || s.Model == "" {
			return nil, fmt.Errorf("candidate %d missing provider/model", i)
		}
	}
	return list, nil
}
