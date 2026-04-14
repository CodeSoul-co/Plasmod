package access

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"plasmod/src/internal/coordinator"
	"plasmod/src/internal/dataplane"
	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/evidence"
	"plasmod/src/internal/materialization"
	"plasmod/src/internal/semantic"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/worker/nodes"
)

type gatewayDeps struct {
	gw      *Gateway
	store   *storage.MemoryRuntimeStorage
	runtime *worker.Runtime
	cold    *storage.InMemoryColdStore
}

func buildTestGatewayWithDeps() gatewayDeps {
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	cold := storage.NewInMemoryColdStore()
	tieredObjs := storage.NewTieredObjectStore(store.HotCache(), store.Objects(), store.Edges(), cold)
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
		gw:      NewGateway(coord, runtime, store, nil, nil),
		store:   store,
		runtime: runtime,
		cold:    cold,
	}
}

// buildTestGatewayNoTieredRuntime wires Runtime with nil TieredObjectStore (ingest uses PutMemoryWithBaseEdges only).
func buildTestGatewayNoTieredRuntime() gatewayDeps {
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	cold := storage.NewInMemoryColdStore()
	plane := dataplane.NewTieredDataPlane(nil)
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

	runtime := worker.CreateRuntime(wal, bus, plane, coord, policy, planner, mat, preCompute, assembler, evCache, nil, nil, nodeManager, store, nil)
	runtime.RegisterDefaults()

	return gatewayDeps{
		gw:      NewGateway(coord, runtime, store, nil, nil),
		store:   store,
		runtime: runtime,
		cold:    cold,
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

func TestGateway_AdminAuth_DisabledByDefault(t *testing.T) {
	t.Setenv(EnvAdminAPIKey, "")

	gw := buildTestGateway()
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	h := WrapAdminAuth(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/topology", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 when admin auth disabled, got %d", w.Code)
	}
}

func TestGateway_AdminAuth_EnforcedForAdminPrefix(t *testing.T) {
	t.Setenv(EnvAdminAPIKey, "k_test_admin")

	gw := buildTestGateway()
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	h := WrapAdminAuth(mux)

	// No header → 401
	req1 := httptest.NewRequest(http.MethodGet, "/v1/admin/topology", nil)
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req1)
	if w1.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w1.Code)
	}

	// Wrong header → 401
	req2 := httptest.NewRequest(http.MethodGet, "/v1/admin/topology", nil)
	req2.Header.Set("X-Admin-Key", "wrong")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w2.Code)
	}

	// Correct header → 200
	req3 := httptest.NewRequest(http.MethodGet, "/v1/admin/topology", nil)
	req3.Header.Set("X-Admin-Key", "k_test_admin")
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w3.Code)
	}

	// Non-admin path should not require the key.
	req4 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w4 := httptest.NewRecorder()
	h.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("healthz want 200, got %d", w4.Code)
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

