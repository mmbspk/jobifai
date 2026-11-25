package domain

import "encoding/json"

// CVAnswerPrefill is an unconfirmed upload suggestion. It travels beside the
// canonical profile, so evidence never becomes a candidate-authored fact merely
// because a document was imported. Value uses the named binding's existing type.
type CVAnswerPrefill struct {
	TopicID           string             `json:"topic_id"`
	Binding           string             `json:"binding"`
	Value             json.RawMessage    `json:"value"`
	Source            string             `json:"source"`
	ReviewState       string             `json:"review_state"`
	NeedsConfirmation bool               `json:"needs_confirmation,omitempty"`
	Warning           string             `json:"warning,omitempty"`
	Evidence          []CVAnswerEvidence `json:"evidence"`
}

// CVAnswerEvidence refers to exact text in the upload's extracted text. Offsets
// are zero-based UTF-8 byte offsets with an exclusive end. Page is present only
// when the source text includes explicit form-feed page boundaries.
type CVAnswerEvidence struct {
	Quote string `json:"quote"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Page  int    `json:"page,omitempty"`
}
