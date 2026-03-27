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
	"strconv"
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
	endpoint := firstNonEmptyEnv("S3_ENDPOINT", "MINIO_ADDRESS")
	accessKey := firstNonEmptyEnv("S3_ACCESS_KEY", "MINIO_ACCESS_KEY_ID")
	secretKey := firstNonEmptyEnv("S3_SECRET_KEY", "MINIO_SECRET_ACCESS_KEY")
	bucket := firstNonEmptyEnv("S3_BUCKET", "MINIO_BUCKET_NAME")
	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
		return S3Config{}, errors.New("missing S3 config: require S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY, S3_BUCKET (MINIO_* aliases supported)")
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
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
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

type s3HTTPConfig struct {
	timeout    time.Duration
	maxRetries int
	retryBase  time.Duration
}

func loadS3HTTPConfigFromEnv() s3HTTPConfig {
	timeoutMS := parseEnvInt("S3_HTTP_TIMEOUT_MS", 8000)
	if timeoutMS <= 0 {
		timeoutMS = 8000
	}
	maxRetries := parseEnvInt("S3_MAX_RETRIES", 2)
	if maxRetries < 0 {
		maxRetries = 0
	}
	retryBaseMS := parseEnvInt("S3_RETRY_BASE_MS", 150)
	if retryBaseMS <= 0 {
		retryBaseMS = 150
	}
	return s3HTTPConfig{
		timeout:    time.Duration(timeoutMS) * time.Millisecond,
		maxRetries: maxRetries,
		retryBase:  time.Duration(retryBaseMS) * time.Millisecond,
	}
}

func parseEnvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func resolveS3HTTPClient(httpClient *http.Client, cfg s3HTTPConfig) *http.Client {
	if httpClient != nil {
		return httpClient
	}
	return &http.Client{Timeout: cfg.timeout}
}

func isRetryableS3Status(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func doSignedS3Request(
	ctx context.Context,
	httpClient *http.Client,
	cfg S3Config,
	method, rawURL string,
	body []byte,
	contentType string,
) (*http.Response, error) {
	httpCfg := loadS3HTTPConfigFromEnv()
	client := resolveS3HTTPClient(httpClient, httpCfg)
	attempts := httpCfg.maxRetries + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
		if err != nil {
			return nil, err
		}
		if body != nil {
			req.ContentLength = int64(len(body))
		}
		s3Sign(req, cfg, body, contentType)

		resp, err := client.Do(req)
		if err == nil {
			if !isRetryableS3Status(resp.StatusCode) || attempt == attempts-1 {
				return resp, nil
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("retryable status %d", resp.StatusCode)
		} else {
			lastErr = err
			if attempt == attempts-1 {
				return nil, err
			}
		}

		delay := time.Duration(attempt+1) * httpCfg.retryBase
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastErr
}

// EnsureBucket makes sure the bucket exists (creates if missing).
func EnsureBucket(ctx context.Context, httpClient *http.Client, cfg S3Config) error {
	rawURL := fmt.Sprintf("%s/%s", cfg.baseURL(), cfg.Bucket)
	resp, err := doSignedS3Request(ctx, httpClient, cfg, http.MethodPut, rawURL, nil, "")
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
	if strings.HasPrefix(objectKey, "/") {
		objectKey = strings.TrimLeft(objectKey, "/")
	}

	if err := EnsureBucket(ctx, httpClient, cfg); err != nil {
		return 0, false, err
	}

	putURL := fmt.Sprintf("%s/%s/%s", cfg.baseURL(), cfg.Bucket, objectKey)
	putResp, err := doSignedS3Request(ctx, httpClient, cfg, http.MethodPut, putURL, data, contentType)
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
	getResp, err := doSignedS3Request(ctx, httpClient, cfg, http.MethodGet, getURL, nil, "")
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
	objectKey = strings.TrimLeft(objectKey, "/")
	putURL := fmt.Sprintf("%s/%s/%s", cfg.baseURL(), cfg.Bucket, objectKey)
	resp, err := doSignedS3Request(ctx, httpClient, cfg, http.MethodPut, putURL, data, contentType)
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
	objectKey = strings.TrimLeft(objectKey, "/")
	getURL := fmt.Sprintf("%s/%s/%s", cfg.baseURL(), cfg.Bucket, objectKey)
	resp, err := doSignedS3Request(ctx, httpClient, cfg, http.MethodGet, getURL, nil, "")
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

		resp, err := doSignedS3Request(ctx, httpClient, cfg, http.MethodGet, listURL, nil, "")
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
	canonHeaderParts := make([]string, 0, len(signedNames))
	for _, name := range signedNames {
		// Note: header values must be trimmed and sequential spaces collapsed.
		val := strings.TrimSpace(req.Header.Get(httpHeaderCanonicalName(name)))
		val = strings.Join(strings.Fields(val), " ")
		canonHeaderParts = append(canonHeaderParts, name)
		canonHeaders.WriteString(name)
		canonHeaders.WriteByte(':')
		canonHeaders.WriteString(val)
		canonHeaders.WriteByte('\n')
		_ = canonHeaderParts // keep for readability
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
