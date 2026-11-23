package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ApplicationUploadFilename adds the job identity to an application copy while
// preserving its document kind, version suffix and extension. A generic document
// without a job identity keeps its name. No saved document or history is renamed.
func ApplicationUploadFilename(jobID, filename string) string {
	filename = filepath.Base(filename)
	if jobID == "" {
		return filename
	}
	ext := filepath.Ext(filename)
	stem := strings.TrimSuffix(filename, ext)
	suffix := "_job_" + applicationJobFilenameToken(jobID)
	if strings.HasSuffix(stem, suffix) {
		return filename
	}
	versionSuffix := ""
	if i := strings.LastIndex(stem, "_"); i >= 0 && len(stem[i+1:]) == 8 {
		if _, err := hex.DecodeString(stem[i+1:]); err == nil {
			versionSuffix, stem = stem[i:], stem[:i]
		}
	}
	if strings.HasSuffix(stem, suffix) {
		return filename
	}
	// File components are commonly limited to 255 bytes. Leave room for the
	// suffix on long original-upload names; a digest retains their distinction.
	const maxFilenameBytes = 240
	if maxStem := maxFilenameBytes - len(suffix) - len(versionSuffix) - len(ext); len(stem) > maxStem {
		digest := sha256.Sum256([]byte(stem))
		ending := "_" + hex.EncodeToString(digest[:8])
		stem = truncateFilenameUTF8(stem, maxStem-len(ending)) + ending
	}
	// LinkedIn's duplicate-upload guard deliberately requires the trailing
	// eight-hex version token. Keep it last instead of widening that guard.
	return stem + suffix + versionSuffix + ext
}

func applicationJobFilenameToken(jobID string) string {
	token := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, jobID)
	const maxTokenBytes = 80
	if token != jobID || len(token) > maxTokenBytes {
		digest := sha256.Sum256([]byte(jobID))
		ending := "_" + hex.EncodeToString(digest[:8])
		if len(token) > maxTokenBytes-len(ending) {
			token = token[:maxTokenBytes-len(ending)]
		}
		token += ending
	}
	return token
}

func truncateFilenameUTF8(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
