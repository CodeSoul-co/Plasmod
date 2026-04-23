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
	"plasmod/src/internal/metrics"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/semantic"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker"
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

func TestGateway_EffectiveConfig(t *testing.T) {
	gw := buildTestGateway()
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	t.Setenv("PLASMOD_RRF_K", "88")
	t.Setenv("PLASMOD_COLD_BATCH_SIZE", "222")
	t.Setenv("PLASMOD_HOT_TIER_THRESHOLD", "0.75")

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/config/effective", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/v1/admin/config/effective: want 200, got %d", w.Code)
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	cfg, ok := got["algorithm_config"].(map[string]any)
	if !ok {
		t.Fatalf("expected algorithm_config in response, got %v", got)
	}
	if int(cfg["RRFK"].(float64)) != 88 {
		t.Fatalf("expected RRFK=88, got %v", cfg["RRFK"])
	}
	if int(cfg["ColdBatchSize"].(float64)) != 222 {
		t.Fatalf("expected ColdBatchSize=222, got %v", cfg["ColdBatchSize"])
	}
	if cfg["HotTierSalienceThreshold"].(float64) != 0.75 {
		t.Fatalf("expected HotTierSalienceThreshold=0.75, got %v", cfg["HotTierSalienceThreshold"])
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
		"query_text":        "dataset=deep1B.ibin",
		"query_scope":       "w_batch_query",
		"session_id":        "s_batch_query",
		"agent_id":          "a_loader",
		"tenant_id":         "t_batch",
		"workspace_id":      "w_batch_query",
		"top_k":             10,
		"response_mode":     "structured_evidence",
		"dataset_name":      "deep1B",
		"source_file_name":  "deep1B.ibin",
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

func TestGateway_Query_LatestBatchOnly_RequiresWorkspaceID(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	qBody, _ := json.Marshal(map[string]any{
		"query_text":        "dataset=deep1B.ibin",
		"query_scope":       "w_batch_query",
		"session_id":        "s_batch_query",
		"agent_id":          "a_loader",
		"tenant_id":         "t_batch",
		"top_k":             10,
		"response_mode":     "structured_evidence",
		"dataset_name":      "deep1B",
		"source_file_name":  "deep1B.ibin",
		"latest_batch_only": true,
	})
	qReq := httptest.NewRequest(http.MethodPost, "/v1/query", bytes.NewReader(qBody))
	qReq.Header.Set("Content-Type", "application/json")
	qW := httptest.NewRecorder()
	mux.ServeHTTP(qW, qReq)
	if qW.Code != http.StatusBadRequest {
		t.Fatalf("query: want 400, got %d", qW.Code)
	}
	if got := qW.Body.String(); got != "latest_batch_only requires workspace_id\n" {
		t.Fatalf("unexpected body: %q", got)
	}
}

func TestGateway_Query_LatestBatchOnly_RequiresDatasetOrSourceFile(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	qBody, _ := json.Marshal(map[string]any{
		"query_text":        "dataset=deep1B.ibin",
		"query_scope":       "w_batch_query",
		"session_id":        "s_batch_query",
		"agent_id":          "a_loader",
		"tenant_id":         "t_batch",
		"workspace_id":      "w_batch_query",
		"top_k":             10,
		"response_mode":     "structured_evidence",
		"latest_batch_only": true,
	})
	qReq := httptest.NewRequest(http.MethodPost, "/v1/query", bytes.NewReader(qBody))
	qReq.Header.Set("Content-Type", "application/json")
	qW := httptest.NewRecorder()
	mux.ServeHTTP(qW, qReq)
	if qW.Code != http.StatusBadRequest {
		t.Fatalf("query: want 400, got %d", qW.Code)
	}
	if got := qW.Body.String(); got != "latest_batch_only requires dataset_name or source_file_name\n" {
		t.Fatalf("unexpected body: %q", got)
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
		"dataset_name": "purgeDS",
		"workspace_id": "w_purge",
		"dry_run":      false,
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
	if _, ok := pr["purge_scan_elapsed_ms"]; !ok {
		t.Fatalf("expected purge_scan_elapsed_ms in response: %+v", pr)
	}
	if _, ok := pr["purge_delete_elapsed_ms"]; !ok {
		t.Fatalf("expected purge_delete_elapsed_ms in response: %+v", pr)
	}
	if _, ok := pr["purge_response_build_elapsed_ms"]; !ok {
		t.Fatalf("expected purge_response_build_elapsed_ms in response: %+v", pr)
	}
	if _, ok := deps.store.Objects().GetMemory(memID); !ok {
		t.Fatalf("memory should still exist after purge dry-run")
	}
}

func TestGateway_DatasetPurge_WithoutMemoryIDs(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	ev := schemas.Event{
		EventID:     "evt_purge_ds_no_ids",
		TenantID:    "t_member_a",
		WorkspaceID: "w_purge_no_ids",
		AgentID:     "agent_a",
		SessionID:   "sess_a",
		EventType:   "user_message",
		Payload: map[string]any{
			"text": "dataset=purge_no_ids.bin row=1 dataset_name:purgeNoIDs",
		},
		Source:  "test",
		Version: 1,
	}
	if _, err := deps.runtime.SubmitIngest(ev); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	purgeBody, _ := json.Marshal(map[string]any{
		"dataset_name":       "purgeNoIDs",
		"workspace_id":       "w_purge_no_ids",
		"dry_run":            true,
		"include_memory_ids": false,
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
	if v, ok := pr["include_memory_ids"].(bool); !ok || v {
		t.Fatalf("expected include_memory_ids=false in response, got %v", pr["include_memory_ids"])
	}
	if v, ok := pr["memory_ids_omitted"].(bool); !ok || !v {
		t.Fatalf("expected memory_ids_omitted=true, got %v", pr["memory_ids_omitted"])
	}
	if _, ok := pr["memory_ids"]; ok {
		t.Fatal("expected memory_ids to be omitted when include_memory_ids=false")
	}
	if _, ok := pr["purged_memory_ids"]; ok {
		t.Fatal("expected purged_memory_ids to be omitted when include_memory_ids=false")
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
		"dataset_name": "purgeDS2",
		"workspace_id": "w_purge2",
		"dry_run":      false,
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
		"dataset_name": "DSW",
		"workspace_id": "w_warmonly",
		"dry_run":      false,
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

func TestGateway_DatasetPurge_AsyncIdempotencyReturnsExistingTask(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	ev := schemas.Event{
		EventID:     "evt_purge_async_dedupe",
		TenantID:    "t_member_a",
		WorkspaceID: "w_purge_async_dedupe",
		AgentID:     "agent_a",
		SessionID:   "sess_a",
		EventType:   "user_message",
		Payload: map[string]any{
			"text": "dataset=purge-async.bin row=1 dataset_name:purgeAsync",
		},
		Source:  "test",
		Version: 1,
	}
	if _, err := deps.runtime.SubmitIngest(ev); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}
	memID := "mem_evt_purge_async_dedupe"

	// Pre-create an active async task with the same idempotency key to verify
	// that gateway returns accepted + existing task instead of falling back to sync purge.
	existingTask := &hardDeleteTask{
		TaskID:         "purge_task_existing",
		WorkspaceID:    "w_purge_async_dedupe",
		DatasetName:    "purgeAsync",
		MemoryIDs:      []string{"mem_nonexistent"},
		State:          hardDeleteStateQueued,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
		IdempotencyKey: "idem-dup",
	}
	deps.gw.hardDeleteMgr.mu.Lock()
	deps.gw.hardDeleteMgr.tasks[existingTask.TaskID] = existingTask
	deps.gw.hardDeleteMgr.mu.Unlock()

	purgeBody, _ := json.Marshal(map[string]any{
		"dataset_name":    "purgeAsync",
		"workspace_id":    "w_purge_async_dedupe",
		"only_if_inactive": false,
		"dry_run":         false,
		"async":           true,
		"idempotency_key": "idem-dup",
	})
	purgeReq := httptest.NewRequest(http.MethodPost, "/v1/admin/dataset/purge", bytes.NewReader(purgeBody))
	purgeReq.Header.Set("Content-Type", "application/json")
	purgeW := httptest.NewRecorder()
	mux.ServeHTTP(purgeW, purgeReq)
	if purgeW.Code != http.StatusOK {
		t.Fatalf("purge async: want 200, got %d body=%s", purgeW.Code, purgeW.Body.String())
	}
	var pr map[string]any
	if err := json.Unmarshal(purgeW.Body.Bytes(), &pr); err != nil {
		t.Fatalf("decode purge response: %v", err)
	}
	if pr["status"] != "accepted" {
		t.Fatalf("expected accepted status, got %+v", pr)
	}
	if v, ok := pr["deduplicated"].(bool); !ok || !v {
		t.Fatalf("expected deduplicated=true, got %+v", pr)
	}
	if pr["task_id"] != existingTask.TaskID {
		t.Fatalf("expected existing task_id=%s, got %v", existingTask.TaskID, pr["task_id"])
	}
	if _, ok := deps.store.Objects().GetMemory(memID); !ok {
		t.Fatalf("memory should not be synchronously purged on idempotency dedupe")
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

func TestGateway_AdminMetrics(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var snap map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := snap["query_latency"]; !ok {
		t.Fatal("expected query_latency in metrics snapshot")
	}
	if _, ok := snap["write_latency"]; !ok {
		t.Fatal("expected write_latency in metrics snapshot")
	}
	if _, ok := snap["go_goroutines"]; !ok {
		t.Fatal("expected go_goroutines in metrics snapshot")
	}
}

func TestGateway_AdminGovernanceMode(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	getReq := httptest.NewRequest(http.MethodGet, "/v1/admin/governance-mode", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, getReq)
	if w.Code != http.StatusOK {
		t.Fatalf("GET want 200, got %d", w.Code)
	}

	body, _ := json.Marshal(map[string]bool{"enabled": false})
	postReq := httptest.NewRequest(http.MethodPost, "/v1/admin/governance-mode", bytes.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, postReq)
	if w2.Code != http.StatusOK {
		t.Fatalf("POST want 200, got %d body=%s", w2.Code, w2.Body.String())
	}
	if !deps.runtime.GovernanceDisabled {
		t.Fatal("expected GovernanceDisabled=true after enabled=false")
	}
}

func TestGateway_AdminRuntimeMode(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	vomTrue := true
	body, _ := json.Marshal(map[string]*bool{"vector_only_mode": &vomTrue})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/runtime-mode", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !deps.runtime.VectorOnlyMode {
		t.Fatal("expected VectorOnlyMode=true")
	}
}

func TestGateway_MemoryMarkStale(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	deps.store.Objects().PutMemory(schemas.Memory{
		MemoryID: "stale-target", IsActive: true,
		LifecycleState: "active",
	})

	body, _ := json.Marshal(map[string]string{"memory_id": "stale-target", "reason": "test"})
	req := httptest.NewRequest(http.MethodPost, "/v1/internal/memory/stale", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	mem, ok := deps.store.Objects().GetMemory("stale-target")
	if !ok {
		t.Fatal("memory should still exist")
	}
	if mem.LifecycleState != "stale" {
		t.Fatalf("expected lifecycle_state=stale, got %s", mem.LifecycleState)
	}
	if mem.IsActive {
		t.Fatal("expected IsActive=false after mark_stale")
	}
}

func TestGateway_MemoryConflictInject(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]any{
		"agent_id":   "agent-x",
		"session_id": "sess-x",
		"content_a":  "sky is blue",
		"content_b":  "sky is red",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/internal/memory/conflict/inject", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	idA, _ := resp["memory_id_a"].(string)
	idB, _ := resp["memory_id_b"].(string)
	if idA == "" || idB == "" {
		t.Fatalf("expected memory IDs in response: %+v", resp)
	}
	if _, ok := deps.store.Objects().GetMemory(idA); !ok {
		t.Fatal("memory A not found after inject")
	}
	if _, ok := deps.store.Objects().GetMemory(idB); !ok {
		t.Fatal("memory B not found after inject")
	}
	edges := deps.store.Edges().EdgesFrom(idA)
	found := false
	for _, e := range edges {
		if e.EdgeType == "conflict" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected conflict edge between injected memories")
	}
}

func TestGateway_TaskLifecycle(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	sessID := "test-sess-lifecycle"

	// start
	b, _ := json.Marshal(map[string]string{"session_id": sessID, "task_type": "analysis", "goal": "test goal"})
	req := httptest.NewRequest(http.MethodPost, "/v1/internal/task/start", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("task/start: want 200, got %d: %s", w.Code, w.Body.String())
	}
	sess, ok := deps.store.Objects().GetSession(sessID)
	if !ok {
		t.Fatal("session should be created on task start")
	}
	if sess.TaskType != "analysis" {
		t.Fatalf("expected task_type=analysis, got %s", sess.TaskType)
	}

	// tokens
	b, _ = json.Marshal(map[string]any{"session_id": sessID, "tokens": 256})
	req = httptest.NewRequest(http.MethodPost, "/v1/internal/task/tokens", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("task/tokens: want 200, got %d", w.Code)
	}

	// complete
	b, _ = json.Marshal(map[string]any{"session_id": sessID, "success": true, "duration_ms": 500.0})
	req = httptest.NewRequest(http.MethodPost, "/v1/internal/task/complete", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("task/complete: want 200, got %d: %s", w.Code, w.Body.String())
	}
	snap, ok2 := metrics.Global().SessionSnapshot(sessID)
	if !ok2 {
		t.Fatal("expected session snapshot to exist after task/complete")
	}
	if !snap.Done {
		t.Fatal("task should be marked done")
	}
	if snap.Tokens != 256 {
		t.Fatalf("expected 256 tokens, got %d", snap.Tokens)
	}
}

func TestGateway_PlanRepair(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	sessID := "test-sess-plan"

	b, _ := json.Marshal(map[string]any{"session_id": sessID, "step_index": 1, "step_description": "fetch data"})
	req := httptest.NewRequest(http.MethodPost, "/v1/internal/plan/step", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("plan/step: want 200, got %d", w.Code)
	}

	b, _ = json.Marshal(map[string]any{"session_id": sessID, "success": true, "reason": "adjusted query"})
	req = httptest.NewRequest(http.MethodPost, "/v1/internal/plan/repair", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("plan/repair: want 200, got %d", w.Code)
	}
}

func TestGateway_IngestDocument(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	text := "First paragraph with some content.\n\nSecond paragraph with more content.\n\nThird paragraph for testing."
	b, _ := json.Marshal(map[string]any{
		"agent_id":   "agent-doc",
		"session_id": "sess-doc",
		"text":       text,
		"chunk_size": 40,
		"title":      "test_doc",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest/document", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ingest/document: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["batch_id"] == "" {
		t.Fatal("expected batch_id in response")
	}
	chunks := resp["chunks"].(float64)
	if chunks < 1 {
		t.Fatalf("expected at least 1 chunk, got %v", chunks)
	}
	ids, _ := resp["memory_ids"].([]any)
	if len(ids) == 0 {
		t.Fatal("expected at least one memory_id")
	}
}

func TestGateway_TaskStage(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]any{
		"session_id":   "sess-stage",
		"agent_id":     "agent-stage",
		"stage":        "draft",
		"stage_index":  1,
		"total_stages": 4,
		"description":  "writing draft section",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/internal/task/stage", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("task/stage: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["stage"] != "draft" {
		t.Fatalf("expected stage=draft, got %v", resp["stage"])
	}
}

func TestGateway_MASAnswerConsistency(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]any{"score": 0.85, "session_id": "sess-mas", "agent_id": "agent-a"})
	req := httptest.NewRequest(http.MethodPost, "/v1/internal/mas/answer-consistency", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("mas/answer-consistency: want 200, got %d: %s", w.Code, w.Body.String())
	}

	// Score out of range should be rejected.
	body, _ = json.Marshal(map[string]any{"score": 1.5})
	req = httptest.NewRequest(http.MethodPost, "/v1/internal/mas/answer-consistency", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range score, got %d", w.Code)
	}
}

func TestGateway_MASAggregate(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	// Create two memories for agent-b, each in their own scope.
	deps.store.Objects().PutMemory(schemas.Memory{
		MemoryID: "mem-b1", AgentID: "agent-b", Scope: "scope-b", IsActive: true,
	})
	// No share contract → memory should be blocked.
	body, _ := json.Marshal(map[string]any{
		"requester_agent_id": "agent-a",
		"source_agent_ids":   []string{"agent-b"},
		"top_k":              5,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/internal/mas/aggregate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("mas/aggregate: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	blocked := resp["contamination_blocked"].(float64)
	if blocked < 1 {
		t.Fatalf("expected at least 1 contamination blocked, got %v", blocked)
	}
}

func TestGateway_ToolState(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	// Store a tool_call_issued event.
	deps.store.Objects().PutEvent(schemas.Event{
		EventID:   "evt-call-1",
		AgentID:   "agent-tool",
		SessionID: "sess-tool",
		EventType: string(schemas.EventTypeToolCallIssued),
		Payload:   map[string]any{"tool": "search", "args": map[string]any{"query": "plasmod"}},
	})
	// And a matching result.
	deps.store.Objects().PutEvent(schemas.Event{
		EventID:       "evt-result-1",
		AgentID:       "agent-tool",
		SessionID:     "sess-tool",
		EventType:     string(schemas.EventTypeToolResultReturned),
		ParentEventID: "evt-call-1",
		Payload:       map[string]any{"result": map[string]any{"hits": 3}},
	})
	// An unmatched call.
	deps.store.Objects().PutEvent(schemas.Event{
		EventID:   "evt-call-2",
		AgentID:   "agent-tool",
		SessionID: "sess-tool",
		EventType: string(schemas.EventTypeToolCallIssued),
		Payload:   map[string]any{"tool": "fetch", "args": map[string]any{"url": "http://example.com"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/internal/tool-state?agent_id=agent-tool&session_id=sess-tool", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tool-state: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	pending, _ := resp["pending"].([]any)
	completed, _ := resp["completed"].([]any)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending call, got %d", len(pending))
	}
	if len(completed) != 1 {
		t.Fatalf("expected 1 completed call, got %d", len(completed))
	}
}

func TestGateway_AgentHandoff(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	deps.store.Objects().PutAgent(schemas.Agent{AgentID: "planner-1", RoleProfile: "planner"})
	deps.store.Objects().PutAgent(schemas.Agent{AgentID: "executor-1", RoleProfile: ""})

	body, _ := json.Marshal(map[string]any{
		"from_agent_id": "planner-1",
		"to_agent_id":   "executor-1",
		"session_id":    "sess-handoff",
		"role_from":     "planner",
		"role_to":       "executor",
		"context":       map[string]any{"task": "write report"},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/internal/agent/handoff", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("agent/handoff: want 200, got %d: %s", w.Code, w.Body.String())
	}

	// Executor's role should be updated.
	agent, ok := deps.store.Objects().GetAgent("executor-1")
	if !ok {
		t.Fatal("executor agent should exist")
	}
	if agent.RoleProfile != "executor" {
		t.Fatalf("expected role=executor, got %q", agent.RoleProfile)
	}
}

func TestGateway_AgentList(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	deps.store.Objects().PutAgent(schemas.Agent{AgentID: "a1", RoleProfile: "planner", WorkspaceID: "ws1"})
	deps.store.Objects().PutAgent(schemas.Agent{AgentID: "a2", RoleProfile: "executor", WorkspaceID: "ws1"})
	deps.store.Objects().PutAgent(schemas.Agent{AgentID: "a3", RoleProfile: "planner", WorkspaceID: "ws2"})

	// Filter by role.
	req := httptest.NewRequest(http.MethodGet, "/v1/agent/list?role=planner", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("agent/list: want 200, got %d", w.Code)
	}
	var agents []schemas.Agent
	if err := json.Unmarshal(w.Body.Bytes(), &agents); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 planners, got %d", len(agents))
	}

	// Filter by role + workspace.
	req = httptest.NewRequest(http.MethodGet, "/v1/agent/list?role=planner&workspace_id=ws1", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if err := json.Unmarshal(w.Body.Bytes(), &agents); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(agents) != 1 || agents[0].AgentID != "a1" {
		t.Fatalf("expected only a1, got %v", agents)
	}
}

func TestGateway_SessionContext(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	deps.store.Objects().PutEvent(schemas.Event{
		EventID: "ev-user-1", AgentID: "agent-ctx", SessionID: "sess-ctx",
		EventType: string(schemas.EventTypeUserMessage),
		Payload:   map[string]any{"text": "hello"},
	})
	deps.store.Objects().PutEvent(schemas.Event{
		EventID: "ev-asst-1", AgentID: "agent-ctx", SessionID: "sess-ctx",
		EventType: string(schemas.EventTypeAssistantMessage),
		Payload:   map[string]any{"text": "hi there"},
	})
	deps.store.Objects().PutEvent(schemas.Event{
		EventID: "ev-tool-1", AgentID: "agent-ctx", SessionID: "sess-ctx",
		EventType: "irrelevant_type",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/internal/session/context?session_id=sess-ctx&agent_id=agent-ctx", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("session/context: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	evs, _ := resp["events"].([]any)
	if len(evs) != 2 {
		t.Fatalf("expected 2 relevant events, got %d", len(evs))
	}
	if resp["turns"].(float64) != 3 {
		t.Fatalf("expected turns=3, got %v", resp["turns"])
	}
}

func TestGateway_EvalGroundTruth(t *testing.T) {
	deps := buildTestGatewayWithDeps()
	mux := http.NewServeMux()
	deps.gw.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]any{
		"task_id":  "task-gt-1",
		"expected": "Paris",
		"metadata": map[string]any{"source": "unit-test"},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/internal/eval/ground-truth", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("eval/ground-truth POST: want 200, got %d: %s", w.Code, w.Body.String())
	}

	// Retrieve by task_id.
	req = httptest.NewRequest(http.MethodGet, "/v1/internal/eval/ground-truth?task_id=task-gt-1", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("eval/ground-truth GET: want 200, got %d", w.Code)
	}
	var rec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rec); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec["expected"] != "Paris" {
		t.Fatalf("expected 'Paris', got %v", rec["expected"])
	}

	// Unknown task_id → 404.
	req = httptest.NewRequest(http.MethodGet, "/v1/internal/eval/ground-truth?task_id=no-such", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown task, got %d", w.Code)
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
