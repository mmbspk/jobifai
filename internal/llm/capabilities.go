package llm

import (
	"fmt"
	"strings"

	"github.com/user/jobifai/internal/pricing"
)

var anthropicEffortAll = map[string]bool{
	"low": true, "medium": true, "high": true, "xhigh": true, "max": true,
}

// NormalizeEffort maps user input to a provider-scoped effort level.
func NormalizeEffort(provider, raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", nil
	}
	switch provider {
	case "claude":
		if !anthropicEffortAll[s] {
			return "", fmt.Errorf("anthropic effort %q not supported", raw)
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
		allowed := AllowedAnthropicEfforts(model)
		return allowed[effort]
	default:
		return false
	}
}

// AllowedAnthropicEfforts returns effort levels supported for a Claude model id (aliases ok).
func AllowedAnthropicEfforts(model string) map[string]bool {
	canon := canonicalClaudeModel(model)
	switch canon {
	case "claude-sonnet-4-6":
		return subsetEffort("low", "medium", "high", "max")
	case "claude-sonnet-5-5":
		return subsetEffort("low", "medium", "high", "xhigh", "max")
	case "claude-sonnet-4-5", "claude-haiku-4-5":
		return nil
	default:
		if strings.Contains(strings.ToLower(model), "haiku") {
			return nil
		}
		if strings.Contains(strings.ToLower(model), "opus") {
			return subsetEffort("low", "medium", "high", "max")
		}
		return nil
	}
}

func subsetEffort(levels ...string) map[string]bool {
	out := map[string]bool{}
	for _, l := range levels {
		out[l] = true
	}
	return out
}

func canonicalClaudeModel(model string) string {
	c := pricing.DefaultCatalog()
	res, err := c.Resolve(model, false)
	if err == nil && res.Known && !res.UsedFallback {
		return strings.ToLower(res.Record.CanonicalID)
	}
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(m, "5-5") || strings.Contains(m, "5.5"):
		return "claude-sonnet-5-5"
	case strings.Contains(m, "4-6") || strings.Contains(m, "4.6"):
		return "claude-sonnet-4-6"
	case strings.Contains(m, "4-5") || strings.Contains(m, "4.5"):
		return "claude-sonnet-4-5"
	case strings.Contains(m, "haiku"):
		return "claude-haiku-4-5"
	}
	return m
}

// ModelSupportsAnthropicEffort is true when the model supports any effort level.
func ModelSupportsAnthropicEffort(model string) bool {
	return len(AllowedAnthropicEfforts(model)) > 0
}

// ModelSupportsVision reports whether the model can accept image input for form_vision.
func ModelSupportsVision(provider, model string) bool {
	if provider != "claude" {
		return false
	}
	c := pricing.DefaultCatalog()
	res, _ := c.Resolve(model, false)
	if res.Known && !res.UsedFallback {
		return res.Record.SupportsVision
	}
	m := strings.ToLower(model)
	return strings.Contains(m, "claude-sonnet") || strings.Contains(m, "claude-haiku") || strings.Contains(m, "claude-opus")
}
