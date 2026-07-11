package worker

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"plasmod/src/internal/metrics"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/worker/consistency"
)

// ConfigureConsistency installs the generic WAL-to-visibility policy before
// the data path starts. Runtime defaults remain strict for embedded callers.
func (r *Runtime) ConfigureConsistency(cfg consistency.Config, watermark consistency.WatermarkAdvancer) error {
	if r == nil {
		return errors.New("runtime is nil")
	}
	r.consistencyMu.Lock()
	defer r.consistencyMu.Unlock()
	if r.consistencyController != nil && r.consistencyController.Status().DataPathActive {
		return errors.New("consistency controller is already active")
	}

	checkpoint := consistency.CheckpointStore(consistency.NewMemoryCheckpoint())
	if strings.TrimSpace(cfg.CheckpointPath) != "" {
		checkpoint = consistency.NewFileCheckpoint(cfg.CheckpointPath)
	}
	controller, err := consistency.NewController(r.wal, watermark, checkpoint, cfg, r.projectWALEntry)
	if err != nil {
		return err
	}
	r.consistencyConfig = cfg
	r.consistencyWatermark = watermark
	r.consistencyController = controller
	return nil
}

func (r *Runtime) ensureConsistencyControllerLocked() error {
	if r.consistencyController != nil {
		return nil
	}
	cfg := r.consistencyConfig
	if cfg.QueueSize == 0 {
		cfg = consistency.DefaultConfig()
	}
	checkpoint := consistency.CheckpointStore(consistency.NewMemoryCheckpoint())
	if strings.TrimSpace(cfg.CheckpointPath) != "" {
		checkpoint = consistency.NewFileCheckpoint(cfg.CheckpointPath)
	}
	controller, err := consistency.NewController(
		r.wal, r.consistencyWatermark, checkpoint, cfg, r.projectWALEntry,
	)
	if err != nil {
		return err
	}
	r.consistencyConfig = cfg
	r.consistencyController = controller
	return nil
}

// StartConsistency activates recovery and asynchronous projection workers.
func (r *Runtime) StartConsistency(ctx context.Context) error {
	if r == nil {
		return errors.New("runtime is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.consistencyMu.Lock()
	defer r.consistencyMu.Unlock()
	if err := r.ensureConsistencyControllerLocked(); err != nil {
		return err
	}
	return r.consistencyController.Start(context.WithoutCancel(ctx))
}

// ShutdownConsistency drains accepted projections and stops background workers.
func (r *Runtime) ShutdownConsistency(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.consistencyMu.Lock()
	controller := r.consistencyController
	cfg := r.consistencyConfig
	r.consistencyMu.Unlock()
	if controller == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && cfg.ShutdownTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.ShutdownTimeout)
		defer cancel()
	}
	return controller.Shutdown(ctx)
}

func (r *Runtime) pauseConsistencyForReset(ctx context.Context) (bool, error) {
	r.consistencyMu.Lock()
	controller := r.consistencyController
	r.consistencyMu.Unlock()
	if controller == nil || !controller.Status().DataPathActive {
		return false, nil
	}
	if err := controller.Pause(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Runtime) resetConsistency() error {
	r.consistencyMu.Lock()
	controller := r.consistencyController
	r.consistencyMu.Unlock()
	if controller == nil {
		return nil
	}
	return controller.Reset()
}

func (r *Runtime) resumeConsistency() {
	r.consistencyMu.Lock()
	controller := r.consistencyController
	r.consistencyMu.Unlock()
	if controller != nil {
		controller.Resume()
	}
}

// SubmitIngestContext validates before WAL acceptance, then delegates mode
// semantics, projection, and acknowledgement to the consistency controller.
func (r *Runtime) SubmitIngestContext(ctx context.Context, ev schemas.Event) (map[string]any, error) {
	if r == nil {
		return nil, errors.New("runtime is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ev = ev.NormalizeDynamicEventV04()
	if strings.TrimSpace(ev.Identity.EventID) == "" {
		return nil, errors.New("event_id is required")
	}
	if err := validateEmbeddingIngestPayload(ev); err != nil {
		return nil, err
	}
	if err := r.nodeManager.DispatchIngestValidation(ev); err != nil {
		return nil, err
	}
	if err := r.StartConsistency(context.Background()); err != nil {
		return nil, err
	}

	r.consistencyMu.Lock()
	controller := r.consistencyController
	r.consistencyMu.Unlock()
	started := time.Now()
	ack, err := controller.Submit(ctx, ev)
	metrics.Global().RecordWriteLatency(time.Since(started))
	return ack, err
}

// SubmitIngest preserves the existing embedded API with a background context.
func (r *Runtime) SubmitIngest(ev schemas.Event) (map[string]any, error) {
	return r.SubmitIngestContext(context.Background(), ev)
}

// ExecuteQueryContext applies the resolved read barrier before the unchanged
// query implementation touches canonical and retrieval state.
func (r *Runtime) ExecuteQueryContext(ctx context.Context, req schemas.QueryRequest) (schemas.QueryResponse, error) {
	if r == nil {
		return schemas.QueryResponse{}, errors.New("runtime is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := r.StartConsistency(context.Background()); err != nil {
		return schemas.QueryResponse{}, err
	}
	r.consistencyMu.Lock()
	controller := r.consistencyController
	r.consistencyMu.Unlock()
	if err := controller.WaitForRead(ctx, req); err != nil {
		return schemas.QueryResponse{}, err
	}
	return r.executeQuery(req), nil
}

// ExecuteQuery preserves the historical no-error API for embedded callers.
func (r *Runtime) ExecuteQuery(req schemas.QueryRequest) schemas.QueryResponse {
	resp, err := r.ExecuteQueryContext(context.Background(), req)
	if err == nil {
		return resp
	}
	log.Printf("[consistency] query visibility wait failed: %v", err)
	return schemas.QueryResponse{
		Objects:     []string{},
		QueryStatus: "visibility_wait_failed",
		QueryHint:   err.Error(),
	}
}

func (r *Runtime) SetConsistencyMode(raw string) (consistency.Mode, error) {
	if r == nil {
		return "", errors.New("runtime is nil")
	}
	r.consistencyMu.Lock()
	defer r.consistencyMu.Unlock()
	if err := r.ensureConsistencyControllerLocked(); err != nil {
		return "", err
	}
	return r.consistencyController.SetDefaultMode(raw)
}

func (r *Runtime) ConsistencyStatus() consistency.ControllerStatus {
	if r == nil {
		return consistency.ControllerStatus{}
	}
	r.consistencyMu.Lock()
	defer r.consistencyMu.Unlock()
	if r.consistencyController != nil {
		return r.consistencyController.Status()
	}
	mode := r.consistencyConfig.DefaultMode
	if mode == "" {
		mode = consistency.StrictVisible
	}
	return consistency.ControllerStatus{
		DefaultMode: mode,
		SupportedModes: []string{
			string(consistency.StrictVisible),
			string(consistency.BoundedStaleness),
			string(consistency.EventualVisibility),
		},
		QueueCapacity: r.consistencyConfig.QueueSize,
	}
}
