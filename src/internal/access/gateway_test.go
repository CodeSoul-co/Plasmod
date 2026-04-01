package access

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"andb/src/internal/coordinator"
	"andb/src/internal/dataplane"
	"andb/src/internal/eventbackbone"
	"andb/src/internal/evidence"
	"andb/src/internal/materialization"
	"andb/src/internal/semantic"
	"andb/src/internal/storage"
	"andb/src/internal/worker"
	"andb/src/internal/schemas"
	"andb/src/internal/worker/nodes"
)

type gatewayDeps struct {
	gw      *Gateway
	store   *storage.MemoryRuntimeStorage
	runtime *worker.Runtime
}

func buildTestGatewayWithDeps() gatewayDeps {
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	tieredObjs := storage.NewTieredObjectStore(store.HotCache(), store.Objects(), store.Edges(), storage.NewInMemoryColdStore())
	plane := dataplane.NewTieredDataPlane(tieredObjs)
	policy := semantic.NewPolicyEngine()
	planner := semantic.NewDefaultQueryPlanner()
	model := semantic.NewObjectModelRegistry()
	mat := materialization.NewService()
	evCache := evidence.NewCache(1000)
	preCompute := materialization.NewPreComputeService(evCache)
	assembler := evidence.NewCachedAssembler(evCache).WithEdgeStore(store.Edges())
	nodeManager := nodes.CreateManager()
	nodeManager.RegisterData(nodes.CreateInMemoryDataNode("d1", store.Segments()))
	nodeManager.RegisterIndex(nodes.CreateInMemoryIndexNode("i1", store.Indexes()))
	nodeManager.RegisterQuery(nodes.CreateInMemoryQueryNode("q1", plane))

	coord := coordinator.NewCoordinatorHub(
		coordinator.NewSchemaCoordinator(model),
		coordinator.NewObjectCoordinator(store.Objects(), store.Versions()),
		coordinator.NewPolicyCoordinator(policy, store.Policies()),
		coordinator.NewVersionCoordinator(clock, store.Versions()),
		coordinator.NewWorkerScheduler(),
		coordinator.NewMemoryCoordinator(store.Objects()),
		coordinator.NewIndexCoordinator(store.Segments(), store.Indexes()),
		coordinator.NewShardCoordinator(4),
		coordinator.NewQueryCoordinator(planner, policy),
	)

	runtime := worker.CreateRuntime(wal, bus, plane, coord, policy, planner, mat, preCompute, assembler, evCache, nil, nil, nodeManager, store, tieredObjs)
	runtime.RegisterDefaults()

	return gatewayDeps{
		gw:      NewGateway(coord, runtime, store, nil),
		store:   store,
		runtime: runtime,
	}
}

func buildTestGateway() *Gateway {
	return buildTestGatewayWithDeps().gw
}

func TestGateway_Healthz(t *testing.T) {
	gw := buildTestGateway()
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("/healthz: want 200, got %d", w.Code)
	}
}

func TestGateway_RegisterRoutes_NoDoubleRegister(t *testing.T) {
	gw := buildTestGateway()
	mux := http.NewServeMux()
	// Should not panic on double registration in tests.
	gw.RegisterRoutes(mux)
}

func TestGateway_Topology(t *testing.T) {
	gw := buildTestGateway()
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/topology", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("/v1/admin/topology: want 200, got %d", w.Code)
	}
}

func TestGateway_DatasetDelete_MethodNotAllowed(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/dataset/delete", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
}

func TestGateway_DatasetDelete_DryRunAndDelete(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	ev := schemas.Event{
		EventID:     "evt_delete_ds_1",
		TenantID:    "t_member_a",
		WorkspaceID: "w_member_a_dataset",
		AgentID:     "agent_member_a",
		SessionID:   "sess_member_a_dataset",
		EventType:   "user_message",
		Payload: map[string]any{
			"text": "dataset=deep1B.ibin row=1 dim=100 vec[:8]=[...]",
		},
		Source:  "test",
		Version: 1,
	}
	if _, err := deps.runtime.SubmitIngest(ev); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	memID := "mem_evt_delete_ds_1"

	// dry run
	body, _ := json.Marshal(map[string]any{
		"file_name":    "deep1B.ibin",
		"workspace_id": "w_member_a_dataset",
		"dry_run":      true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dry-run: want 200, got %d", w.Code)
	}
	var dry map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &dry); err != nil {
		t.Fatalf("decode dry-run response: %v", err)
	}
	if int(dry["matched"].(float64)) != 1 || int(dry["deleted"].(float64)) != 0 {
		t.Fatalf("dry-run mismatch: %+v", dry)
	}
	if mem, ok := deps.store.Objects().GetMemory(memID); !ok || !mem.IsActive {
		t.Fatalf("memory should remain active after dry-run")
	}

	// real delete
	body2, _ := json.Marshal(map[string]any{
		"file_name":    "deep1B.ibin",
		"workspace_id": "w_member_a_dataset",
		"dry_run":      false,
	})
	req2 := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/delete", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("delete: want 200, got %d", w2.Code)
	}
	if mem, ok := deps.store.Objects().GetMemory(memID); !ok || mem.IsActive {
		t.Fatalf("memory should be inactive after delete")
	}
}

func TestGateway_DatasetDelete_DeletedMemoryNotReturnedInQuery(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	ev := schemas.Event{
		EventID:     "evt_delete_ds_2",
		TenantID:    "t_member_a",
		WorkspaceID: "w_member_a_dataset",
		AgentID:     "agent_member_a",
		SessionID:   "sess_member_a_dataset",
		EventType:   "user_message",
		Payload: map[string]any{
			"text": "dataset=GT.public.ibin row=2 dim=10 vec[:8]=[...]",
		},
		Source:  "test",
		Version: 1,
	}
	if _, err := deps.runtime.SubmitIngest(ev); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	delBody, _ := json.Marshal(map[string]any{
		"file_name":    "GT.public.ibin",
		"workspace_id": "w_member_a_dataset",
		"dry_run":      false,
	})
	delReq := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/delete", bytes.NewReader(delBody))
	delReq.Header.Set("Content-Type", "application/json")
	delW := httptest.NewRecorder()
	mux.ServeHTTP(delW, delReq)
	if delW.Code != http.StatusOK {
		t.Fatalf("delete: want 200, got %d", delW.Code)
	}

	qBody, _ := json.Marshal(map[string]any{
		"query_text":    "dataset=GT.public.ibin row=2",
		"query_scope":   "w_member_a_dataset",
		"session_id":    "sess_member_a_dataset",
		"agent_id":      "agent_member_a",
		"tenant_id":     "t_member_a",
		"workspace_id":  "w_member_a_dataset",
		"top_k":         5,
		"response_mode": "structured_evidence",
	})
	qReq := httptest.NewRequest(http.MethodPost, "/v1/query", bytes.NewReader(qBody))
	qReq.Header.Set("Content-Type", "application/json")
	qW := httptest.NewRecorder()
	mux.ServeHTTP(qW, qReq)
	if qW.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d", qW.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(qW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode query response: %v", err)
	}
	objects, _ := resp["objects"].([]any)
	if len(objects) != 0 {
		t.Fatalf("expected deleted dataset memory not returned, got objects=%v", objects)
	}
}
