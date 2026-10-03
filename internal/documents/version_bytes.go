package documents

import (
	"context"
	"fmt"
	"strings"
)

// VersionUploadPayload is file bytes + metadata for platform upload.
type VersionUploadPayload struct {
	Data     []byte
	Filename string
	MediaType string
}

// UploadPayloadForVersion returns bytes for upload (rendered PDF or original upload).
func (s *Service) UploadPayloadForVersion(ctx context.Context, userID, versionID string) (VersionUploadPayload, error) {
	_, _, p, err := s.Store.GetVersionRow(ctx, userID, versionID)
	if err != nil {
		return VersionUploadPayload{}, err
	}
	switch p.ContentKind {
	case ContentOriginalFileRef:
		raw, filename, mediaType, err := s.OriginalBytes(ctx, userID, versionID)
		if err != nil {
			return VersionUploadPayload{}, err
		}
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		return VersionUploadPayload{Data: raw, Filename: filename, MediaType: mediaType}, nil
	case ContentResumeJSON, ContentCoverText:
		pdf, _, err := s.PDFBytes(ctx, userID, versionID)
		if err != nil {
			return VersionUploadPayload{}, err
		}
		name := "resume.pdf"
		if p.ContentKind == ContentCoverText {
			name = "cover_letter.pdf"
		}
		return VersionUploadPayload{Data: pdf, Filename: name, MediaType: "application/pdf"}, nil
	default:
		return VersionUploadPayload{}, fmt.Errorf("unsupported content kind for upload")
	}
}

// AllowedUploadMedia reports whether the platform accepts this MIME (basic PDF/DOC guard).
func AllowedUploadMedia(mediaType string, allowDocx bool) bool {
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	switch {
	case mt == "application/pdf" || strings.HasSuffix(mt, "/pdf"):
		return true
	case allowDocx && (mt == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" || strings.Contains(mt, "word")):
		return true
	case mt == "application/msword":
		return allowDocx
	default:
		return false
	}
}
