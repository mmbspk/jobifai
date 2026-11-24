package bot

import "strings"

func optionTypographyKey(value string) string {
	value = strings.NewReplacer("’", "'", "‘", "'", "“", `"`, "”", `"`).Replace(value)
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

// Resolve typography only, never a semantic synonym or a first-option guess.
// Return the exact current label so verification, filling and reuse agree.
func currentOptionTypography(field SnapField, value string) (string, bool) {
	key := optionTypographyKey(value)
	match, count := "", 0
	for _, option := range field.Options {
		if key != "" && optionTypographyKey(option) == key {
			match, count = option, count+1
		}
	}
	if count != 1 {
		return "", false
	}
	return match, true
}
