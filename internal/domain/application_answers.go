package domain

import (
	"encoding/json"
	"errors"
)

var ErrApplicationAnswerConflict = errors.New("application answers changed; reload before saving")

// Only this explicit missing-fact observation may suppress a repeated model
// request. An ambiguous or incompatible proposal is not evidence of absence.
const ApplicationAnswerMissingFactReason = "A required profile fact is missing; this application was deferred"

// ApplicationAnswerTopic is a curated profile topic, never a learned variant.
type ApplicationAnswerTopic struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Core  bool   `json:"core"`
}

type ApplicationAnswerSource struct {
	Source   string   `json:"source"`
	Label    string   `json:"label"`
	Evidence []string `json:"evidence,omitempty"`
	State    string   `json:"state"` // unknown|confirmed|not_applicable; drafts use suggested sidecar
}

type ApplicationAnswersView struct {
	ApplicationDefaults ApplicationDefaults                `json:"application_defaults"`
	Revision            string                             `json:"revision"`
	ProfileRevision     string                             `json:"profile_revision"`
	Sources             map[string]ApplicationAnswerSource `json:"sources"`
	Catalog             []ApplicationAnswerTopic           `json:"catalog"`
	Certifications      []Certification                    `json:"certifications"`
	Preferences         WorkPreferences                    `json:"preferences"`
	Profile             *ResumeProfile                     `json:"profile,omitempty"`
}

type ApplicationAnswersPatch struct {
	Revision string                     `json:"revision"`
	Topics   map[string]json.RawMessage `json:"topics"`
	ImportID string                     `json:"import_id,omitempty"`
}

// ApplicationAnswerEncounter is an immutable observation owned by one user.
// GroupKey may be supplied only after equivalent question semantics have been
// established. An empty key falls back to the exact field signature.
type ApplicationAnswerEncounter struct {
	ID               string   `json:"id"`
	ReviewID         string   `json:"review_id,omitempty"`
	GroupKey         string   `json:"-"`
	FieldSignature   string   `json:"-"`
	FieldInstanceKey string   `json:"-"`
	MappingID        string   `json:"-"`
	Question         string   `json:"question"`
	Status           string   `json:"status"` // suggested|required; used derives from fill evidence
	ProposedAnswer   string   `json:"suggested_answer,omitempty"`
	AnswerUsed       string   `json:"answer_used,omitempty"`
	TopicID          string   `json:"topic_id,omitempty"`
	Country          string   `json:"country,omitempty"`
	Employer         string   `json:"employer,omitempty"`
	Platform         string   `json:"platform,omitempty"`
	JobTitle         string   `json:"job_title,omitempty"`
	JobID            string   `json:"job_id,omitempty"`
	JobScope         string   `json:"-"`
	ContextKey       string   `json:"-"` // choices, temporal/pay/credential qualifiers
	AttemptID        string   `json:"attempt_id,omitempty"`
	Evidence         []string `json:"evidence,omitempty"`
	Reason           string   `json:"reason"`
	Source           string   `json:"source"`
	ProfileRevision  string   `json:"profile_revision"`
	UseStatus        string   `json:"use_status"` // not_used|filled|replaced|submitted|submission_unknown
	CreatedAt        string   `json:"created_at"`
}

type ApplicationAnswerReview struct {
	ID              string                       `json:"id"`
	Question        string                       `json:"question"`
	Status          string                       `json:"status"`
	AnswerUsed      string                       `json:"answer_used,omitempty"`
	SuggestedAnswer string                       `json:"suggested_answer,omitempty"`
	Revision        int64                        `json:"revision"`
	ProfileRevision string                       `json:"profile_revision"`
	TopicID         string                       `json:"topic_id,omitempty"`
	Country         string                       `json:"country,omitempty"`
	Employer        string                       `json:"employer,omitempty"`
	Platform        string                       `json:"platform,omitempty"`
	JobTitle        string                       `json:"job_title,omitempty"`
	JobID           string                       `json:"job_id,omitempty"`
	Evidence        []string                     `json:"evidence,omitempty"`
	Reason          string                       `json:"reason"`
	Source          string                       `json:"source"`
	OccurrenceCount int                          `json:"occurrence_count"`
	Lifecycle       string                       `json:"lifecycle"`
	UseStatus       string                       `json:"use_status"`
	CreatedAt       string                       `json:"created_at"`
	UpdatedAt       string                       `json:"updated_at"`
	Encounters      []ApplicationAnswerEncounter `json:"encounters,omitempty"`
}

type ApplicationAnswerReviewPage struct {
	Items           []ApplicationAnswerReview `json:"items"`
	UnresolvedCount int                       `json:"unresolved_count"`
	UnreadCount     int                       `json:"unread_count"`
	NextCursor      string                    `json:"next_cursor,omitempty"`
}

type ApplicationAnswerConfirmation struct {
	Answer          string                     `json:"answer"`
	Revision        int64                      `json:"revision"`
	ProfileRevision string                     `json:"profile_revision,omitempty"`
	Topics          map[string]json.RawMessage `json:"topics,omitempty"`
}
