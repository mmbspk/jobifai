package bot

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/user/jobifai/internal/domain"
)

// Work preferences remain owned by settings, rather than becoming duplicate
// editable resume fields. These bindings are independent of model proposals.
func knownWorkPreferenceBinding(key string) bool {
	switch key {
	case "work_preferences.remote", "work_preferences.hybrid", "work_preferences.onsite":
		return true
	}
	return false
}

func genericWorkArrangementBinding(field SnapField) string {
	if field.Disabled || field.Truncated || manualDeclaration(field) {
		return ""
	}
	if field.Type != SnapTypeRadio && field.Type != SnapTypeSelect && field.Type != SnapTypeText && field.Type != SnapTypeTextarea {
		return ""
	}
	if (field.Type == SnapTypeText || field.Type == SnapTypeTextarea) && field.InputType != "" && field.InputType != "text" && field.InputType != "textarea" {
		return ""
	}
	normalize := func(s string) string {
		return strings.ToLower(strings.Join(strings.Fields(strings.Trim(s, " *?:.!")), " "))
	}
	label := normalize(field.Label)
	switch normalize(field.Group) {
	case "", "additional questions", "application questions", "screening questions", "work preferences", "work arrangement", "work arrangements", "working arrangements":
	default:
		if normalize(field.Group) != label {
			return "" // surrounding qualifications cannot be discarded
		}
	}
	switch normalize(field.HelpText) {
	case "", "required", "select an option", "select one":
	default:
		return ""
	}
	for _, mode := range []struct{ word, binding string }{
		{"hybrid", "work_preferences.hybrid"}, {"remote", "work_preferences.remote"}, {"remotely", "work_preferences.remote"},
		{"onsite", "work_preferences.onsite"}, {"on-site", "work_preferences.onsite"}, {"on site", "work_preferences.onsite"},
	} {
		for _, question := range []string{
			"are you comfortable working " + mode.word,
			"are you comfortable working in a " + mode.word + " setting",
			"are you comfortable working in a " + mode.word + " environment",
			"are you comfortable working in a " + mode.word + " arrangement",
			"are you willing to work " + mode.word,
			"are you willing to work in a " + mode.word + " setting",
			"would you be willing to work " + mode.word,
			"would you be willing to work in a " + mode.word + " setting",
			"are you happy to work " + mode.word,
			"are you open to " + mode.word + " work",
			"are you open to working " + mode.word,
		} {
			if label == question || label == strings.Replace(question, " in a ", " in an ", 1) {
				return mode.binding
			}
		}
	}
	return ""
}

func workPreferenceResolution(field SnapField, preferences *domain.WorkPreferences, profile *domain.ResumeProfile) (FieldResolution, bool) {
	binding := genericWorkArrangementBinding(field)
	if binding == "" || preferences == nil || profile != nil && strings.TrimSpace(profile.ApplicationDefaults.CommuteConditions) != "" {
		return FieldResolution{}, false
	}
	selected := false
	switch binding {
	case "work_preferences.remote":
		selected = preferences.Remote
	case "work_preferences.hybrid":
		selected = preferences.Hybrid
	case "work_preferences.onsite":
		selected = preferences.Onsite
	}
	if !selected {
		// Not selecting a search preference is not an explicit declaration of
		// unwillingness. In particular, it must not retain a prior cached Yes.
		return FieldResolution{}, false
	}
	value := "Yes"
	if field.Type == SnapTypeRadio || field.Type == SnapTypeSelect {
		matches := 0
		for _, option := range field.Options {
			if strings.EqualFold(strings.TrimSpace(option), "Yes") {
				value, matches = option, matches+1
			}
		}
		if matches != 1 {
			return FieldResolution{}, false
		}
	}
	if !bindingValueCompatible(field, binding, value) {
		return FieldResolution{}, false
	}
	return FieldResolution{Ref: field.Ref, Source: SourcePreferences, Action: ActionFill, Value: value, IntentType: FieldIntentPreferences, BindingKey: binding}, true
}

func (b *Bot) currentPreferences() *domain.WorkPreferences {
	if b.cfg.PreferencesLoader != nil {
		return b.cfg.PreferencesLoader()
	}
	preferences := b.cfg.Preferences
	return &preferences
}

func workPreferenceDigest(preferences *domain.WorkPreferences) string {
	if preferences == nil {
		return ""
	}
	raw, _ := json.Marshal([]bool{preferences.Remote, preferences.Hybrid, preferences.Onsite})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func (b *Bot) guardWorkPreferenceResolutions(lazy *lazyDocGen, snap StepSnapshot, interp StepInterpretation, profile *domain.ResumeProfile) error {
	if lazy == nil {
		return nil
	}
	var preferences *domain.WorkPreferences
	for _, resolution := range interp.Fields {
		if resolution.IntentType != FieldIntentPreferences || resolution.Pending || resolution.Action != ActionFill {
			continue
		}
		if preferences == nil {
			preferences = b.currentPreferences()
		}
		field, exists := snap.FieldByRef(resolution.Ref)
		current, supported := workPreferenceResolution(field, preferences, profile)
		if !exists || !supported || current.BindingKey != resolution.BindingKey || current.Value != resolution.Value {
			return errors.New("current work preference no longer supports this answer; retry with current facts")
		}
		if lazy.answerPreferencesDigest == "" {
			lazy.answerPreferencesDigest = workPreferenceDigest(preferences)
			// Preference answers also depend on profile commute conditions. Keep
			// those facts current even when no answer-review recorder is wired.
			lazy.answerProfileDigest = profileDigest(profile)
		}
	}
	return nil
}
