package llm

import (
	"fmt"
	"strings"
)

// Anthropic effort levels per current Messages API output_config (verify against official docs).
var anthropicEffortLevels = map[string]bool{
	"low": true, "medium": true, "high": true, "max": true,
}

// NormalizeEffort maps user input to a provider-scoped effort level.
func NormalizeEffort(provider, raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", nil
	}
	switch provider {
	case "claude":
		if !anthropicEffortLevels[s] {
			return "", fmt.Errorf("anthropic effort %q not supported (use low, medium, high, max)", raw)
		}
		return s, nil
	case "openai", "gemini":
		return "", fmt.Errorf("effort not implemented for provider %q", provider)
	default:
		return "", fmt.Errorf("effort not supported for provider %q", provider)
	}
}

// EffortSupported reports whether provider/model accepts the normalized effort value.
func EffortSupported(provider, model, effort string) bool {
	if effort == "" {
		return true
	}
	switch provider {
	case "claude":
		if !anthropicEffortLevels[effort] {
			return false
		}
		return ModelSupportsAnthropicEffort(model)
	default:
		return false
	}
}

// ModelSupportsAnthropicEffort uses catalog-style model id checks (not Haiku unless documented).
func ModelSupportsAnthropicEffort(model string) bool {
	m := strings.ToLower(model)
	if strings.Contains(m, "claude-haiku") {
		return false
	}
	return strings.Contains(m, "claude-sonnet") || strings.Contains(m, "claude-opus")
}

// ModelSupportsVision reports whether the model can accept image input for form_vision.
func ModelSupportsVision(provider, model string) bool {
	if provider != "claude" {
		return false
	}
	m := strings.ToLower(model)
	return strings.Contains(m, "claude-sonnet") || strings.Contains(m, "claude-haiku") || strings.Contains(m, "claude-opus")
}
