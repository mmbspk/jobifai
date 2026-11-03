package documents

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// VersionUploadPayload is file bytes + metadata for platform upload.
type VersionUploadPayload struct {
	Data      []byte
	Filename  string
	MediaType string
}

// uploadFilename returns a version-scoped filename for platform uploads.
// Using a per-version name prevents the duplicate-filename guard from firing when the
// user already has a file with the same base name in the job-site library.
func uploadFilename(versionID, base, ext string) string {
	shortID := versionID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	if base == "" {
		base = "document"
	}
	return base + "_" + shortID + ext
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
		ext := filepath.Ext(filename)
		stem := strings.TrimSuffix(filepath.Base(filename), ext)
		return VersionUploadPayload{Data: raw, Filename: uploadFilename(versionID, stem, ext), MediaType: mediaType}, nil
	case ContentResumeJSON, ContentCoverText:
		pdf, _, _, err := s.PDFBytes(ctx, userID, versionID)
		if err != nil {
			return VersionUploadPayload{}, err
		}
		base, ext := "resume", ".pdf"
		if p.ContentKind == ContentCoverText {
			base = "cover_letter"
		}
		return VersionUploadPayload{Data: pdf, Filename: uploadFilename(versionID, base, ext), MediaType: "application/pdf"}, nil
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
