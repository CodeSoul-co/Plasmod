package access

import (
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
	"andb/src/internal/worker/nodes"
)

func buildTestGateway() *Gateway {
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	tieredObjs := storage.NewTieredObjectStore(store.HotCache(), store.Objects(), storage.NewInMemoryColdStore())
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

	return NewGateway(coord, runtime, store, nil)
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
