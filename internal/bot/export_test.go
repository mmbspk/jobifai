package bot

import "context"

// AcquireRunForTest exposes acquireRun for external test packages.
func (m *Manager) AcquireRunForTest(ctx context.Context, userID, proposedID string) (string, func(), error) {
	return m.acquireRun(ctx, userID, proposedID)
}
