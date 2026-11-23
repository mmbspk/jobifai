package bot

import "encoding/json"

// Stored snapshots stay compact. An unresolved live dropdown can provide its
// complete public choices to interpretation within a separate bounded budget.
// The full option hash stays unchanged, so verified answers replay against the
// same full list and invalidate when the website changes it.
const maxInterpretOptions = 512
const maxInterpretOptionBytes = 16 * 1024

func expandInterpretationOptions(field SnapField) (SnapField, bool) {
	if !field.Truncated || field.BrowserField == nil || len(field.BrowserField.Options) > maxInterpretOptions {
		return field, false
	}
	labels := make([]string, len(field.BrowserField.Options))
	values := make([]string, len(labels))
	for i, option := range field.BrowserField.Options {
		labels[i], values[i] = option.Label, option.Value
	}
	encoded, err := json.Marshal(labels)
	if err != nil || len(encoded) > maxInterpretOptionBytes {
		return field, false
	}
	field.Options, field.OptionValues, field.Truncated = labels, values, false
	return field, true
}
