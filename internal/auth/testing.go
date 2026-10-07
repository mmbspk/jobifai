package auth

import "context"

// TestContext returns ctx with userID injected as the authenticated user.
// For use in tests only — do not call in production code.
func TestContext(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// SetBcryptCostForTest overrides the bcrypt work factor for unit tests only.
// Call with bcrypt.MinCost (4) from TestMain to avoid spending minutes
// hashing passwords in tests. Production code must never call this.
func SetBcryptCostForTest(cost int) {
	bcryptCost = cost
}
