package bot

// snapshot.go — versioned, privacy-minimized form step snapshot for the adaptive engine.
//
// CaptureStepSnapshot transforms the in-memory []formField from jsScanFields into a
// StepSnapshot. No browser/Rod dependency: takes already-scanned fields as input.
// Privacy: candidate-filled values are never stored — only Filled:bool.
// Bounds: capped at maxSnapFields per step and maxSnapOptions per field.

import (
	"crypto/sha256"
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
	Ref           string        `json:"ref"`                     // snapshot-scoped opaque token "f0".."fN"
	Label         string        `json:"label"`                   // normalized visible label
	Group         string        `json:"group,omitempty"`         // fieldset/legend/section heading
	Type          SnapFieldType `json:"type"`
	Options       []string      `json:"options,omitempty"`       // option labels; radio/select only
	Required      bool          `json:"required,omitempty"`
	Disabled      bool          `json:"disabled,omitempty"`
	MaxChars      int           `json:"max_chars,omitempty"`
	ValidationErr string        `json:"validation_err,omitempty"`
	Filled        bool          `json:"filled,omitempty"` // current value present (privacy: value not stored)
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
	V          int         `json:"v"`                    // schema version = SnapshotSchemaV1
	Platform   string      `json:"platform"`
	StepIndex  int         `json:"step_index"`
	CapturedAt time.Time   `json:"captured_at"`
	StepTitle  string      `json:"step_title,omitempty"`
	Fields     []SnapField `json:"fields"`
	NavActions []SnapNav   `json:"nav_actions,omitempty"`
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
// meaning: platform, normalized label, type, and sorted option labels.
//
// Two fields with the same label and options on the same platform produce the same
// signature regardless of which step they appear in, enabling field-level cache reuse
// when a new field is added to an otherwise-familiar page.
//
// Changing any qualifier in the label (e.g. "minimum 2 years" → "minimum 5 years")
// or changing the available options invalidates the signature for that field only.
func (f SnapField) FieldSignature(platform string) string {
	opts := make([]string, len(f.Options))
	copy(opts, f.Options)
	sort.Strings(opts)
	raw := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(platform)),
		strings.ToLower(strings.TrimSpace(f.Label)),
		string(f.Type),
		strings.Join(opts, ";"),
	}, "\x00")
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("fs1:%x", h[:8])
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
			Ref:      fmt.Sprintf("f%d", i),
			Label:    normalizeLabel(f.Question),
			Required: f.Required,
			MaxChars: f.MaxChars,
			Filled:   false, // privacy: never store the actual value
		}

		// Detect whether the field currently has a value (without storing it).
		// For file fields: HasFile signals presence.
		if f.Type == "file" {
			sf.Filled = f.HasFile
		}

		// Determine snapshot field type.
		sf.Type = snapTypeFrom(f)

		optN := min(len(f.Options), maxSnapOptions)
		if optN > 0 {
			sf.Options = make([]string, optN)
		for j := range optN {
				lbl := f.Options[j].Label
				if len(lbl) > maxSnapOptLabel {
					lbl = lbl[:maxSnapOptLabel]
				}
				sf.Options[j] = lbl
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
		case "number":
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
