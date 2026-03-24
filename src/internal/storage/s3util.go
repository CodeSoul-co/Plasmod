package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// S3Config is a minimal MinIO/S3 config used for raw SigV4 (stdlib only).
type S3Config struct {
	Endpoint  string // host:port, no scheme
	AccessKey string
	SecretKey string
	Bucket    string
	Secure    bool
	Region    string
	Prefix    string
}

func LoadFromEnv() (S3Config, error) {
	// Canonical keys are S3_*; MINIO_* aliases are supported for compatibility.
	endpoint := firstNonEmptyEnv("S3_ENDPOINT", "MINIO_ADDRESS")
	accessKey := firstNonEmptyEnv("S3_ACCESS_KEY", "MINIO_ACCESS_KEY_ID")
	secretKey := firstNonEmptyEnv("S3_SECRET_KEY", "MINIO_SECRET_ACCESS_KEY")
	bucket := firstNonEmptyEnv("S3_BUCKET", "MINIO_BUCKET_NAME")
	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
		return S3Config{}, errors.New("missing S3 config: require S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY/S3_BUCKET (MINIO_* aliases supported)")
	}

	secure := firstNonEmptyEnv("S3_SECURE", "MINIO_USE_SSL")
	isSecure := secure == "true" || secure == "1"

	region := firstNonEmptyEnv("S3_REGION", "MINIO_REGION")
	if region == "" {
		region = "us-east-1"
	}

	prefix := firstNonEmptyEnv("S3_PREFIX", "MINIO_ROOT_PATH")
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		prefix = "andb/integration_tests"
	}

	return S3Config{
		Endpoint:  endpoint,
		AccessKey: accessKey,
		SecretKey: secretKey,
		Bucket:    bucket,
		Secure:    isSecure,
		Region:    region,
		Prefix:    prefix,
	}, nil
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func (c S3Config) baseURL() string {
	scheme := "http"
	if c.Secure {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, c.Endpoint)
}

// EnsureBucket makes sure the bucket exists (creates if missing).
func EnsureBucket(ctx context.Context, httpClient *http.Client, cfg S3Config) error {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	rawURL := fmt.Sprintf("%s/%s", cfg.baseURL(), cfg.Bucket)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, rawURL, nil)
	if err != nil {
		return fmt.Errorf("ensure bucket new request: %w", err)
	}
	s3Sign(req, cfg, nil, "")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ensure bucket do: %w", err)
	}
	defer resp.Body.Close()

	// 200 OK = created; 409 Conflict = bucket already exists — both are fine.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ensure bucket status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func PutJSONAndVerify(ctx context.Context, httpClient *http.Client, cfg S3Config, objectKey string, payload any) (int, bool, error) {
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return 0, false, fmt.Errorf("marshal payload: %w", err)
	}
	return PutBytesAndVerify(ctx, httpClient, cfg, objectKey, b, "application/json")
}

func PutBytesAndVerify(ctx context.Context, httpClient *http.Client, cfg S3Config, objectKey string, data []byte, contentType string) (int, bool, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if strings.HasPrefix(objectKey, "/") {
		objectKey = strings.TrimLeft(objectKey, "/")
	}

	if err := EnsureBucket(ctx, httpClient, cfg); err != nil {
		return 0, false, err
	}

	putURL := fmt.Sprintf("%s/%s/%s", cfg.baseURL(), cfg.Bucket, objectKey)
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, bytes.NewReader(data))
	if err != nil {
		return 0, false, fmt.Errorf("put new request: %w", err)
	}
	putReq.ContentLength = int64(len(data))
	s3Sign(putReq, cfg, data, contentType)
	putResp, err := httpClient.Do(putReq)
	if err != nil {
		return 0, false, fmt.Errorf("put do: %w", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(putResp.Body)
		return 0, false, fmt.Errorf("put status %d: %s", putResp.StatusCode, string(body))
	}

	// Round-trip verification: GET the object back and compare bytes.
	getURL := fmt.Sprintf("%s/%s/%s", cfg.baseURL(), cfg.Bucket, objectKey)
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return 0, false, fmt.Errorf("get new request: %w", err)
	}
	s3Sign(getReq, cfg, nil, "")
	getResp, err := httpClient.Do(getReq)
	if err != nil {
		return 0, false, fmt.Errorf("get do: %w", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getResp.Body)
		return 0, false, fmt.Errorf("get status %d: %s", getResp.StatusCode, string(body))
	}
	got, err := io.ReadAll(getResp.Body)
	if err != nil {
		return 0, false, fmt.Errorf("get read: %w", err)
	}
	return len(data), bytes.Equal(got, data), nil
}

