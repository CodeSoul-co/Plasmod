package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"plasmod/src/internal/dataplane"
	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/worker/consistency"
)

type gatedDataPlane struct {
	inner   dataplane.DataPlane
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGatedDataPlane(inner dataplane.DataPlane) *gatedDataPlane {
	return &gatedDataPlane{
		inner: inner, started: make(chan struct{}), release: make(chan struct{}),
	}
}

func (p *gatedDataPlane) Ingest(record dataplane.IngestRecord) error {
	p.once.Do(func() { close(p.started) })
	<-p.release
	return p.inner.Ingest(record)
}

func (p *gatedDataPlane) Search(input dataplane.SearchInput) dataplane.SearchOutput {
	return p.inner.Search(input)
}

func (p *gatedDataPlane) Flush() error { return p.inner.Flush() }

type gatedEmbedder struct {
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
	releaseOnce sync.Once
}

func newGatedEmbedder() *gatedEmbedder {
	return &gatedEmbedder{started: make(chan struct{}), release: make(chan struct{})}
}

func (e *gatedEmbedder) Generate(string) ([]float32, error) {
	e.startedOnce.Do(func() { close(e.started) })
	<-e.release
	return make([]float32, e.Dim()), nil
}

func (e *gatedEmbedder) Dim() int { return 8 }

func (e *gatedEmbedder) Reset() {}

func (e *gatedEmbedder) Release() { e.releaseOnce.Do(func() { close(e.release) }) }

func configureRuntimeConsistency(t *testing.T, runtime *Runtime, mode consistency.Mode) {
	t.Helper()
	cfg := consistency.DefaultConfig()
	cfg.DefaultMode = mode
	cfg.QueueSize = 16
	cfg.Workers = 1
	cfg.BoundedMaxLag = 20 * time.Millisecond
	cfg.QueryWaitTimeout = time.Second
	if err := runtime.ConfigureConsistency(cfg, nil); err != nil {
		t.Fatalf("ConfigureConsistency: %v", err)
	}
	if err := runtime.StartConsistency(context.Background()); err != nil {
		t.Fatalf("StartConsistency: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runtime.ShutdownConsistency(ctx)
	})
}

func consistencyTestEvent(id string) schemas.Event {
	return schemas.Event{
		EventID: id, TenantID: "tenant", WorkspaceID: "workspace",
		AgentID: "agent", SessionID: "session", EventType: "user_message",
		Payload: map[string]any{"text": "runtime consistency payload"},
	}
}

func TestRuntime_ConsistencyEventOverrideBeatsGlobalMode(t *testing.T) {
	runtime := buildTestRuntime(t)
	configureRuntimeConsistency(t, runtime, consistency.EventualVisibility)

	event := consistencyTestEvent("strict-override")
	event.Access.Consistency = "strict"
	ack, err := runtime.SubmitIngestContext(context.Background(), event)
	if err != nil {
		t.Fatalf("SubmitIngestContext: %v", err)
	}
	if ack["consistency_mode"] != string(consistency.StrictVisible) {
		t.Fatalf("consistency mode = %v, want strict override", ack["consistency_mode"])
	}
	if ack["visibility_status"] != "visible" {
		t.Fatalf("visibility status = %v, want visible", ack["visibility_status"])
	}
	if _, ok := runtime.storage.Objects().GetMemory("mem_strict-override"); !ok {
		t.Fatal("strict acknowledgement returned before canonical visibility")
	}
}

func TestRuntime_ConsistencyAsyncAckAndReadOverrides(t *testing.T) {
	for _, mode := range []consistency.Mode{
		consistency.BoundedStaleness,
		consistency.EventualVisibility,
	} {
		t.Run(string(mode), func(t *testing.T) {
			runtime := buildTestRuntime(t)
			gate := newGatedDataPlane(runtime.plane)
			runtime.plane = gate
			configureRuntimeConsistency(t, runtime, mode)

			event := consistencyTestEvent("async-" + string(mode))
			if mode == consistency.BoundedStaleness {
				lagMS := int64(20)
				event.Access.FreshnessSLAMS = &lagMS
			}
			ackCh := make(chan map[string]any, 1)
			errCh := make(chan error, 1)
			go func() {
				ack, err := runtime.SubmitIngestContext(context.Background(), event)
				ackCh <- ack
				errCh <- err
			}()

			var ack map[string]any
			select {
			case ack = <-ackCh:
			case <-time.After(time.Second):
				t.Fatal("asynchronous mode did not acknowledge before projection")
			}
			if err := <-errCh; err != nil {
				t.Fatalf("SubmitIngestContext: %v", err)
			}
			if ack["consistency_mode"] != string(mode) || ack["visibility_status"] != "pending" {
				t.Fatalf("unexpected asynchronous acknowledgement: %+v", ack)
			}
			select {
			case <-gate.started:
			case <-time.After(time.Second):
				t.Fatal("projection did not start")
			}
			memoryID := "mem_" + event.EventID
			if _, ok := runtime.storage.Objects().GetMemory(memoryID); ok {
				t.Fatal("canonical object visible while projection is blocked")
			}

			eventualCtx, cancelEventual := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancelEventual()
			eventualResp, err := runtime.ExecuteQueryContext(eventualCtx, schemas.QueryRequest{
				AccessConsistency: "eventual", TargetObjectIDs: []string{memoryID},
				ResponseMode: schemas.ResponseModeObjectsOnly,
			})
			if err != nil {
				t.Fatalf("eventual query: %v", err)
			}
			if len(eventualResp.Objects) != 0 {
				t.Fatalf("eventual query saw blocked object: %v", eventualResp.Objects)
			}

			strictDone := make(chan schemas.QueryResponse, 1)
			strictErr := make(chan error, 1)
			go func() {
				resp, err := runtime.ExecuteQueryContext(context.Background(), schemas.QueryRequest{
					AccessConsistency: "strict", TargetObjectIDs: []string{memoryID},
					ResponseMode: schemas.ResponseModeObjectsOnly,
				})
				strictDone <- resp
				strictErr <- err
			}()
			select {
			case <-strictDone:
				t.Fatal("strict query returned before projection became visible")
			case <-time.After(50 * time.Millisecond):
			}

			close(gate.release)
			var strictResp schemas.QueryResponse
			select {
			case strictResp = <-strictDone:
			case <-time.After(time.Second):
				t.Fatal("strict query did not resume after projection")
			}
			if err := <-strictErr; err != nil {
				t.Fatalf("strict query: %v", err)
			}
			if len(strictResp.Objects) != 1 || strictResp.Objects[0] != memoryID {
				t.Fatalf("strict query did not see projected object: %v", strictResp.Objects)
			}
		})
	}
}

func TestRuntime_ConsistencyModeAndStatusControls(t *testing.T) {
	runtime := buildTestRuntime(t)
	configureRuntimeConsistency(t, runtime, consistency.StrictVisible)

	mode, err := runtime.SetConsistencyMode("bounded")
	if err != nil {
		t.Fatalf("SetConsistencyMode: %v", err)
	}
	if mode != consistency.BoundedStaleness {
		t.Fatalf("mode = %q, want bounded", mode)
	}
	status := runtime.ConsistencyStatus()
	if status.DefaultMode != consistency.BoundedStaleness || !status.DataPathActive {
		t.Fatalf("unexpected consistency status: %+v", status)
	}
}

func TestRuntime_ConsistencyAdminWipeDrainsAndResetsProjection(t *testing.T) {
	runtime := buildTestRuntime(t)
	embedder := newGatedEmbedder()
	defer embedder.Release()
	tieredPlane, err := dataplane.NewTieredDataPlaneWithEmbedder(runtime.tieredObjects, embedder)
	if err != nil {
		t.Fatalf("NewTieredDataPlaneWithEmbedder: %v", err)
	}
	runtime.plane = tieredPlane
	configureRuntimeConsistency(t, runtime, consistency.EventualVisibility)

	event := consistencyTestEvent("wipe-pending")
	ack, err := runtime.SubmitIngestContext(context.Background(), event)
	if err != nil {
		t.Fatalf("SubmitIngestContext: %v", err)
	}
	if ack["visibility_status"] != "pending" {
		t.Fatalf("ack = %+v, want pending", ack)
	}
	select {
	case <-embedder.started:
	case <-time.After(time.Second):
		t.Fatal("projection did not reach gated embedder")
	}

	type wipeResult struct {
		out map[string]any
		err error
	}
	wipeDone := make(chan wipeResult, 1)
	go func() {
		out, err := runtime.AdminWipeAll(nil, schemas.DefaultAlgorithmConfig())
		wipeDone <- wipeResult{out: out, err: err}
	}()
	select {
	case result := <-wipeDone:
		t.Fatalf("wipe returned while projection was active: out=%v err=%v", result.out, result.err)
	case <-time.After(50 * time.Millisecond):
	}

	embedder.Release()
	var result wipeResult
	select {
	case result = <-wipeDone:
	case <-time.After(time.Second):
		t.Fatal("wipe did not finish after projection drained")
	}
	if result.err != nil {
		t.Fatalf("AdminWipeAll: %v", result.err)
	}
	if _, ok := runtime.storage.Objects().GetMemory("mem_wipe-pending"); ok {
		t.Fatal("old projection rematerialized after wipe")
	}
	search := tieredPlane.Search(dataplane.SearchInput{
		QueryText: "runtime consistency payload", TopK: 5,
		Namespace: "workspace", IncludeGrowing: true,
	})
	if len(search.ObjectIDs) != 0 {
		t.Fatalf("retrieval state survived wipe: %v", search.ObjectIDs)
	}
	status := runtime.ConsistencyStatus()
	if status.DefaultMode != consistency.EventualVisibility || !status.DataPathActive {
		t.Fatalf("wipe did not preserve and resume mode: %+v", status)
	}
	if status.LatestLSN != 0 || status.VisibleWatermark != 0 || status.Pending != 0 {
		t.Fatalf("wipe did not clear projection state: %+v", status)
	}
}

func TestRuntime_ConsistencyAdminWipeDrainsSubscriber(t *testing.T) {
	runtime := buildTestRuntime(t)
	configureRuntimeConsistency(t, runtime, consistency.StrictVisible)

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseHandler()
	subscriber := CreateEventSubscriber(runtime.wal, runtime.nodeManager)
	subscriber.SetPollInterval(time.Millisecond)
	subscriber.AddHandler(func(eventbackbone.WALEntry) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		runtime.storage.Objects().PutMemory(schemas.Memory{MemoryID: "subscriber-old", IsActive: true})
	})
	ctx, cancelSubscriber := context.WithCancel(context.Background())
	defer cancelSubscriber()
	runtime.StartSubscriber(ctx, subscriber)

	if _, err := runtime.SubmitIngestContext(context.Background(), consistencyTestEvent("subscriber-wipe")); err != nil {
		t.Fatalf("SubmitIngestContext: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("subscriber handler did not start")
	}

	type wipeResult struct {
		err error
	}
	wipeDone := make(chan wipeResult, 1)
	go func() {
		_, err := runtime.AdminWipeAll(nil, schemas.DefaultAlgorithmConfig())
		wipeDone <- wipeResult{err: err}
	}()
	select {
	case result := <-wipeDone:
		t.Fatalf("wipe returned while subscriber handler was active: %v", result.err)
	case <-time.After(50 * time.Millisecond):
	}

	releaseHandler()
	select {
	case result := <-wipeDone:
		if result.err != nil {
			t.Fatalf("AdminWipeAll: %v", result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("wipe did not finish after subscriber drained")
	}
	if _, ok := runtime.storage.Objects().GetMemory("subscriber-old"); ok {
		t.Fatal("subscriber rematerialized old state after wipe")
	}
}
