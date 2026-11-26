package bot

// snapshot.go — versioned, privacy-minimized form step snapshot for the adaptive engine.
//
// CaptureStepSnapshot transforms the in-memory []formField from jsScanFields into a
// StepSnapshot. No browser/Rod dependency: takes already-scanned fields as input.
// Privacy: candidate-filled values are never stored — only Filled:bool.
// Bounds: capped at maxSnapFields per step and maxSnapOptions per field.

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	SnapshotSchemaV1 = 1
	maxSnapFields    = 50
	maxSnapOptions   = 20
	maxSnapOptLabel  = 100 // bytes; truncated, never stripped
)

// SnapFieldType mirrors formField.Type / InputType in a closed set, so interpreters
// can exhaustively switch without handling unknown cases.
type SnapFieldType string

const (
	SnapTypeText        SnapFieldType = "text"
	SnapTypeNumber      SnapFieldType = "number"
	SnapTypeTextarea    SnapFieldType = "textarea"
	SnapTypeRadio       SnapFieldType = "radio"
	SnapTypeSelect      SnapFieldType = "select"
	SnapTypeCheckbox    SnapFieldType = "checkbox"
	SnapTypeFile        SnapFieldType = "file"
	SnapTypeTypeahead   SnapFieldType = "typeahead"
	SnapTypeUnsupported SnapFieldType = "unsupported"
)

// NavKind classifies a navigation action by intent.
type NavKind string

const (
	NavKindNext   NavKind = "next"
	NavKindBack   NavKind = "back"
	NavKindSubmit NavKind = "submit"
	NavKindCancel NavKind = "cancel"
	NavKindSkip   NavKind = "skip"
	NavKindOther  NavKind = "other"
)

// SnapField is one form control in the snapshot.
// No candidate-specific values: Filled is true/false only.
type SnapField struct {
	OptionValues []string `json:"option_values,omitempty"`
	// OptionSetHash covers every label/value pair while the model sees a bounded list.
	OptionSetHash string `json:"option_set_hash,omitempty"`
	Truncated     bool   `json:"truncated,omitempty"`
	// BrowserField preserves current scanned identity for local execution only.
	// It is excluded from model payloads and persisted snapshots.
	BrowserField      *formField    `json:"-"`
	Ref               string        `json:"ref"`                  // snapshot-scoped opaque token "f0".."fN"
	Label             string        `json:"label"`                // normalized visible label
	Group             string        `json:"group,omitempty"`      // fieldset/legend/section heading
	HelpText          string        `json:"help_text,omitempty"`  // hint / description text below the label
	ControlID         string        `json:"control_id,omitempty"` // DOM element id for direct fill targeting
	Type              SnapFieldType `json:"type"`
	Options           []string      `json:"options,omitempty"` // option labels; radio/select only
	Required          bool          `json:"required,omitempty"`
	Disabled          bool          `json:"disabled,omitempty"`
	MaxChars          int           `json:"max_chars,omitempty"`
	MinValue          string        `json:"min_value,omitempty"`
	MaxValue          string        `json:"max_value,omitempty"`
	StepValue         string        `json:"step_value,omitempty"`
	Pattern           string        `json:"pattern,omitempty"`
	InputType         string        `json:"input_type,omitempty"`
	Locale            string        `json:"locale,omitempty"`
	JobContext        string        `json:"job_context,omitempty"`
	ProfileRecordPath string        `json:"profile_record_path,omitempty"`
	JobTitle          string        `json:"job_title,omitempty"`
	JobID             string        `json:"job_id,omitempty"`
	JobLocation       string        `json:"job_location,omitempty"`
	ValidationErr     string        `json:"validation_err,omitempty"`
	Filled            bool          `json:"filled,omitempty"` // current value present (privacy: value not stored)
}

// SnapNav is a visible navigation action in the step.
type SnapNav struct {
	Ref   string  `json:"ref"`
	Label string  `json:"label"`
	Kind  NavKind `json:"kind"`
}

