package grpcapi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"plasmod/src/internal/access"
	plasmodv1 "plasmod/src/internal/api/grpc/pb/plasmod/v1"
	"plasmod/src/internal/coordinator"
	"plasmod/src/internal/dataplane"
	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/evidence"
	"plasmod/src/internal/materialization"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/semantic"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker"
	"plasmod/src/internal/worker/consistency"
	"plasmod/src/internal/worker/nodes"
)

func TestMapServiceErrorUnavailable(t *testing.T) {
	err := mapServiceError(fmt.Errorf("too many concurrent writes; try again later"))
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error")
	}
	if st.Code() != codes.Unavailable {
		t.Fatalf("code=%v want Unavailable", st.Code())
	}
}

func TestMapServiceErrorInvalidArgument(t *testing.T) {
	err := mapServiceError(fmt.Errorf("vectors is required"))
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error")
	}
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("code=%v want InvalidArgument", st.Code())
	}
}

func TestMapServiceErrorConsistencyAndContextCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{name: "backpressure", err: consistency.ErrBackpressure, want: codes.Unavailable},
		{name: "paused", err: consistency.ErrPaused, want: codes.Unavailable},
		{name: "deadline", err: context.DeadlineExceeded, want: codes.DeadlineExceeded},
		{name: "canceled", err: context.Canceled, want: codes.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := status.Code(mapServiceError(tt.err)); got != tt.want {
				t.Fatalf("code=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestAPIServerIngestEventPreservesConsistencyAcknowledgement(t *testing.T) {
	clock := eventbackbone.NewHybridClock()
	bus := eventbackbone.NewInMemoryBus()
	wal := eventbackbone.NewInMemoryWAL(bus, clock)
	store := storage.NewMemoryRuntimeStorage()
	tieredObjects := storage.NewTieredObjectStore(
		store.HotCache(), store.Objects(), store.Edges(), storage.NewInMemoryColdStore(),
	)
	plane := dataplane.NewTieredDataPlane(tieredObjects)
	policy := semantic.NewPolicyEngine()
	planner := semantic.NewDefaultQueryPlanner()
	nodeManager := nodes.CreateManager()
	nodeManager.RegisterQuery(nodes.CreateInMemoryQueryNode("query", plane))
	coord := coordinator.NewCoordinatorHub(
		coordinator.NewSchemaCoordinator(semantic.NewObjectModelRegistry()),
		coordinator.NewObjectCoordinator(store.Objects(), store.Versions()),
		coordinator.NewPolicyCoordinator(policy, store.Policies()),
		coordinator.NewVersionCoordinator(clock, store.Versions()),
		coordinator.NewWorkerScheduler(),
		coordinator.NewMemoryCoordinator(store.Objects()),
		coordinator.NewIndexCoordinator(store.Segments(), store.Indexes()),
		coordinator.NewShardCoordinator(1),
		coordinator.NewQueryCoordinator(planner, policy),
	)
	cache := evidence.NewCache(16)
	runtime := worker.CreateRuntime(
		wal, bus, plane, coord, policy, planner, materialization.NewService(),
		materialization.NewPreComputeService(cache), evidence.NewAssembler(), cache,
		nil, nil, nodeManager, store, tieredObjects,
	)
	gateway := access.NewGateway(coord, runtime, store, nil, nil)
	t.Cleanup(func() {
		gateway.Shutdown()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runtime.ShutdownConsistency(ctx)
	})
	server := &APIServer{Gateway: gateway}

	event := schemas.Event{
		Identity:  schemas.EventIdentity{EventID: "grpc-event", WorkspaceID: "workspace"},
		Actor:     schemas.EventActor{AgentID: "agent", SessionID: "session"},
		EventInfo: schemas.EventDescriptor{EventType: "user_message"},
		Access:    schemas.EventAccess{Consistency: "eventual"},
		Payload:   map[string]any{"text": "gRPC consistency acknowledgement"},
	}
	rawEvent, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	response, err := server.IngestEvent(context.Background(), &plasmodv1.IngestEventRequest{
		EventJson: string(rawEvent),
	})
	if err != nil {
		t.Fatalf("IngestEvent: %v", err)
	}
	var ack map[string]any
	if err := json.Unmarshal([]byte(response.GetAckJson()), &ack); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if ack["consistency_mode"] != string(consistency.EventualVisibility) || ack["visibility_status"] != "pending" {
		t.Fatalf("consistency acknowledgement lost across gRPC: %+v", ack)
	}
	if lsn, ok := ack["lsn"].(float64); !ok || lsn <= 0 {
		t.Fatalf("ack LSN missing: %+v", ack)
	}
}

func TestFlatWireFloat32RoundTripLittleEndian(t *testing.T) {
	values := []float32{1, -2.5, 3.25}
	payload := float32sToLittleEndianBytes(values)
	if got, want := len(payload), len(values)*4; got != want {
		t.Fatalf("payload len=%d want %d", got, want)
	}
	if got := binary.LittleEndian.Uint32(payload[:4]); got != math.Float32bits(values[0]) {
		t.Fatalf("first bits=%x want %x", got, math.Float32bits(values[0]))
	}
	roundTrip, err := float32sFromLittleEndianBytes(payload, len(values))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	for i := range values {
		if roundTrip[i] != values[i] {
			t.Fatalf("value[%d]=%v want %v", i, roundTrip[i], values[i])
		}
	}
}

func TestFlatWireInt64LittleEndian(t *testing.T) {
	values := []int64{1, -1, 42}
	payload := int64sToLittleEndianBytes(values)
	if got, want := len(payload), len(values)*8; got != want {
		t.Fatalf("payload len=%d want %d", got, want)
	}
	if got := int64(binary.LittleEndian.Uint64(payload[8:16])); got != -1 {
		t.Fatalf("second value=%d want -1", got)
	}
}

func TestFlatWireRejectsWrongLength(t *testing.T) {
	_, err := float32sFromLittleEndianBytes([]byte{0, 0, 0}, 1)
	if err == nil {
		t.Fatal("expected length error")
	}
}

func TestServiceDescriptorRegistersFlatDataplaneRPCs(t *testing.T) {
	want := map[string]bool{
		"IngestVectorsFlat": false,
		"QueryBatchFlat":    false,
	}
	for _, method := range plasmodv1.PlasmodAPIService_ServiceDesc.Methods {
		if _, ok := want[method.MethodName]; ok {
			want[method.MethodName] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("service descriptor missing %s", name)
		}
	}
}
