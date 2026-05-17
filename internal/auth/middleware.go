package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type contextKey string

const userIDKey contextKey = "user_id"
const userEmailKey contextKey = "user_email"

// RequireAuth is middleware that validates the Bearer JWT and injects the
// user_id and email into the request context. Returns 401 on missing/invalid token.
func RequireAuth(tm *TokenManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				writeUnauthorized(w, "missing or malformed authorization header")
				return
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")
			claims, err := tm.Verify(tokenStr)
			if err != nil {
				writeUnauthorized(w, "invalid or expired token")
				return
			}
			ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
			ctx = context.WithValue(ctx, userEmailKey, claims.Email)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromCtx extracts the authenticated user's ID from the context.
// Returns empty string if not set (should not happen behind RequireAuth).
func UserIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(userIDKey).(string)
	return v
}

// UserEmailFromCtx extracts the authenticated user's email from the context.
func UserEmailFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(userEmailKey).(string)
	return v
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	b, _ := json.Marshal(map[string]string{"message": msg})
	_, _ = w.Write(b)
}
