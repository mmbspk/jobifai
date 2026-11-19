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

// ExportedFormField is a test-only alias for the unexported formField type, so
// external test packages can build snapshots without depending on internal types.
type ExportedFormField = formField

// ExportedFormFieldOption is a test-only alias for formFieldOption.
type ExportedFormFieldOption = formFieldOption

// ExportCaptureStepSnapshot exposes captureStepSnapshot for external test packages.
func ExportCaptureStepSnapshot(fields []formField, navActions []SnapNav, platform string, stepIndex int, stepTitle string) StepSnapshot {
	return captureStepSnapshot(fields, navActions, platform, stepIndex, stepTitle)
}
