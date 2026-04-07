package access

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
	"sync"
)

const (
	// EnvAdminAPIKey enables admin auth when set.
	EnvAdminAPIKey = "ANDB_ADMIN_API_KEY"
)

var adminAuthWarnOnce sync.Once

// WrapAdminAuth protects /v1/admin/* with a shared secret when ANDB_ADMIN_API_KEY is set.
//
// Accepted credentials:
// - X-Admin-Key: <key>
// - Authorization: Bearer <key>
//
// If ANDB_ADMIN_API_KEY is not set, the handler is returned unchanged (dev default).
func WrapAdminAuth(next http.Handler) http.Handler {
	key := strings.TrimSpace(os.Getenv(EnvAdminAPIKey))
	if key == "" {
		adminAuthWarnOnce.Do(func() {
			// Intentionally silent by default: dev / tests should work without extra env.
			// Production must enforce network isolation or set ANDB_ADMIN_API_KEY.
		})
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/admin/") {
			got := strings.TrimSpace(r.Header.Get("X-Admin-Key"))
			if got == "" {
				auth := strings.TrimSpace(r.Header.Get("Authorization"))
				if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
					got = strings.TrimSpace(auth[len("bearer "):])
				}
			}
			if !constantTimeEqual(got, key) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func constantTimeEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ab := []byte(a)
	bb := []byte(b)
	if len(ab) != len(bb) {
		// Compare anyway to keep timing similar for same-length keys.
		// Note: subtle.ConstantTimeCompare requires equal lengths; so we do a length check
		// but still avoid early return on content paths by comparing against itself.
		_ = subtle.ConstantTimeCompare(ab, ab)
		return false
	}
	return subtle.ConstantTimeCompare(ab, bb) == 1
}

