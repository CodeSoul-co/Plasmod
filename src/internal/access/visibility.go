package access

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	AppModeEnv  = "APP_MODE"
	AppModeTest = "test"
	AppModeProd = "prod"
)

// CurrentAppMode returns runtime mode from APP_MODE.
// Any unknown/empty value falls back to prod.
func CurrentAppMode() string {
	m := strings.ToLower(strings.TrimSpace(os.Getenv(AppModeEnv)))
	switch m {
	case AppModeTest:
		return AppModeTest
	default:
		return AppModeProd
	}
}

func isTestMode() bool { return CurrentAppMode() == AppModeTest }

type captureResponseWriter struct {
	header      http.Header
	statusCode  int
	wroteHeader bool
	buf         bytes.Buffer
}

func newCaptureResponseWriter() *captureResponseWriter {
	return &captureResponseWriter{header: make(http.Header), statusCode: http.StatusOK}
}

func (w *captureResponseWriter) Header() http.Header { return w.header }

func (w *captureResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.statusCode = statusCode
}

func (w *captureResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.buf.Write(b)
}

// WrapVisibility enforces environment-bound visibility policy:
// - test: keeps full response and appends debug metadata (for JSON object responses)
// - prod: strips debug/sensitive fields from JSON responses before returning
func WrapVisibility(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		traceID := time.Now().UTC().Format("20060102T150405.000000000Z07:00")
		mode := CurrentAppMode()

		var reqBody []byte
		if isTestMode() && r.Body != nil {
			raw, _ := readAndRestoreBody(r)
			reqBody = raw
		}

		crw := newCaptureResponseWriter()
		next.ServeHTTP(crw, r)

		outBody := crw.buf.Bytes()
		contentType := crw.header.Get("Content-Type")
		if contentType == "" {
			contentType = http.DetectContentType(outBody)
		}
		isJSON := strings.Contains(strings.ToLower(contentType), "application/json")

		if isJSON && len(outBody) > 0 {
			var v any
			if err := json.Unmarshal(outBody, &v); err == nil {
				if mode == AppModeProd {
					v = sanitizeForProd(v)
				} else if mode == AppModeTest {
					if obj, ok := v.(map[string]any); ok {
						obj["_debug"] = map[string]any{
							"trace_id":       traceID,
							"request_path":   r.URL.Path,
							"request_method": r.Method,
							"request_body":   string(reqBody),
							"duration_ms":    time.Since(start).Milliseconds(),
						}
						v = obj
					}
					log.Printf("[debug-transparent] trace=%s method=%s path=%s status=%d duration_ms=%d request_body=%q raw_response=%s",
						traceID, r.Method, r.URL.Path, crw.statusCode, time.Since(start).Milliseconds(), string(reqBody), string(outBody))
				}
				if data, err := json.Marshal(v); err == nil {
					outBody = data
				}
			}
		}

		// Copy headers to original writer.
		for k, vals := range crw.header {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.Header().Set("X-ANDB-Mode", mode)
		if mode == AppModeTest {
			w.Header().Set("X-ANDB-Trace-ID", traceID)
		}
		w.WriteHeader(crw.statusCode)
		_, _ = w.Write(outBody)
	})
}

func readAndRestoreBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return raw, nil
}

func sanitizeForProd(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			lk := strings.ToLower(strings.TrimSpace(k))
			if isBlockedProdKey(lk) {
				continue
			}
			out[k] = sanitizeForProd(val)
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, it := range x {
			out = append(out, sanitizeForProd(it))
		}
		return out
	default:
		return v
	}
}

func isBlockedProdKey(key string) bool {
	switch key {
	case "_debug", "debug", "logs", "log", "trace", "chain_traces", "raw_request", "raw_response", "request_body", "intermediate", "internal":
		return true
	default:
		return strings.HasPrefix(key, "debug_") || strings.HasPrefix(key, "raw_")
	}
}