func TestGateway_DatasetDelete_WorkspaceIDRequired(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]any{
		"file_name": "deep1B.ibin",
		"dry_run":   true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
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
	_ = deps.cold.PutMemoryEmbedding(memID, []float32{1, 2, 3})

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
	if _, ok, _ := deps.cold.GetMemoryEmbedding(memID); !ok {
		t.Fatalf("cold embedding should remain after dry-run")
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
	if _, ok, _ := deps.cold.GetMemoryEmbedding(memID); !ok {
		t.Fatalf("cold embedding should remain after soft delete until purge")
	}
	if deps.runtime.TieredObjects().HotCache().Contains(memID) {
		t.Fatalf("hot cache should be evicted after soft delete")
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

func TestGateway_Query_BulkDatasetLoaderKeepsMultipleActiveRows(t *testing.T) {
	t.Setenv("ANDB_CONFLICT_MERGE_SKIP_DATASET_LOADER", "true")
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	events := []schemas.Event{
		{
			EventID:     "evt_bulk_query_1",
			TenantID:    "t_bulk",
			WorkspaceID: "w_bulk",
			AgentID:     "agent_loader",
			SessionID:   "sess_bulk_query",
			EventType:   "dataset_record",
			Payload: map[string]any{
				"text":        "dataset=bulk.fbin dataset_name:bulk_ds row:1 dim:4 head:1 2 3 4",
				"dataset":     "bulk_ds",
				"file_name":   "bulk.fbin",
				"ingest_mode": "bulk_dataset",
			},
			Source:  "dataset_loader",
			Version: 1,
		},
		{
			EventID:     "evt_bulk_query_2",
			TenantID:    "t_bulk",
			WorkspaceID: "w_bulk",
			AgentID:     "agent_loader",
			SessionID:   "sess_bulk_query",
			EventType:   "dataset_record",
			Payload: map[string]any{
				"text":        "dataset=bulk.fbin dataset_name:bulk_ds row:2 dim:4 head:5 6 7 8",
				"dataset":     "bulk_ds",
				"file_name":   "bulk.fbin",
				"ingest_mode": "bulk_dataset",
			},
			Source:  "dataset_loader",
			Version: 1,
		},
	}
	for _, ev := range events {
		if _, err := deps.runtime.SubmitIngest(ev); err != nil {
			t.Fatalf("ingest failed: %v", err)
		}
	}

	time.Sleep(250 * time.Millisecond)

	activeCount := 0
	for _, m := range deps.store.Objects().ListMemories("agent_loader", "sess_bulk_query") {
		if m.Scope == "w_bulk" && m.IsActive {
			activeCount++
		}
	}
	if activeCount < 2 {
		t.Fatalf("expected at least 2 active memories for bulk dataset rows, got %d", activeCount)
	}

	qBody, _ := json.Marshal(map[string]any{
		"query_text":    "dataset_name:bulk_ds",
		"query_scope":   "w_bulk",
		"session_id":    "sess_bulk_query",
		"agent_id":      "agent_loader",
		"tenant_id":     "t_bulk",
		"workspace_id":  "w_bulk",
		"top_k":         10,
		"response_mode": "structured_evidence",
		"include_cold":  true,
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
	if len(objects) < 2 {
		t.Fatalf("expected query to return multiple active bulk rows, got %d objects: %v", len(objects), resp)
	}
}

func TestGateway_Query_LatestBatchOnlySelector(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	events := []schemas.Event{
		{
			EventID:     "evt_query_batch_old",
			TenantID:    "t_batch",
			WorkspaceID: "w_batch_query",
			AgentID:     "a_loader",
			SessionID:   "s_batch_query",
			EventType:   "dataset_record",
			Payload: map[string]any{
				"text":            "dataset=deep1B.ibin row:1",
				"dataset":         "deep1B",
				"file_name":       "deep1B.ibin",
				"import_batch_id": "batch_old",
			},
			Source:  "dataset_loader",
			Version: 1,
		},
		{
			EventID:     "evt_query_batch_new",
			TenantID:    "t_batch",
			WorkspaceID: "w_batch_query",
			AgentID:     "a_loader",
			SessionID:   "s_batch_query",
			EventType:   "dataset_record",
			Payload: map[string]any{
				"text":            "dataset=deep1B.ibin row:2",
				"dataset":         "deep1B",
				"file_name":       "deep1B.ibin",
				"import_batch_id": "batch_new",
			},
			Source:  "dataset_loader",
			Version: 1,
		},
	}
	for _, ev := range events {
		if _, err := deps.runtime.SubmitIngest(ev); err != nil {
			t.Fatalf("ingest failed: %v", err)
		}
	}

	qBody, _ := json.Marshal(map[string]any{
		"query_text":       "dataset=deep1B.ibin",
		"query_scope":      "w_batch_query",
		"session_id":       "s_batch_query",
		"agent_id":         "a_loader",
		"tenant_id":        "t_batch",
		"workspace_id":     "w_batch_query",
		"top_k":            10,
		"response_mode":    "structured_evidence",
		"dataset_name":     "deep1B",
		"source_file_name": "deep1B.ibin",
		"latest_batch_only": true,
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
	if len(objects) != 1 || objects[0] != "mem_evt_query_batch_new" {
		t.Fatalf("expected latest batch object only, got objects=%v", objects)
	}
}

func TestGateway_DatasetDelete_CompatViaSourceEventIDs_WhenPolicyTagsEmpty(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	ev := schemas.Event{
		EventID:     "evt_delete_ds_compat_1",
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
	memID := "mem_evt_delete_ds_compat_1"

	mem, ok := deps.store.Objects().GetMemory(memID)
	if !ok {
		t.Fatalf("expected memory %s to exist", memID)
	}
	if len(mem.SourceEventIDs) == 0 {
		t.Fatalf("expected memory.SourceEventIDs to be populated for compatibility path")
	}

	// Force the delete path to skip PolicyTags matching and rely on SourceEventIDs.
	mem.PolicyTags = nil
	deps.store.Objects().PutMemory(mem)

	body, _ := json.Marshal(map[string]any{
		"file_name":    "deep1B.ibin",
		"workspace_id": "w_member_a_dataset",
		"dry_run":      false,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete: want 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if int(resp["matched"].(float64)) != 1 || int(resp["deleted"].(float64)) != 1 {
		t.Fatalf("delete response mismatch: %+v", resp)
	}
	if m2, ok := deps.store.Objects().GetMemory(memID); !ok || m2.IsActive {
		t.Fatalf("memory should be inactive after delete")
	}
}

func TestGateway_DatasetPurge_DryRun(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	ev := schemas.Event{
		EventID:     "evt_purge_ds_1",
		TenantID:    "t_member_a",
		WorkspaceID: "w_purge",
		AgentID:     "agent_a",
		SessionID:   "sess_a",
		EventType:   "user_message",
		Payload: map[string]any{
			"text": "dataset=purge.bin row=1 dataset_name:purgeDS",
		},
		Source:  "test",
		Version: 1,
	}
	if _, err := deps.runtime.SubmitIngest(ev); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	memID := "mem_evt_purge_ds_1"

	delBody, _ := json.Marshal(map[string]any{
		"dataset_name":   "purgeDS",
		"workspace_id":   "w_purge",
		"dry_run":        false,
	})
	delReq := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/delete", bytes.NewReader(delBody))
	delReq.Header.Set("Content-Type", "application/json")
	delW := httptest.NewRecorder()
	mux.ServeHTTP(delW, delReq)
	if delW.Code != http.StatusOK {
		t.Fatalf("soft delete: want 200, got %d", delW.Code)
	}

	purgeBody, _ := json.Marshal(map[string]any{
		"dataset_name": "purgeDS",
		"workspace_id": "w_purge",
		"dry_run":      true,
	})
	purgeReq := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/purge", bytes.NewReader(purgeBody))
	purgeReq.Header.Set("Content-Type", "application/json")
	purgeW := httptest.NewRecorder()
	mux.ServeHTTP(purgeW, purgeReq)
	if purgeW.Code != http.StatusOK {
		t.Fatalf("purge dry-run: want 200, got %d", purgeW.Code)
	}
	var pr map[string]any
	if err := json.Unmarshal(purgeW.Body.Bytes(), &pr); err != nil {
		t.Fatalf("decode purge response: %v", err)
	}
	if int(pr["matched"].(float64)) != 1 || int(pr["purgeable"].(float64)) != 1 || int(pr["purged"].(float64)) != 0 {
		t.Fatalf("purge dry-run mismatch: %+v", pr)
	}
	if _, ok := deps.store.Objects().GetMemory(memID); !ok {
		t.Fatalf("memory should still exist after purge dry-run")
	}
}

func TestGateway_DatasetPurge_RemovesMemoryAndAudit(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	ev := schemas.Event{
		EventID:     "evt_purge_ds_2",
		TenantID:    "t_member_a",
		WorkspaceID: "w_purge2",
		AgentID:     "agent_a",
		SessionID:   "sess_a",
		EventType:   "user_message",
		Payload: map[string]any{
			"text": "dataset=purge2.bin row=1 dataset_name:purgeDS2",
		},
		Source:  "test",
		Version: 1,
	}
	if _, err := deps.runtime.SubmitIngest(ev); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	memID := "mem_evt_purge_ds_2"
	_ = deps.cold.PutMemoryEmbedding(memID, []float32{1, 2, 3})

	delBody, _ := json.Marshal(map[string]any{
		"dataset_name":   "purgeDS2",
		"workspace_id":   "w_purge2",
		"dry_run":        false,
	})
	delReq := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/delete", bytes.NewReader(delBody))
	delReq.Header.Set("Content-Type", "application/json")
	delW := httptest.NewRecorder()
	mux.ServeHTTP(delW, delReq)
	if delW.Code != http.StatusOK {
		t.Fatalf("soft delete: want 200, got %d", delW.Code)
	}

	purgeBody, _ := json.Marshal(map[string]any{
		"dataset_name": "purgeDS2",
		"workspace_id": "w_purge2",
		"dry_run":      false,
	})
	purgeReq := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/purge", bytes.NewReader(purgeBody))
	purgeReq.Header.Set("Content-Type", "application/json")
	purgeW := httptest.NewRecorder()
	mux.ServeHTTP(purgeW, purgeReq)
	if purgeW.Code != http.StatusOK {
		t.Fatalf("purge: want 200, got %d", purgeW.Code)
	}
	var pr map[string]any
	if err := json.Unmarshal(purgeW.Body.Bytes(), &pr); err != nil {
		t.Fatalf("decode purge response: %v", err)
	}
	if int(pr["purged"].(float64)) != 1 {
		t.Fatalf("purge count mismatch: %+v", pr)
	}
	if _, ok := deps.store.Objects().GetMemory(memID); ok {
		t.Fatalf("memory should be removed after purge")
	}
	if _, ok, _ := deps.cold.GetMemoryEmbedding(memID); ok {
		t.Fatalf("cold embedding should be removed after purge")
	}
	audits := deps.store.Audits().GetAudits(memID)
	if len(audits) != 1 {
		t.Fatalf("expected one audit record, got %d", len(audits))
	}
	if audits[0].ReasonCode != "dataset_purge" {
		t.Fatalf("unexpected audit reason: %q", audits[0].ReasonCode)
	}
	if pr["purge_backend"] != "tiered" {
		t.Fatalf("expected purge_backend=tiered, got %v", pr["purge_backend"])
	}
}

func TestGateway_DatasetPurge_WarmOnlyWithoutTieredRuntime(t *testing.T) {
	deps := buildTestGatewayNoTieredRuntime()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	ev := schemas.Event{
		EventID:     "evt_purge_warmonly",
		TenantID:    "t_a",
		WorkspaceID: "w_warmonly",
		AgentID:     "agent_a",
		SessionID:   "sess_a",
		EventType:   "user_message",
		Payload: map[string]any{
			"text": "dataset=w.bin row=1 dataset_name:DSW",
		},
		Source:  "test",
		Version: 1,
	}
	if _, err := deps.runtime.SubmitIngest(ev); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	memID := "mem_evt_purge_warmonly"

	delBody, _ := json.Marshal(map[string]any{
		"dataset_name":   "DSW",
		"workspace_id":   "w_warmonly",
		"dry_run":        false,
	})
	delReq := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/delete", bytes.NewReader(delBody))
	delReq.Header.Set("Content-Type", "application/json")
	delW := httptest.NewRecorder()
	mux.ServeHTTP(delW, delReq)
	if delW.Code != http.StatusOK {
		t.Fatalf("soft delete: want 200, got %d", delW.Code)
	}

	purgeBody, _ := json.Marshal(map[string]any{
		"dataset_name": "DSW",
		"workspace_id": "w_warmonly",
		"dry_run":      false,
	})
	purgeReq := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/purge", bytes.NewReader(purgeBody))
	purgeReq.Header.Set("Content-Type", "application/json")
	purgeW := httptest.NewRecorder()
	mux.ServeHTTP(purgeW, purgeReq)
	if purgeW.Code != http.StatusOK {
		t.Fatalf("purge: want 200, got %d body=%s", purgeW.Code, purgeW.Body.String())
	}
	var pr map[string]any
	if err := json.Unmarshal(purgeW.Body.Bytes(), &pr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pr["purge_backend"] != "warm_only" {
		t.Fatalf("expected purge_backend=warm_only, got %v", pr["purge_backend"])
	}
	if _, ok := deps.store.Objects().GetMemory(memID); ok {
		t.Fatalf("memory should be removed from warm store")
	}
}

func TestGateway_ListMemory_WorkspaceIDFilter(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	deps.store.Objects().PutMemory(schemas.Memory{
		MemoryID: "mem-ws1", Scope: "ws-target", IsActive: true,
	})
	deps.store.Objects().PutMemory(schemas.Memory{
		MemoryID: "mem-ws2", Scope: "ws-other", IsActive: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/memory?workspace_id=ws-target", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var mems []schemas.Memory
	if err := json.Unmarshal(w.Body.Bytes(), &mems); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 memory for ws-target, got %d", len(mems))
	}
	if mems[0].MemoryID != "mem-ws1" {
		t.Fatalf("expected mem-ws1, got %s", mems[0].MemoryID)
	}
}

func TestGateway_AdminDataWipe(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	deps.store.Objects().PutMemory(schemas.Memory{MemoryID: "mem_wipe_test", Content: "keep"})
	if _, ok := deps.store.Objects().GetMemory("mem_wipe_test"); !ok {
		t.Fatal("expected memory before wipe")
	}

	body, _ := json.Marshal(map[string]string{"confirm": "delete_all_data"})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/data/wipe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("wipe: want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("status: %+v", resp)
	}
	if _, ok := deps.store.Objects().GetMemory("mem_wipe_test"); ok {
		t.Fatal("memory should be removed after wipe")
	}
}
