package integration_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestS3Dataflow runs only when ANDB_RUN_S3_TESTS=true.
// It ingests an event, queries the server, captures the result as JSON,
// and writes/reads it to/from a MinIO-compatible S3 bucket using raw HTTP
// (stdlib only, no external Go module dependency required).
//
// Required env vars (when enabled):
//
//	S3_ENDPOINT    e.g. 127.0.0.1:9000
//	S3_ACCESS_KEY  e.g. minioadmin
//	S3_SECRET_KEY  e.g. minioadmin
//	S3_BUCKET      e.g. andb-integration
//	S3_SECURE      false (default)
//	S3_PREFIX      andb/integration_tests (default)
//	S3_REGION      us-east-1 (default, MinIO ignores this)
func TestS3Dataflow(t *testing.T) {
	if !s3TestsEnabled() {
		t.Skip("S3 tests disabled (set ANDB_RUN_S3_TESTS=true to enable)")
	}

	cfg := loadS3Config(t)

	// 1. Ingest an event.
	ev := sampleEvent(uniqID())
	_, ack := doJSON(t, http.MethodPost, "/v1/ingest/events", ev)
	assertKeys(t, ack, "status", "lsn", "event_id")

	// 2. Run a query.
	_, qResp := doJSON(t, http.MethodPost, "/v1/query", sampleQuery())
	assertKeys(t, qResp, "objects", "provenance", "proof_trace")

	// 3. Serialize the capture payload.
	capture := map[string]any{
		"captured_at": time.Now().UTC().Format(time.RFC3339),
		"base_url":    serverURL,
		"ack":         ack,
		"query":       sampleQuery(),
		"response":    qResp,
	}
	captureJSON, err := json.MarshalIndent(capture, "", "  ")
	if err != nil {
		t.Fatalf("marshal capture: %v", err)
	}

	// 4. Ensure bucket exists.
	t.Run("ensure_bucket", func(t *testing.T) {
		s3EnsureBucket(t, cfg)
	})

	// 5. Upload the capture to S3.
	objectKey := fmt.Sprintf("%s/capture_%s.json", cfg.prefix, time.Now().UTC().Format("20060102T150405Z"))

	t.Run("upload_capture_to_s3", func(t *testing.T) {
		s3PutObject(t, cfg, objectKey, captureJSON)
	})

	// 6. Download and verify round-trip.
	t.Run("download_and_verify_roundtrip", func(t *testing.T) {
		got := s3GetObject(t, cfg, objectKey)
		if !bytes.Equal(got, captureJSON) {
			t.Errorf("S3 roundtrip mismatch:\nuploaded: %s\ndownloaded: %s", captureJSON, got)
		}
		t.Logf("S3 roundtrip OK: %d bytes at key %s", len(got), objectKey)
	})
}

// ─── s3Config ────────────────────────────────────────────────────────────────

type s3Config struct {
	endpoint string
	key      string
	secret   string
	bucket   string
	region   string
	secure   bool
	prefix   string
}

func loadS3Config(t *testing.T) s3Config {
	t.Helper()
	endpoint := os.Getenv("S3_ENDPOINT")
	key := os.Getenv("S3_ACCESS_KEY")
	secret := os.Getenv("S3_SECRET_KEY")
	bucket := os.Getenv("S3_BUCKET")
	if endpoint == "" || key == "" || secret == "" || bucket == "" {
		t.Fatal("S3 config incomplete: set S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY, S3_BUCKET")
	}
	prefix := strings.TrimRight(os.Getenv("S3_PREFIX"), "/")
	if prefix == "" {
		prefix = "andb/integration_tests"
	}
	region := os.Getenv("S3_REGION")
	if region == "" {
		region = "us-east-1"
	}
	secure := os.Getenv("S3_SECURE") == "true" || os.Getenv("S3_SECURE") == "1"
	return s3Config{
		endpoint: endpoint,
		key:      key,
		secret:   secret,
		bucket:   bucket,
		region:   region,
		secure:   secure,
		prefix:   prefix,
	}
}

func (c s3Config) baseURL() string {
	scheme := "http"
	if c.secure {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, c.endpoint)
}

