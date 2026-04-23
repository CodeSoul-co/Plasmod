package access

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"plasmod/src/internal/metrics"
)

const (
	hardDeleteStateQueued    = "queued"
	hardDeleteStateRunning   = "running"
	hardDeleteStateCompleted = "completed"
	hardDeleteStateFailed    = "failed"
	hardDeleteStateCancelled = "cancelled"
)

type hardDeleteTask struct {
	TaskID          string   `json:"task_id"`
	WorkspaceID     string   `json:"workspace_id"`
	DatasetName     string   `json:"dataset_name,omitempty"`
	MemoryIDs       []string `json:"memory_ids"`
	Processed       int      `json:"processed"`
	Failed          int      `json:"failed"`
	State           string   `json:"state"`
	Error           string   `json:"error,omitempty"`
	CurrentBatch    int      `json:"current_batch_size"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
	StartedAt       string   `json:"started_at,omitempty"`
	CompletedAt     string   `json:"completed_at,omitempty"`
	IdempotencyKey  string   `json:"idempotency_key,omitempty"`
	PurgeBackend    string   `json:"purge_backend,omitempty"`
	Workers         int      `json:"workers,omitempty"`
	BatchSize       int      `json:"batch_size,omitempty"`
	DeleteObjectMs  float64  `json:"delete_object_ms,omitempty"`
	DeleteAuditMs   float64  `json:"delete_audit_ms,omitempty"`
	DeleteOutboxMs  float64  `json:"delete_outbox_ms,omitempty"`
}

type hardDeleteBatchStats struct {
	DeleteObjectNs int64
	DeleteAuditNs  int64
	DeleteOutboxNs int64
	Workers        int
	BatchSize      int
}

type hardDeleteManager struct {
	mu    sync.Mutex
	path  string
	workers int
	batchMu sync.Mutex
	stopCh  chan struct{}
	tasks map[string]*hardDeleteTask
}

type hardDeleteSnapshot struct {
	Tasks []*hardDeleteTask `json:"tasks"`
}

func newHardDeleteManagerFromEnv() *hardDeleteManager {
	path := strings.TrimSpace(os.Getenv("PLASMOD_HARD_DELETE_QUEUE_FILE"))
	if path == "" {
		dataDir := strings.TrimSpace(os.Getenv("PLASMOD_DATA_DIR"))
		if dataDir == "" {
			path = ".out/hard_delete_tasks.json"
		} else {
			path = filepath.Join(dataDir, "hard_delete_tasks.json")
		}
	}
	m := &hardDeleteManager{
		path:  path,
		tasks: map[string]*hardDeleteTask{},
		stopCh: make(chan struct{}),
	}
	_ = m.load()
	return m
}

func (m *hardDeleteManager) enqueue(task *hardDeleteTask) bool {
	if m == nil || task == nil {
		return false
	}
	if strings.TrimSpace(task.TaskID) == "" || strings.TrimSpace(task.WorkspaceID) == "" || len(task.MemoryIDs) == 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(task.IdempotencyKey) != "" {
		for _, t := range m.tasks {
			if t.IdempotencyKey == task.IdempotencyKey && (t.State == hardDeleteStateQueued || t.State == hardDeleteStateRunning) {
				return false
			}
		}
	}
	m.tasks[task.TaskID] = task
	return m.persistLocked() == nil
}

func (m *hardDeleteManager) getActiveByIdempotencyKey(key string) (*hardDeleteTask, bool) {
	if m == nil {
		return nil, false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t.IdempotencyKey != key {
			continue
		}
		if t.State != hardDeleteStateQueued && t.State != hardDeleteStateRunning {
			continue
		}
		cp := *t
		cp.MemoryIDs = append([]string(nil), t.MemoryIDs...)
		return &cp, true
	}
	return nil, false
}

func (m *hardDeleteManager) get(taskID string) (*hardDeleteTask, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return nil, false
	}
	cp := *t
	cp.MemoryIDs = append([]string(nil), t.MemoryIDs...)
	return &cp, true
}

func (m *hardDeleteManager) run(stopCh <-chan struct{}, ctx context.Context, process func(task *hardDeleteTask, batchSize int) (processed, failed int, done bool, stats hardDeleteBatchStats, err error)) {
	for {
		select {
		case <-stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}
		task := m.nextRunnable()
		if task == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		batchSize := m.recommendBatchSize(task.CurrentBatch)
		processed, failed, done, stats, err := process(task, batchSize)
		m.applyResult(task.TaskID, processed, failed, batchSize, done, stats, err)
	}
}

func (m *hardDeleteManager) nextRunnable() *hardDeleteTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t.State != hardDeleteStateQueued && t.State != hardDeleteStateRunning {
			continue
		}
		now := time.Now().UTC().Format(time.RFC3339)
		if t.State == hardDeleteStateQueued {
			if !hardDeleteTransitionAllowed(t.State, hardDeleteStateRunning) {
				continue
			}
			t.State = hardDeleteStateRunning
			t.StartedAt = now
		}
		t.UpdatedAt = now
		_ = m.persistLocked()
		cp := *t
		cp.MemoryIDs = append([]string(nil), t.MemoryIDs...)
		return &cp
	}
	return nil
}

func (m *hardDeleteManager) applyResult(taskID string, processed, failed, batchSize int, done bool, stats hardDeleteBatchStats, runErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	t.Processed += processed
	t.Failed += failed
	t.CurrentBatch = batchSize
	if stats.Workers > 0 {
		t.Workers = stats.Workers
	}
	if stats.BatchSize > 0 {
		t.BatchSize = stats.BatchSize
	}
	t.DeleteObjectMs += float64(stats.DeleteObjectNs) / float64(time.Millisecond)
	t.DeleteAuditMs += float64(stats.DeleteAuditNs) / float64(time.Millisecond)
	t.DeleteOutboxMs += float64(stats.DeleteOutboxNs) / float64(time.Millisecond)
	t.UpdatedAt = now
	if runErr != nil {
		if hardDeleteTransitionAllowed(t.State, hardDeleteStateFailed) {
			t.State = hardDeleteStateFailed
			t.Error = runErr.Error()
			t.CompletedAt = now
		}
		_ = m.persistLocked()
		return
	}
	if done {
		if hardDeleteTransitionAllowed(t.State, hardDeleteStateCompleted) {
			t.State = hardDeleteStateCompleted
			t.CompletedAt = now
		}
	}
	_ = m.persistLocked()
}

func (m *hardDeleteManager) recommendBatchSize(prev int) int {
	if prev <= 0 {
		prev = 128
	}
	snap := metrics.Global().Snapshot()
	pressureHigh := snap.ConcurrentQueries > 4 || snap.GoAllocBytes > 2*1024*1024*1024
	if pressureHigh {
		prev = prev / 2
		if prev < 32 {
			prev = 32
		}
		return prev
	}
	prev += 32
	if prev > 1024 {
		prev = 1024
	}
	return prev
}

func hardDeleteTransitionAllowed(from, to string) bool {
	switch from {
	case hardDeleteStateQueued:
		return to == hardDeleteStateRunning || to == hardDeleteStateCancelled
	case hardDeleteStateRunning:
		return to == hardDeleteStateCompleted || to == hardDeleteStateFailed || to == hardDeleteStateCancelled || to == hardDeleteStateRunning
	default:
		return false
	}
}

func (m *hardDeleteManager) load() error {
	if m == nil || strings.TrimSpace(m.path) == "" {
		return nil
	}
	b, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var snap hardDeleteSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return err
	}
	for _, t := range snap.Tasks {
		if t == nil || strings.TrimSpace(t.TaskID) == "" {
			continue
		}
		// Crash-safe recovery: queued/running tasks continue from pending cursor.
		if t.State == hardDeleteStateRunning {
			t.State = hardDeleteStateQueued
		}
		m.tasks[t.TaskID] = t
	}
	return nil
}

func (m *hardDeleteManager) persistLocked() error {
	if m == nil || strings.TrimSpace(m.path) == "" {
		return nil
	}
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out := hardDeleteSnapshot{Tasks: make([]*hardDeleteTask, 0, len(m.tasks))}
	for _, t := range m.tasks {
		out.Tasks = append(out.Tasks, t)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}

	// Crash-safe persistence: write to a temp file first, fsync, then atomically rename.
	tmpFile, err := os.CreateTemp(dir, "hard_delete_tasks_*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := func() {
		_ = os.Remove(tmpPath)
	}
	if _, err := tmpFile.Write(b); err != nil {
		_ = tmpFile.Close()
		cleanupTmp()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		cleanupTmp()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		cleanupTmp()
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		cleanupTmp()
		return err
	}
	if err := os.Rename(tmpPath, m.path); err != nil {
		cleanupTmp()
		return err
	}
	return nil
}
