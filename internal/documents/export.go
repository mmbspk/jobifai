package documents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// ExportApplicationUploadFile writes bytes to a user- and version-scoped path with a unique temp file.
// Only this application export gets a job filename; saved document artifacts and
// previously recorded application paths retain their existing names.
func ExportApplicationUploadFile(root, userID, jobID, versionID, kind, filename string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("empty upload payload")
	}
	if filename == "" {
		if kind == "cover_letter" {
			filename = "cover_letter.pdf"
		} else {
			filename = "resume.pdf"
		}
	}
	filename = ApplicationUploadFilename(jobID, filename)
	seg := versionID
	if seg == "" {
		seg = uuid.NewString()
	}
	dir := filepath.Join(root, "job_applications", sanitizePath(userID), sanitizePath(jobID), sanitizePath(seg))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, filepath.Base(filename))
	tmp := filepath.Join(dir, "."+uuid.NewString()+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dest, nil
}

func sanitizePath(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, s)
	if s == "" {
		return "application"
	}
	if s == "." || s == ".." {
		return strings.Repeat("_", len(s))
	}
	return s
}
