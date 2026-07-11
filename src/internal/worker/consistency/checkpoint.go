package consistency

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// CheckpointStore persists the highest contiguous visible WAL LSN.
type CheckpointStore interface {
	Load() (lsn int64, exists bool, err error)
	Save(lsn int64) error
	Reset() error
}

// MemoryCheckpoint stores a checkpoint for an ephemeral runtime.
type MemoryCheckpoint struct {
	mu     sync.Mutex
	lsn    int64
	exists bool
}

func NewMemoryCheckpoint() *MemoryCheckpoint {
	return &MemoryCheckpoint{}
}

func (s *MemoryCheckpoint) Load() (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lsn, s.exists, nil
}

func (s *MemoryCheckpoint) Save(lsn int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exists && lsn < s.lsn {
		return fmt.Errorf("checkpoint regression: current=%d next=%d", s.lsn, lsn)
	}
	s.lsn = lsn
	s.exists = true
	return nil
}

func (s *MemoryCheckpoint) Reset() error {
	s.mu.Lock()
	s.lsn = 0
	s.exists = false
	s.mu.Unlock()
	return nil
}

type checkpointPayload struct {
	VisibleLSN int64 `json:"visible_lsn"`
}

// FileCheckpoint atomically persists a checkpoint as JSON.
type FileCheckpoint struct {
	mu   sync.Mutex
	path string
}

func NewFileCheckpoint(path string) *FileCheckpoint {
	return &FileCheckpoint{path: filepath.Clean(path)}
}

func (s *FileCheckpoint) Load() (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *FileCheckpoint) loadLocked() (int64, bool, error) {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	var payload checkpointPayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return 0, false, fmt.Errorf("decode consistency checkpoint: %w", err)
	}
	if payload.VisibleLSN < 0 {
		return 0, false, fmt.Errorf("invalid consistency checkpoint LSN %d", payload.VisibleLSN)
	}
	return payload.VisibleLSN, true, nil
}

func (s *FileCheckpoint) Save(lsn int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, exists, err := s.loadLocked(); err != nil {
		return err
	} else if exists && lsn < current {
		return fmt.Errorf("checkpoint regression: current=%d next=%d", current, lsn)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := json.NewEncoder(tmp).Encode(checkpointPayload{VisibleLSN: lsn}); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}

func (s *FileCheckpoint) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
