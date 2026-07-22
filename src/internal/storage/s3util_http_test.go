package storage

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGetBytesRepeated404ReusesConnection(t *testing.T) {
	t.Setenv("S3_MAX_RETRIES", "0")
	var newConnections atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(strings.Repeat("missing", 128)))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConnections.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	cfg := S3Config{
		Endpoint: strings.TrimPrefix(srv.URL, "http://"), AccessKey: "ak", SecretKey: "sk",
		Bucket: "b", Region: "us-east-1",
	}
	for i := 0; i < 25; i++ {
		got, err := GetBytes(context.Background(), nil, cfg, "missing.json")
		if err != nil || got != nil {
			t.Fatalf("request %d: got=%q err=%v", i, string(got), err)
		}
	}
	if got := newConnections.Load(); got > 2 {
		t.Fatalf("404 response bodies were not reused efficiently: new connections=%d", got)
	}
}

func TestGetBytes_RetryOn503ThenSuccess(t *testing.T) {
	t.Setenv("S3_MAX_RETRIES", "3")
	t.Setenv("S3_RETRY_BASE_MS", "1")
	t.Setenv("S3_HTTP_TIMEOUT_MS", "1000")

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("retry"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := S3Config{
		Endpoint:  strings.TrimPrefix(srv.URL, "http://"),
		AccessKey: "ak",
		SecretKey: "sk",
		Bucket:    "b",
		Region:    "us-east-1",
		Secure:    false,
	}
	got, err := GetBytes(context.Background(), nil, cfg, "obj.json")
	if err != nil {
		t.Fatalf("GetBytes err: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("GetBytes body: want ok, got %q", string(got))
	}
	if calls.Load() != 3 {
		t.Fatalf("request calls: want 3, got %d", calls.Load())
	}
}

func TestGetBytes_NoRetryOn404(t *testing.T) {
	t.Setenv("S3_MAX_RETRIES", "3")
	t.Setenv("S3_RETRY_BASE_MS", "1")
	t.Setenv("S3_HTTP_TIMEOUT_MS", "1000")

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := S3Config{
		Endpoint:  strings.TrimPrefix(srv.URL, "http://"),
		AccessKey: "ak",
		SecretKey: "sk",
		Bucket:    "b",
		Region:    "us-east-1",
		Secure:    false,
	}
	got, err := GetBytes(context.Background(), nil, cfg, "missing.json")
	if err != nil {
		t.Fatalf("GetBytes err: %v", err)
	}
	if got != nil {
		t.Fatalf("GetBytes body: want nil on 404, got %q", string(got))
	}
	if calls.Load() != 1 {
		t.Fatalf("request calls: want 1, got %d", calls.Load())
	}
}
