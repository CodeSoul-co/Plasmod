// Package integration_test provides HTTP-level integration tests for the ANDB server.
// Run with: go test ./integration_tests/... -v
// Override server URL: ANDB_BASE_URL=http://... go test ./integration_tests/...
// Enable S3 tests:    ANDB_RUN_S3_TESTS=true ... go test ./integration_tests/...
package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

var serverURL string

func TestMain(m *testing.M) {
	serverURL = os.Getenv("ANDB_BASE_URL")
	if serverURL == "" {
		serverURL = "http://127.0.0.1:8080"
	}
	if err := awaitServer(20 * time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "integration_test: server not available: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// awaitServer polls /healthz until the server is ready or the timeout is reached.
func awaitServer(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		resp, err := http.Get(serverURL + "/healthz") //nolint:noctx
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		last = err
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("server %s not ready after %s: %v", serverURL, timeout, last)
}

// doJSON sends an HTTP request with an optional JSON body and decodes the JSON response body.
func doJSON(t *testing.T, method, path string, body any) (status int, result map[string]any) {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("doJSON marshal: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, serverURL+path, reqBody)
	if err != nil {
		t.Fatalf("doJSON new request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("doJSON do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &result)
	}
	return resp.StatusCode, result
}

// doJSONSlice decodes the response as a JSON array.
func doJSONSlice(t *testing.T, method, path string) (status int, result []any) {
	t.Helper()
	req, err := http.NewRequest(method, serverURL+path, nil)
	if err != nil {
		t.Fatalf("doJSONSlice: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("doJSONSlice do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &result)
	}
	return resp.StatusCode, result
}

// doRaw sends an HTTP request with a raw body (for negative/protocol tests).
func doRaw(t *testing.T, method, path, contentType string, body []byte) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, serverURL+path, bodyReader)
	if err != nil {
		t.Fatalf("doRaw new request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("doRaw do %s %s: %v", method, path, err)
	}
	return resp
}

// assertKeys checks that all given keys are present in the result map.
func assertKeys(t *testing.T, result map[string]any, keys ...string) {
	t.Helper()
	if result == nil {
		t.Fatal("assertKeys: result is nil")
	}
	for _, k := range keys {
		if _, ok := result[k]; !ok {
			t.Errorf("assertKeys: missing key %q (got keys: %v)", k, mapKeys(result))
		}
	}
}

func mapKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// nowISO returns current UTC time in RFC3339 format.
func nowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

// uniqID returns a string suffix based on current nanoseconds for unique object IDs.
func uniqID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// s3TestsEnabled returns true if the ANDB_RUN_S3_TESTS env var is set.
func s3TestsEnabled() bool {
	v := os.Getenv("ANDB_RUN_S3_TESTS")
	return v == "1" || v == "true" || v == "yes"
}
