package quota

import "strings"

// ModelCost holds per-million-token prices for a model (USD).
type ModelCost struct {
	InputPerM  float64
	OutputPerM float64
}

var costTable = []struct {
	prefix string
	cost   ModelCost
}{
	{"claude-opus", ModelCost{15.00, 75.00}},
	{"claude-sonnet", ModelCost{3.00, 15.00}},
	{"claude-haiku", ModelCost{0.25, 1.25}},
	{"gpt-4o", ModelCost{2.50, 10.00}},
	{"gpt-4", ModelCost{30.00, 60.00}},
	{"gpt-3.5", ModelCost{0.50, 1.50}},
}

// LookupCost returns pricing for a model name prefix, or false for unknown models.
func LookupCost(model string) (ModelCost, bool) {
	lower := strings.ToLower(model)
	for _, entry := range costTable {
		if strings.HasPrefix(lower, entry.prefix) {
			return entry.cost, true
		}
	}
	return ModelCost{}, false
}

// MicroUSDFromTokens converts token counts to micro-USD (1 USD = 1_000_000 micro).
func MicroUSDFromTokens(model string, inputTokens, outputTokens int64) int64 {
	c, ok := LookupCost(model)
	if !ok {
		return 0
	}
	usd := float64(inputTokens)/1_000_000*c.InputPerM +
		float64(outputTokens)/1_000_000*c.OutputPerM
	if usd <= 0 {
		return 0
	}
	return int64(usd * 1_000_000)
}

// USDFromMicro converts stored micro-USD to float dollars for API responses.
func USDFromMicro(micro int64) float64 {
	return float64(micro) / 1_000_000
}

// MicroFromUSD converts admin-entered dollars to micro-USD.
func MicroFromUSD(usd float64) int64 {
	if usd <= 0 {
		return 0
	}
	return int64(usd * 1_000_000)
}
