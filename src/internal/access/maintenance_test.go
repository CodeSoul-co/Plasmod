package access

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"plasmod/src/internal/worker"
	"plasmod/src/internal/worker/cognitive"
	"plasmod/src/internal/worker/cognitive/memorybank"
)

func postLifecycle(t *testing.T, mux http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", path, bytes.NewReader(data)))
	return w
}
func TestGatewayLifecycleActualChainEndpoints(t *testing.T) {
	d := buildTestGatewayWithDeps()
	d.runtime.MemoryBankMode = true
	d.runtime.Manager().RegisterAlgorithmDispatch(cognitive.CreateAlgorithmDispatchWorker("bank", memorybank.New("bank", memorybank.DefaultConfig()), d.store.Objects(), d.store.AlgorithmStates(), d.store.Audits(), d.store))
	mux := http.NewServeMux()
	d.gw.RegisterRoutes(mux)
	for _, id := range []string{"evt_first", "evt_second"} {
		w := postLifecycle(t, mux, "/v1/ingest/events", map[string]any{"identity": map[string]string{"event_id": id, "tenant_id": "t", "workspace_id": "w"}, "actor": map[string]string{"agent_id": "a", "session_id": "s"}, "event": map[string]any{"event_type": "user_message", "importance": 1, "confidence": 1}, "access": map[string]string{"visibility": "workspace"}, "payload": map[string]string{"text": "drink coffee " + id}})
		if w.Code != 200 {
			t.Fatalf("ingest: %s", w.Body.String())
		}
	}
	req := worker.MaintenanceRequest{OperationID: "sum", TenantID: "t", WorkspaceID: "w", AgentID: "a"}
	w := postLifecycle(t, mux, "/v1/internal/memory/summarize", req)
	var out worker.MaintenanceReceipt
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.UpdatedCount != 2 || len(out.ProducedIDs) != 1 || out.ChainName != "memory_pipeline_chain" {
		t.Fatalf("broken maintenance: %s", w.Body.String())
	}
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest("GET", "/v1/internal/memory/operations/sum?tenant_id=t&workspace_id=w", nil))
	if get.Code != 200 {
		t.Fatal(get.Body.String())
	}
	w = postLifecycle(t, mux, "/v1/internal/memory/mutate", worker.MemoryMutationRequest{OperationID: "forget", Operation: "forget", TenantID: "t", WorkspaceID: "w", ReferenceIDs: []string{"evt_first"}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	get = httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest("GET", "/v1/traces/"+out.ProducedIDs[0], nil))
	if get.Code != 410 {
		t.Fatalf("forgotten version exposed: %d %s", get.Code, get.Body.String())
	}
	w = postLifecycle(t, mux, "/v1/internal/memory/decay", map[string]string{"agent_id": "a"})
	if w.Code != 400 {
		t.Fatal("unscoped mutation accepted")
	}
}
func TestGatewayDocumentDurableSegmentsIsolationAndIdempotency(t *testing.T) {
	d := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	d.gw.RegisterRoutes(mux)
	req := documentRequest{TenantID: "t", WorkspaceID: "w", AgentID: "a", SessionID: "s", SourceID: "meeting", UploadBatchID: "upload", SegmentTotal: 2, ChunkSize: 12, Overlap: 2, Text: "第一段内容。"}
	w := postLifecycle(t, mux, "/v1/ingest/document", req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	// Lose the in-memory assembler exactly as a process restart would.
	d.gw.docAssembler = newDocumentSegmentAssembler()
	req.SegmentIndex = 1
	req.Text = "第二段内容。会议决定下周再确认。"
	w = postLifecycle(t, mux, "/v1/ingest/document", req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var result struct {
		MemoryIDs []string `json:"memory_ids"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.MemoryIDs) < 2 {
		t.Fatal("chunks missing")
	}
	count := len(d.store.Objects().ListMemories("a", "s"))
	w = postLifecycle(t, mux, "/v1/ingest/document", req)
	if w.Code != 200 || len(d.store.Objects().ListMemories("a", "s")) != count {
		t.Fatalf("duplicate document: %s", w.Body.String())
	}
	for _, id := range result.MemoryIDs {
		m, _ := d.store.Objects().GetMemory(id)
		if m.TenantID != "t" || len(m.SourceSpans) != 1 || m.SourceSpans[0].End <= m.SourceSpans[0].Start {
			t.Fatalf("lost identity/offset: %+v", m)
		}
	}
	req.Text = "changed"
	w = postLifecycle(t, mux, "/v1/ingest/document", req)
	if w.Code != 409 {
		t.Fatal("changed segment accepted")
	}
	req.TenantID = "other"
	w = postLifecycle(t, mux, "/v1/ingest/document", req)
	if !bytes.Contains(w.Body.Bytes(), []byte("accumulating")) {
		t.Fatal("cross-tenant upload assembled")
	}
}
func TestGatewayDocumentPartialReceipt(t *testing.T) {
	d := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	d.gw.RegisterRoutes(mux)
	data, _ := json.Marshal(documentRequest{TenantID: "t", WorkspaceID: "w", AgentID: "a", SourceID: "cancelled", Text: "some document text", ChunkSize: 4})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/v1/ingest/document", bytes.NewReader(data)).WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"status":"partial"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"failed_count"`)) {
		t.Fatalf("lost partial receipt: %d %s", w.Code, w.Body.String())
	}
}