// PutBytes writes data to S3 at objectKey without a round-trip read verification.
// Use this for high-frequency cold-store writes where latency matters more than
// immediate consistency checks (ArchiveMemory, ArchiveAgent, etc.).
//
// NOTE: bucket creation is the caller's responsibility. S3ColdStore calls
// EnsureBucket once via sync.Once before its first write.
func PutBytes(ctx context.Context, httpClient *http.Client, cfg S3Config, objectKey string, data []byte, contentType string) error {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	objectKey = strings.TrimLeft(objectKey, "/")
	putURL := fmt.Sprintf("%s/%s/%s", cfg.baseURL(), cfg.Bucket, objectKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("s3 put new request: %w", err)
	}
	req.ContentLength = int64(len(data))
	s3Sign(req, cfg, data, contentType)
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("s3 put do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("s3 put status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// GetBytes fetches an object from S3 at objectKey.
// Returns (nil, nil) when the object does not exist (404).
func GetBytes(ctx context.Context, httpClient *http.Client, cfg S3Config, objectKey string) ([]byte, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	objectKey = strings.TrimLeft(objectKey, "/")
	getURL := fmt.Sprintf("%s/%s/%s", cfg.baseURL(), cfg.Bucket, objectKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return nil, fmt.Errorf("s3 get new request: %w", err)
	}
	s3Sign(req, cfg, nil, "")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3 get do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("s3 get status %d: %s", resp.StatusCode, string(body))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("s3 get read: %w", err)
	}
	return data, nil
}

// ListObjects returns the object keys under the given prefix using S3 ListObjectsV2.
// The prefix must not have a leading slash.  Returns nil on any error (caller
// should treat a nil return as "no keys found").
func ListObjects(ctx context.Context, httpClient *http.Client, cfg S3Config, prefix string) ([]string, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	prefix = strings.TrimLeft(prefix, "/")

	// S3 ListObjectsV2 with continuation token support.
	var allKeys []string
	continuationToken := ""

	for {
		listURL := fmt.Sprintf("%s/%s?list-type=2&prefix=%s",
			cfg.baseURL(), cfg.Bucket, prefix)
		if continuationToken != "" {
			listURL += "&continuation-token=" + continuationToken
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
		if err != nil {
			return nil, fmt.Errorf("list objects new request: %w", err)
		}
		s3Sign(req, cfg, nil, "")

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list objects do: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("list objects read: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("list objects status %d: %s", resp.StatusCode, string(body))
		}

		// Parse the XML response manually (stdlib xml is heavy; simple text scan is enough).
		bodyStr := string(body)

		// Extract <Key>...</Key> entries.
		for {
			start := strings.Index(bodyStr, "<Key>")
			if start == -1 {
				break
			}
			start += len("<Key>")
			end := strings.Index(bodyStr[start:], "</Key>")
			if end == -1 {
				break
			}
			allKeys = append(allKeys, bodyStr[start:start+end])
			bodyStr = bodyStr[start+end+len("</Key>"):]
		}

		// Check for NextContinuationToken.
		ctStart := strings.Index(string(body), "<NextContinuationToken>")
		if ctStart == -1 {
			break
		}
		ctStart += len("<NextContinuationToken>")
		ctEnd := strings.Index(string(body)[ctStart:], "</NextContinuationToken>")
		if ctEnd == -1 {
			break
		}
		continuationToken = string(body)[ctStart : ctStart+ctEnd]
	}

	return allKeys, nil
}

// ─── AWS Signature V4 (stdlib only) ─────────────────────────────────────────

func s3Sign(req *http.Request, cfg S3Config, body []byte, contentType string) {
	now := time.Now().UTC()
	dateOnly := now.Format("20060102")
	dateTime := now.Format("20060102T150405Z")

	payloadHash := sha256Hex(body)

	// Mandatory headers.
	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("x-amz-date", dateTime)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	// Signed headers.
	signedNames := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if contentType != "" {
		signedNames = append(signedNames, "content-type")
	}
	sort.Strings(signedNames)

	canonHeaders := bytes.Buffer{}
	for _, name := range signedNames {
		// Note: header values must be trimmed and sequential spaces collapsed.
		val := strings.TrimSpace(req.Header.Get(httpHeaderCanonicalName(name)))
		val = strings.Join(strings.Fields(val), " ")
		canonHeaders.WriteString(name)
		canonHeaders.WriteByte(':')
		canonHeaders.WriteString(val)
		canonHeaders.WriteByte('\n')
	}

	canonURI := req.URL.EscapedPath()
	if canonURI == "" {
		canonURI = "/"
	}

	canonQuery := req.URL.RawQuery

	// Canonical request.
	canonRequest := strings.Join([]string{
		req.Method,
		canonURI,
		canonQuery,
		canonHeaders.String(),
		strings.Join(signedNames, ";"),
		payloadHash,
	}, "\n")

	credScope := fmt.Sprintf("%s/%s/s3/aws4_request", dateOnly, cfg.Region)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		dateTime,
		credScope,
		sha256Hex([]byte(canonRequest)),
	}, "\n")

	signingKey := s3DeriveKey(cfg.SecretKey, dateOnly, cfg.Region)
	signature := hex.EncodeToString(hmacSHA256Bytes(signingKey, []byte(stringToSign)))

	auth := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s,SignedHeaders=%s,Signature=%s",
		cfg.AccessKey,
		credScope,
		strings.Join(signedNames, ";"),
		signature,
	)
	req.Header.Set("Authorization", auth)
}

func httpHeaderCanonicalName(lower string) string {
	switch lower {
	case "host":
		return "Host"
	case "x-amz-date":
		return "x-amz-date"
	case "x-amz-content-sha256":
		return "x-amz-content-sha256"
	case "content-type":
		return "Content-Type"
	default:
		return http.CanonicalHeaderKey(lower)
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func s3DeriveKey(secretKey, dateOnly, region string) []byte {
	kDate := hmacSHA256Bytes([]byte("AWS4"+secretKey), []byte(dateOnly))
	kRegion := hmacSHA256Bytes(kDate, []byte(region))
	kService := hmacSHA256Bytes(kRegion, []byte("s3"))
	kSigning := hmacSHA256Bytes(kService, []byte("aws4_request"))
	return kSigning
}

func hmacSHA256Bytes(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(data)
	return m.Sum(nil)
}
