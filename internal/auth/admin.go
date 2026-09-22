package auth

import (
	"encoding/json"
	"net/http"
)

// RequireAdmin ensures the authenticated user has is_admin set in the database.
// Must run after RequireAuth.
func RequireAdmin(users *UserStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := UserIDFromCtx(r.Context())
			if userID == "" {
				writeForbidden(w, "admin access required")
				return
			}
			u, err := users.ByID(userID)
			if err != nil || !u.IsAdmin {
				writeForbidden(w, "admin access required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeForbidden(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	b, _ := json.Marshal(map[string]string{"message": msg})
	_, _ = w.Write(b)
}
