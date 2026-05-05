package auth

import "context"

// TestContext returns ctx with userID injected as the authenticated user.
// For use in tests only — do not call in production code.
func TestContext(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}