// StepSnapshot is a versioned, privacy-minimized point-in-time capture of one form step.
// Refs are only valid for the step that produced this snapshot; re-capture after any mutation.
type StepSnapshot struct {
	Truncated   bool        `json:"truncated,omitempty"`
	V           int         `json:"v"` // schema version = SnapshotSchemaV1
	Platform    string      `json:"platform"`
	StepIndex   int         `json:"step_index"`
	CapturedAt  time.Time   `json:"captured_at"`
	StepTitle   string      `json:"step_title,omitempty"`
	EmployerKey string      `json:"employer_key,omitempty"` // company / ATS tenant from the job posting
	Fields      []SnapField `json:"fields"`
	NavActions  []SnapNav   `json:"nav_actions,omitempty"`
}

// FieldByRef returns the SnapField for the given ref, and false if the ref is stale
// (not present in this snapshot). Always call this before acting on a ref.
func (s StepSnapshot) FieldByRef(ref string) (SnapField, bool) {
	for _, f := range s.Fields {
		if f.Ref == ref {
			return f, true
		}
	}
	return SnapField{}, false
}

// NavByRef returns the SnapNav for the given ref, and false if not present.
func (s StepSnapshot) NavByRef(ref string) (SnapNav, bool) {
	for _, n := range s.NavActions {
		if n.Ref == ref {
			return n, true
		}
	}
	return SnapNav{}, false
}

// FieldSignature returns a stable hash that identifies ONE form field by its observable
// meaning: platform, normalized label, group/section heading, type, help text, and
// sorted option labels.
//
// Two fields with the same label in DIFFERENT sections (groups) produce different
// signatures, preventing cross-section cache collisions (e.g. "Years of experience"
// under "Software Skills" vs. "Medical Skills").
//
// Changing any qualifier in the label (e.g. "minimum 2 years" → "minimum 5 years"),
// the section heading, or the available options invalidates the signature for that
// field only, leaving unaffected fields in the cache intact.
func (f SnapField) FieldSignature(platform string) string {
	opts := make([]string, len(f.Options))
	for i, label := range f.Options {
		value := ""
		if i < len(f.OptionValues) {
			value = f.OptionValues[i]
		}
		pair, _ := json.Marshal([]string{strings.ToLower(strings.TrimSpace(label)), value})
		opts[i] = string(pair)
	}
	sort.Strings(opts)
	if f.OptionSetHash != "" {
		opts = []string{f.OptionSetHash}
	}
	parts := []string{
		strings.ToLower(strings.TrimSpace(platform)),
		strings.ToLower(strings.TrimSpace(f.Group)),
		strings.ToLower(strings.TrimSpace(f.Label)),
		string(f.Type),
		strings.ToLower(strings.TrimSpace(f.HelpText)),
		strings.Join(opts, ";"),
		fmt.Sprintf("required=%t;max_chars=%d", f.Required, f.MaxChars),
		f.MinValue, f.MaxValue, f.StepValue, f.Pattern, f.InputType, strings.ToLower(f.Locale),
		f.JobContext,
	}
	if f.ProfileRecordPath != "" {
		parts = append(parts, "record:"+f.ProfileRecordPath)
	}
	raw, _ := json.Marshal(parts)
	h := sha256.Sum256(raw)
	return fmt.Sprintf("fs2:%x", h[:16])
}

// Context-dependent answers are reusable only for the same vacancy. Candidate
// facts such as contact information and named skills keep field-level reuse.
func scopeJobDependentFields(snap *StepSnapshot, job linkedInJob) {
	context, _ := json.Marshal([]string{job.ID, job.URL, normalizeLabel(job.Title), normalizeLabel(job.Location)})
	digest := sha256.Sum256(context)
	for i := range snap.Fields {
		f := &snap.Fields[i]
		// All questions receive the verified listing context so country-scoped
		// candidate facts can be checked even for generic question wording.
		f.JobTitle = job.Title
		f.JobID = job.ID
		f.JobLocation = job.Location
		question := f.Label + " " + f.Group + " " + f.HelpText
		// Eligibility and sponsorship answers can depend on the vacancy's
		// jurisdiction even when the visible label omits the country.
		if isJobDependentQuestion(question) || isWorkAuthorizationQuestion(question) || matchesAny(strings.ToLower(question), "sponsor") {
			f.JobContext = fmt.Sprintf("job:%x", digest[:16])
		}
	}
}

