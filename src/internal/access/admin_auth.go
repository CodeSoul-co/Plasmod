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
	// Hash to fixed-length digests before comparing so we do not leak key length via timing.
	// Empty key semantics remain: empty is always rejected.
	digest := func(s string) []byte {
		mac := hmac.New(sha256.New, []byte("andb_admin_auth_compare_v1"))
		_, _ = mac.Write([]byte(s))
		return mac.Sum(nil)
	}
	da := digest(a)
	db := digest(b)
	eq := subtle.ConstantTimeCompare(da, db) == 1
	return eq && a != "" && b != ""
}

