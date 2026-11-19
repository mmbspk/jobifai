package bot

import (
	"context"
	"database/sql"
)

// AcquireRunForTest exposes acquireRun for external test packages.
func (m *Manager) AcquireRunForTest(ctx context.Context, userID, proposedID string) (string, func(), error) {
	return m.acquireRun(ctx, userID, proposedID)
}

// ExportNewAttemptContext exposes newAttemptContext for testing.
func ExportNewAttemptContext(userID, platform, jobID, url, role, company, entryPoint, runID string) *AttemptContext {
	return newAttemptContext(userID, platform, jobID, url, role, company, entryPoint, runID)
}

// ExportBeginAttempt exposes beginAttempt for testing.
func ExportBeginAttempt(db *sql.DB, ac *AttemptContext) {
	beginAttempt(db, ac)
}
