package pricing

import "strings"

// legacyPrefixCost is the isolated backward-compat prefix table (mirrors quota/cost.go).
func legacyPrefixCost(model string) (inputPerM, outputPerM float64, ok bool) {
	lower := strings.ToLower(model)
	for _, entry := range []struct {
		prefix string
		in, out float64
	}{
		{"claude-opus", 15.00, 75.00},
		{"claude-sonnet", 3.00, 15.00},
		{"claude-haiku", 0.25, 1.25},
		{"gpt-4o", 2.50, 10.00},
		{"gpt-4", 30.00, 60.00},
		{"gpt-3.5", 0.50, 1.50},
	} {
		if strings.HasPrefix(lower, entry.prefix) {
			return entry.in, entry.out, true
		}
	}
	return 0, 0, false
}
