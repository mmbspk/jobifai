package documents

import "github.com/user/jobifai/internal/domain"

const (
	KindResume         = "resume"
	KindCoverLetter    = "cover_letter"
	KindOriginalUpload = "original_upload"

	ContentResumeJSON     = "resume_json"
	ContentCoverText      = "cover_text"
	ContentOriginalFileRef = "original_file_ref"

	SourceProfileRender = "profile_render"
	SourceUserEdit      = "user_edit"
	SourceAIGeneral     = "ai_general"
	SourceAIImprove     = "ai_improve"
	SourceOriginalUpload = "original_upload"

	ArtifactReady   = "ready"
	ArtifactPending = "pending"
)

// RendererVersion identifies the PDF HTML template + renderer implementation generation.
const RendererVersion = "jobifai-pdf-v1"

type Document struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Versions  []VersionSummary `json:"versions,omitempty"`
}

type VersionSummary struct {
	ID              string `json:"id"`
	VersionNumber   int    `json:"version_number"`
	Source          string `json:"source"`
	ContentKind     string `json:"content_kind"`
	Market          string `json:"market,omitempty"`
	Reconstructible bool   `json:"reconstructible"`
	HasPDF          bool   `json:"has_pdf"`
	CreatedAt       string `json:"created_at"`
}

type DefaultsView struct {
	ResumeVersionID      string `json:"resume_version_id,omitempty"`
	CoverLetterVersionID string `json:"cover_letter_version_id,omitempty"`
	ResumeOutdated       bool   `json:"resume_outdated"`
	CoverOutdated        bool   `json:"cover_outdated"`
	OutdatedReason       string `json:"outdated_reason,omitempty"`
}

type ListResponse struct {
	Documents []Document    `json:"documents"`
	Defaults  DefaultsView  `json:"defaults"`
}

type ResumeContent struct {
	Profile domain.ResumeProfile `json:"profile"`
}

type CoverContent struct {
	Body string `json:"body"`
}

type OriginalFileRef struct {
	OriginalFileID string `json:"original_file_id"`
	Filename       string `json:"filename"`
	Sha256         string `json:"sha256"`
	ByteSize       int64  `json:"byte_size"`
}