// ─── S3 HTTP helpers ─────────────────────────────────────────────────────────

func s3EnsureBucket(t *testing.T, cfg s3Config) {
	t.Helper()
	rawURL := fmt.Sprintf("%s/%s", cfg.baseURL(), cfg.bucket)
	req, err := http.NewRequest(http.MethodPut, rawURL, nil)
	if err != nil {
		t.Fatalf("s3EnsureBucket: %v", err)
	}
	s3Sign(req, cfg, nil, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("s3EnsureBucket do: %v", err)
	}
	defer resp.Body.Close()
	// 200 OK = created; 409 Conflict = bucket already exists — both are fine.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("s3EnsureBucket: status %d: %s", resp.StatusCode, body)
	}
}

func s3PutObject(t *testing.T, cfg s3Config, key string, data []byte) {
	t.Helper()
	rawURL := fmt.Sprintf("%s/%s/%s", cfg.baseURL(), cfg.bucket, key)
	req, err := http.NewRequest(http.MethodPut, rawURL, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("s3PutObject: %v", err)
	}
	req.ContentLength = int64(len(data))
	s3Sign(req, cfg, data, "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("s3PutObject do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("s3PutObject: status %d: %s", resp.StatusCode, body)
	}
}

func s3GetObject(t *testing.T, cfg s3Config, key string) []byte {
	t.Helper()
	rawURL := fmt.Sprintf("%s/%s/%s", cfg.baseURL(), cfg.bucket, key)
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("s3GetObject: %v", err)
	}
	s3Sign(req, cfg, nil, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("s3GetObject do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("s3GetObject: status %d: %s", resp.StatusCode, body)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("s3GetObject read: %v", err)
	}
	return data
}

// ─── AWS Signature V4 (stdlib only) ─────────────────────────────────────────

// s3Sign attaches AWS Signature Version 4 headers to req.
// body is the raw request body (may be nil). contentType is optional.
func s3Sign(req *http.Request, cfg s3Config, body []byte, contentType string) {
	now := time.Now().UTC()
	dateOnly := now.Format("20060102")
	dateTime := now.Format("20060102T150405Z")

	// Compute payload hash.
	payloadHash := sha256Hex(body)

	// Set mandatory headers before building the canonical request.
	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("x-amz-date", dateTime)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	// Collect headers that will be signed.
	signedNames := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if contentType != "" {
		signedNames = append(signedNames, "content-type")
	}
	sort.Strings(signedNames)

	var canonHeaders strings.Builder
	for _, name := range signedNames {
		canonHeaders.WriteString(name)
		canonHeaders.WriteByte(':')
		canonHeaders.WriteString(strings.TrimSpace(req.Header.Get(name)))
		canonHeaders.WriteByte('\n')
	}
	signedHeadersStr := strings.Join(signedNames, ";")

	// Canonical URI — path only, no query string in our calls.
	canonURI := req.URL.EscapedPath()
	if canonURI == "" {
		canonURI = "/"
	}

	canonRequest := strings.Join([]string{
		req.Method,
		canonURI,
		req.URL.RawQuery,
		canonHeaders.String(),
		signedHeadersStr,
		payloadHash,
	}, "\n")

	// Credential scope.
	scope := strings.Join([]string{dateOnly, cfg.region, "s3", "aws4_request"}, "/")

	// String to sign.
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		dateTime,
		scope,
		sha256Hex([]byte(canonRequest)),
	}, "\n")

	// Derive signing key and compute signature.
	sigKey := s3DeriveKey(cfg.secret, dateOnly, cfg.region)
	sig := hex.EncodeToString(hmacSHA256Bytes(sigKey, []byte(stringToSign)))

	// Attach Authorization header.
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s,SignedHeaders=%s,Signature=%s",
		cfg.key, scope, signedHeadersStr, sig,
	))
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256Bytes(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func s3DeriveKey(secret, date, region string) []byte {
	kDate := hmacSHA256Bytes([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256Bytes(kDate, []byte(region))
	kService := hmacSHA256Bytes(kRegion, []byte("s3"))
	kSigning := hmacSHA256Bytes(kService, []byte("aws4_request"))
	return kSigning
}
