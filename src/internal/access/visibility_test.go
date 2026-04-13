package access

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"plasmod/src/internal/storage"
)

func TestWrapVisibility_Prod_StripsDebugFields(t *testing.T) {
	t.Setenv(AppModeEnv, AppModeProd)
	h := WrapVisibility(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"status":       "ok",
			"debug":        map[string]any{"x": 1},
			"chain_traces": []any{"a", "b"},
			"nested": map[string]any{
				"raw_response": "secret",
				"ok":           true,
			},
		})
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := body["debug"]; ok {
		t.Fatal("expected debug field to be stripped in prod")
	}
	if _, ok := body["chain_traces"]; ok {
		t.Fatal("expected chain_traces to be stripped in prod")
	}
	nested, _ := body["nested"].(map[string]any)
	if _, ok := nested["raw_response"]; ok {
		t.Fatal("expected raw_response to be stripped in prod nested object")
	}
	if rr.Header().Get("X-ANDB-Mode") != AppModeProd {
		t.Fatalf("X-ANDB-Mode = %q", rr.Header().Get("X-ANDB-Mode"))
	}
}

func TestWrapVisibility_TestMode_AppendsDebugBlock(t *testing.T) {
	t.Setenv(AppModeEnv, AppModeTest)
	h := WrapVisibility(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "ok"})
	}))

	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := body["_debug"]; !ok {
		t.Fatal("expected _debug field in test mode")
	}
	if rr.Header().Get("X-ANDB-Trace-ID") == "" {
		t.Fatal("expected X-ANDB-Trace-ID in test mode")
	}
}

func TestGateway_DebugEndpoint_ModeGated(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()

	t.Run("prod_no_debug_route", func(t *testing.T) {
		t.Setenv(AppModeEnv, AppModeProd)
		mux := http.NewServeMux()
		NewGateway(nil, nil, store, nil, nil).RegisterRoutes(mux)

		req := httptest.NewRequest(http.MethodPost, "/v1/debug/echo", strings.NewReader(`{"x":1}`))
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rr.Code)
		}
	})

	t.Run("test_has_debug_route", func(t *testing.T) {
		t.Setenv(AppModeEnv, AppModeTest)
		mux := http.NewServeMux()
		NewGateway(nil, nil, store, nil, nil).RegisterRoutes(mux)

		req := httptest.NewRequest(http.MethodPost, "/v1/debug/echo", strings.NewReader(`{"x":1}`))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
	})
}