// SemanticSignature returns a stable hash that identifies the form step by its structure
// rather than its DOM identity. Used by story #137 to look up verified mappings.
//
// Inputs: platform, step title, sorted (label+type) pairs.
// Excludes: volatile IDs, CSS classes, option values, candidate answers, layout order.
func (s StepSnapshot) SemanticSignature() string {
	parts := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		parts[i] = strings.ToLower(strings.TrimSpace(f.Label)) + "|" + string(f.Type)
	}
	sort.Strings(parts)
	raw := strings.Join([]string{
		s.Platform,
		strings.ToLower(strings.TrimSpace(s.StepTitle)),
		strings.Join(parts, ";"),
	}, "\x00")
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("ss1:%x", h[:8]) // 8-byte prefix — collision-safe for practical form counts
}

// CaptureStepSnapshot converts already-scanned formFields into a versioned StepSnapshot.
// nav is optional; pass nil when nav actions are not known at capture time.
// stepTitle is the visible heading of the current step (empty string is fine).
func captureStepSnapshot(fields []formField, navActions []SnapNav, platform string, stepIndex int, stepTitle string) StepSnapshot {
	snap := StepSnapshot{
		Truncated:  len(fields) > maxSnapFields,
		V:          SnapshotSchemaV1,
		Platform:   platform,
		StepIndex:  stepIndex,
		CapturedAt: time.Now().UTC(),
		StepTitle:  strings.TrimSpace(stepTitle),
		NavActions: navActions,
	}

	n := min(len(fields), maxSnapFields)
	snap.Fields = make([]SnapField, 0, n)

	for i := range n {
		f := fields[i]
		sf := SnapField{
			BrowserField:      &f,
			Ref:               fmt.Sprintf("f%d", i),
			Label:             normalizeLabel(f.Question),
			Group:             normalizeLabel(f.Group),
			ProfileRecordPath: f.ProfileRecordPath,
			HelpText:          normalizeLabel(f.HelpText),
			ControlID:         f.ID,
			Required:          f.Required,
			MaxChars:          f.MaxChars,
			MinValue:          f.MinValue,
			MaxValue:          f.MaxValue,
			StepValue:         f.StepValue,
			Pattern:           f.Pattern,
			InputType:         f.InputType,
			Locale:            f.Locale,
			Filled:            false, // privacy: never store the actual value
		}
		if f.NativeType != "" {
			sf.InputType = f.NativeType
		}

		// Detect whether the field currently has a value (without storing it).
		// For file fields: HasFile signals presence.
		// A platform attachment is not proof that the currently approved document
		// is selected. The document filler verifies that against current policy.

		// Determine snapshot field type.
		sf.Type = snapTypeFrom(f)

		optN := min(len(f.Options), maxSnapOptions)
		sf.Truncated = len(f.Options) > maxSnapOptions
		for _, option := range f.Options {
			if len(option.Label) > maxSnapOptLabel {
				sf.Truncated = true
			}
		}
		if sf.Truncated {
			pairs := make([]string, len(f.Options))
			for j, option := range f.Options {
				pair, _ := json.Marshal([]string{strings.ToLower(strings.TrimSpace(option.Label)), option.Value})
				pairs[j] = string(pair)
			}
			sort.Strings(pairs)
			encoded, _ := json.Marshal(pairs)
			hash := sha256.Sum256(encoded)
			sf.OptionSetHash = fmt.Sprintf("%x", hash[:16])
		}
		if optN > 0 {
			sf.Options = make([]string, optN)
			sf.OptionValues = make([]string, optN)
			for j := range optN {
				lbl := f.Options[j].Label
				if len(lbl) > maxSnapOptLabel {
					lbl = lbl[:maxSnapOptLabel]
				}
				sf.Options[j] = lbl
				sf.OptionValues[j] = f.Options[j].Value
			}
		}

		snap.Fields = append(snap.Fields, sf)
	}

	return snap
}

// snapTypeFrom maps formField → SnapFieldType.
func snapTypeFrom(f formField) SnapFieldType {
	switch f.Type {
	case "file":
		return SnapTypeFile
	case "radio":
		return SnapTypeRadio
	case "select":
		return SnapTypeSelect
	case "checkbox":
		return SnapTypeCheckbox
	case "text":
		if f.Typeahead {
			return SnapTypeTypeahead
		}
		switch f.InputType {
		case "number", "numeric":
			return SnapTypeNumber
		case "textarea":
			return SnapTypeTextarea
		default:
			return SnapTypeText
		}
	}
	return SnapTypeUnsupported
}

// normalizeLabel trims and collapses internal whitespace.
func normalizeLabel(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
