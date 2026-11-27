package bot

import (
	"encoding/csv"
	"io"
	"strings"
)

// Keep the string answer contract. Multiple checkbox values use CSV labels,
// never opaque option values, guessed aliases or an arbitrary coerced array.
func checkboxSelections(field SnapField, value string) ([]string, bool) {
	if field.Type != SnapTypeCheckbox || len(value) > 8192 {
		return nil, false
	}
	if field.Truncated && field.BrowserField != nil {
		field.Options = field.BrowserField.optionLabels()
	}
	if label, unique := currentOptionTypography(field, value); unique {
		return []string{label}, true
	}
	reader := csv.NewReader(strings.NewReader(value))
	reader.TrimLeadingSpace = true
	values, err := reader.Read()
	if err != nil || len(values) < 1 || len(values) > maxSnapOptions {
		return nil, false
	}
	if _, err = reader.Read(); err != io.EOF {
		return nil, false
	}
	selected := make(map[string]bool, len(values))
	for _, value := range values {
		label, unique := currentOptionTypography(field, value)
		if !unique || selected[label] {
			return nil, false
		}
		selected[label] = true
		if len(values) > 1 {
			switch optionTypographyKey(label) {
			case "none", "none of these", "none of the above", "no", "n/a", "not applicable":
				return nil, false
			}
		}
	}
	labels := make([]string, 0, len(selected))
	for _, label := range field.Options {
		if selected[label] {
			labels = append(labels, label)
		}
	}
	return labels, len(labels) == len(values)
}

func canonicalCheckboxAnswer(field SnapField, value string) (string, bool) {
	labels, ok := checkboxSelections(field, value)
	if !ok {
		return "", false
	}
	if len(labels) == 1 {
		return labels[0], true
	}
	var out strings.Builder
	writer := csv.NewWriter(&out)
	if writer.Write(labels) != nil {
		return "", false
	}
	writer.Flush()
	if writer.Error() != nil {
		return "", false
	}
	return strings.TrimSpace(out.String()), true
}
