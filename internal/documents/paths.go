package documents

import (
	"path/filepath"
	"strings"
)

// StylesDirRelative is the repository-relative directory of named resume styles shipped with markets.
const StylesDirRelative = "resume_markets/styles"

const jobApplicationsPrefix = "job_applications/"

// JobApplicationRelRef returns a stable relative ref job_applications/... for DB and auth checks.
// Absolute paths under the repo are normalized; other inputs are slash-normalized as-is.
func JobApplicationRelRef(p string) string {
	if p == "" {
		return ""
	}
	s := filepath.ToSlash(p)
	if idx := strings.Index(s, jobApplicationsPrefix); idx >= 0 {
		return s[idx:]
	}
	return s
}

// JobApplicationFileURLSuffix is the path segment after job_applications/ for GET /api/files/{suffix}.
func JobApplicationFileURLSuffix(p string) string {
	ref := JobApplicationRelRef(p)
	return strings.TrimPrefix(ref, jobApplicationsPrefix)
}
