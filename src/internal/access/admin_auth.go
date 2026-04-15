package access

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

const (
	// EnvAdminAPIKey enables admin auth when set.
	EnvAdminAPIKey = "PLASMOD_ADMIN_API_KEY"
	// EnvAdminAPIKeyCompat keeps backward compatibility with older docs/env.
	EnvAdminAPIKeyCompat = "ANDB_ADMIN_API_KEY"
)

var adminAuthWarnOnce sync.Once

// WrapAdminAuth protects /v1/admin/* with a shared secret when PLASMOD_ADMIN_API_KEY is set.
//
// Accepted credentials:
// - X-Admin-Key: <key>
// - Authorization: Bearer <key>
//
// If PLASMOD_ADMIN_API_KEY is not set, the handler is returned unchanged (dev default).
func WrapAdminAuth(next http.Handler) http.Handler {
	key := strings.TrimSpace(os.Getenv(EnvAdminAPIKey))
	if key == "" {
		key = strings.TrimSpace(os.Getenv(EnvAdminAPIKeyCompat))
	}
	if key == "" {
		adminAuthWarnOnce.Do(func() {
			log.Printf("warning: admin routes are unprotected because %s is not set", EnvAdminAPIKey)
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
	// Keep prior semantics: empty credentials are always rejected.
	if a == "" || b == "" {
		return false
	}
	// Compare fixed-length HMAC digests to avoid leaking key length via timing.
	da := authDigest(a)
	db := authDigest(b)
	return subtle.ConstantTimeCompare(da, db) == 1
}

func authDigest(v string) []byte {
	// Key material is process-local and constant per run; digest length is fixed (sha256.Size).
	m := hmac.New(sha256.New, []byte("andb-admin-auth-v1"))
	_, _ = m.Write([]byte(v))
	return m.Sum(nil)
}

