package access

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestWrapAdminAuth_DisabledWhenEnvEmpty(t *testing.T) {
	t.Setenv(EnvAdminAPIKey, "")
	h := WrapAdminAuth(okHandler())
	for _, path := range []string{"/v1/admin/topology", "/v1/admin/dataset/delete"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("path %s: expected 200 with no key set, got %d", path, rr.Code)
		}
	}
}

func TestWrapAdminAuth_NonAdminRouteAlwaysAllowed(t *testing.T) {
	t.Setenv(EnvAdminAPIKey, "supersecret")
	h := WrapAdminAuth(okHandler())
	for _, path := range []string{"/v1/ingest/events", "/v1/query", "/healthz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("non-admin path %s: expected 200, got %d", path, rr.Code)
		}
	}
}

func TestWrapAdminAuth_XAdminKeyHeader_Correct(t *testing.T) {
	t.Setenv(EnvAdminAPIKey, "my-secret-key")
	h := WrapAdminAuth(okHandler())
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/delete", nil)
	req.Header.Set("X-Admin-Key", "my-secret-key")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("correct X-Admin-Key: expected 200, got %d", rr.Code)
	}
}

func TestWrapAdminAuth_BearerToken_Correct(t *testing.T) {
	t.Setenv(EnvAdminAPIKey, "my-secret-key")
	h := WrapAdminAuth(okHandler())
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/purge", nil)
	req.Header.Set("Authorization", "Bearer my-secret-key")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("correct Bearer token: expected 200, got %d", rr.Code)
	}
}

func TestWrapAdminAuth_BearerToken_CaseInsensitive(t *testing.T) {
	t.Setenv(EnvAdminAPIKey, "my-secret-key")
	h := WrapAdminAuth(okHandler())
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/topology", nil)
	req.Header.Set("Authorization", "BEARER my-secret-key")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("uppercase BEARER: expected 200, got %d", rr.Code)
	}
}

func TestWrapAdminAuth_WrongKey_Rejected(t *testing.T) {
	t.Setenv(EnvAdminAPIKey, "correct-key")
	h := WrapAdminAuth(okHandler())
	for _, bad := range []string{"wrong-key", "correct-key-extra", "correct-ke"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/delete", nil)
		req.Header.Set("X-Admin-Key", bad)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("bad key %q: expected 401, got %d", bad, rr.Code)
		}
	}
}

func TestWrapAdminAuth_NoCredentials_Rejected(t *testing.T) {
	t.Setenv(EnvAdminAPIKey, "secret")
	h := WrapAdminAuth(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/topology", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no credentials: expected 401, got %d", rr.Code)
	}
}

func TestWrapAdminAuth_WhitespaceKeyTrimmed(t *testing.T) {
	t.Setenv(EnvAdminAPIKey, "  secret  ")
	h := WrapAdminAuth(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/topology", nil)
	req.Header.Set("X-Admin-Key", "secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("trimmed env key: expected 200, got %d", rr.Code)
	}
}

func TestConstantTimeEqual_SameKey(t *testing.T) {
	if !constantTimeEqual("abc123", "abc123") {
		t.Error("identical keys should be equal")
	}
}

func TestConstantTimeEqual_DifferentKeys(t *testing.T) {
	if constantTimeEqual("abc123", "abc124") {
		t.Error("different keys should not be equal")
	}
}

func TestConstantTimeEqual_DifferentLengths(t *testing.T) {
	if constantTimeEqual("short", "much-longer-key") {
		t.Error("different-length keys should not be equal")
	}
}

func TestConstantTimeEqual_EmptyInputs(t *testing.T) {
	cases := [][2]string{{"", ""}, {"", "x"}, {"x", ""}}
	for _, c := range cases {
		if constantTimeEqual(c[0], c[1]) {
			t.Errorf("empty input %q/%q should not be equal", c[0], c[1])
		}
	}
}

func TestWrapAdminAuth_ResponseBody_Unauthorized(t *testing.T) {
	t.Setenv(EnvAdminAPIKey, "secret")
	h := WrapAdminAuth(okHandler())
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/delete", nil)
	req.Header.Set("X-Admin-Key", "wrong")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), "unauthorized") {
		t.Errorf("expected 'unauthorized' in body, got: %q", rr.Body.String())
	}
}
