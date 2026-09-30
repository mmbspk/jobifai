package pricing

import "strings"

// ServedModelResolution maps a provider-reported model id to catalog pricing keys.
type ServedModelResolution struct {
	ActualModelRaw        string
	CanonicalPricingModel string
	ViaGatewayAlias       bool
}

// GatewayCanonicalModel maps known gateway/Bedrock served ids to approved catalog canonical ids.
func GatewayCanonicalModel(actual string) (canonical string, ok bool) {
	m := strings.ToLower(strings.TrimSpace(actual))
	if m == "" {
		return "", false
	}
	exact := map[string]string{
		"anthropic.claude-haiku-4-5-20251001-v1:0": "claude-haiku-4-5",
		"anthropic.claude-sonnet-5-5":              "claude-sonnet-5-5",
		"anthropic.claude-sonnet-4-6":              "claude-sonnet-4-6",
	}
	if c, hit := exact[m]; hit {
		return c, true
	}
	prefix := []struct{ prefix, canon string }{
		{"anthropic.claude-haiku-4-5", "claude-haiku-4-5"},
		{"anthropic.claude-sonnet-5-5", "claude-sonnet-5-5"},
		{"anthropic.claude-sonnet-4-6", "claude-sonnet-4-6"},
	}
	for _, p := range prefix {
		if strings.HasPrefix(m, p.prefix) {
			return p.canon, true
		}
	}
	return "", false
}

// ResolveServedModel returns pricing lookup key for an actual served model string.
func ResolveServedModel(actual string) ServedModelResolution {
	raw := strings.TrimSpace(actual)
	if canon, ok := GatewayCanonicalModel(raw); ok {
		return ServedModelResolution{
			ActualModelRaw: raw, CanonicalPricingModel: canon, ViaGatewayAlias: true,
		}
	}
	return ServedModelResolution{
		ActualModelRaw: raw, CanonicalPricingModel: raw, ViaGatewayAlias: false,
	}
}

// CatalogLookupKey chooses the model string passed to Catalog.Resolve for billing/eval.
func CatalogLookupKey(requested, actual string) string {
	if actual != "" {
		if r := ResolveServedModel(actual); r.CanonicalPricingModel != "" {
			return r.CanonicalPricingModel
		}
	}
	return strings.TrimSpace(requested)
}
