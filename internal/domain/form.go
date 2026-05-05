package domain

// IdentifiedField is a form field identified by the LLM vision fallback.
// Used when jsScanFields returns nothing but visible inputs are present.
type IdentifiedField struct {
	Type     string   `json:"type"`     // "radio" | "select" | "text"
	Question string   `json:"question"`
	Options  []string `json:"options"`  // populated for radio/select; empty for free-text
}
