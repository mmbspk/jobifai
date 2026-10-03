package domain

const (
	DocumentRetentionDefault = 20
	DocumentRetentionMin     = 5
	DocumentRetentionMax     = 30
)

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
