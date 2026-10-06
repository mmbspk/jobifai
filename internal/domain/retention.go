package domain

import "fmt"

const (
	DocumentRetentionDefault = 20
	DocumentRetentionMin     = 5
	DocumentRetentionMax     = 30
)

// ErrInvalidDocumentRetention is returned when admin policy is outside allowed bounds.
var ErrInvalidDocumentRetention = fmt.Errorf("latest_submitted_applications must be between %d and %d", DocumentRetentionMin, DocumentRetentionMax)

// DocumentRetentionDefaults is the deployment-wide submitted-application PDF retention policy.
type DocumentRetentionDefaults struct {
	LatestSubmittedApplications int `json:"latest_submitted_applications"`
}

func (d DocumentRetentionDefaults) Normalized() DocumentRetentionDefaults {
	n := d.LatestSubmittedApplications
	if n <= 0 {
		n = DocumentRetentionDefault
	}
	if n < DocumentRetentionMin {
		n = DocumentRetentionMin
	}
	if n > DocumentRetentionMax {
		n = DocumentRetentionMax
	}
	return DocumentRetentionDefaults{LatestSubmittedApplications: n}
}

// Validate rejects out-of-range admin input (does not silently clamp).
func (d DocumentRetentionDefaults) Validate() error {
	n := d.LatestSubmittedApplications
	if n < DocumentRetentionMin || n > DocumentRetentionMax {
		return ErrInvalidDocumentRetention
	}
	return nil
}
