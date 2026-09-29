package llm

import (
	"fmt"
	"strings"
)

// TaskRuntime holds per-task options resolved from settings or policy.
type TaskRuntime struct {
	Effort         string
	Mode           string
	MaxCostUSD     float64
	TimeoutSec     int
	FallbackModels []string // stored for eval/admin; production auto-fallback disabled
}

// NormalizeEffort maps user input to a canonical effort level or returns an error.
func NormalizeEffort(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", nil
	}
	switch s {
	case "minimal", "low", "medium", "high", "xhigh":
		return s, nil
	default:
		return "", fmt.Errorf("unsupported effort %q", raw)
	}
}

// EffortSupported reports whether provider/model accepts the normalized effort value.
func EffortSupported(provider, model, effort string) bool {
	if effort == "" {
		return true
	}
	switch provider {
	case "claude":
		// Anthropic effort via output_config (Sonnet 4.5+ / supported snapshots).
		return strings.Contains(model, "claude-sonnet") || strings.Contains(model, "claude-haiku-4-5")
	case "openai":
		return strings.HasPrefix(model, "gpt-5") || strings.HasPrefix(model, "o")
	case "gemini":
		return strings.Contains(model, "gemini-3")
	default:
		return false
	}
}
