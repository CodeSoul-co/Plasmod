package consistency

import (
	"bytes"
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

// FileCheckpoint persists checkpoints as an append-only JSON journal. Each
// record is synced before Save returns, while a torn tail is ignored on load.
type FileCheckpoint struct {
	mu              sync.Mutex
	path            string
	loaded          bool
	lsn             int64
	exists          bool
	fileSize        int64
	retryWrite      bool
	dirSyncPending  bool
	tornTail        bool
	tornTailOffset  int64
	pathVersion     uint64
	maxJournalBytes int64
}

const defaultCheckpointJournalMaxBytes int64 = 64 << 20

type fileCheckpointPathState struct {
	mu      sync.Mutex
	version uint64
}

var fileCheckpointPathLocks sync.Map

func NewFileCheckpoint(path string) *FileCheckpoint {
	return &FileCheckpoint{
		path:            filepath.Clean(path),
		maxJournalBytes: defaultCheckpointJournalMaxBytes,
	}
}

func (s *FileCheckpoint) Load() (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pathState := checkpointPathState(s.path)
	pathState.mu.Lock()
	defer pathState.mu.Unlock()
	return s.refreshLocked(pathState)
}

func checkpointPathState(path string) *fileCheckpointPathState {
	state, _ := fileCheckpointPathLocks.LoadOrStore(path, &fileCheckpointPathState{})
	return state.(*fileCheckpointPathState)
}

func (s *FileCheckpoint) refreshLocked(pathState *fileCheckpointPathState) (int64, bool, error) {
	if s.loaded && s.pathVersion == pathState.version {
		return s.lsn, s.exists, nil
	}
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.loaded = true
		s.lsn = 0
		s.exists = false
		s.fileSize = 0
		s.retryWrite = false
		s.tornTail = false
		s.tornTailOffset = 0
		s.pathVersion = pathState.version
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	lsn, exists, err := decodeCheckpointJournal(b)
	if err != nil {
		return 0, false, err
	}
	s.loaded = true
	s.lsn = lsn
	s.exists = exists
	s.fileSize = int64(len(b))
	s.retryWrite = false
	s.tornTailOffset, s.tornTail = tornCheckpointTail(b)
	s.pathVersion = pathState.version
	return lsn, exists, nil
}

func tornCheckpointTail(data []byte) (int64, bool) {
	if len(data) == 0 || bytes.HasSuffix(data, []byte{'\n'}) {
		return 0, false
	}
	start := bytes.LastIndexByte(data, '\n') + 1
	line := bytes.TrimSpace(data[start:])
	if len(line) == 0 {
		return 0, false
	}
	var payload checkpointPayload
	if err := json.Unmarshal(line, &payload); err == nil {
		return 0, false
	}
	return int64(start), true
}

func decodeCheckpointJournal(data []byte) (int64, bool, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return 0, false, nil
	}
	var legacy checkpointPayload
	if err := json.Unmarshal(trimmed, &legacy); err == nil {
		if legacy.VisibleLSN < 0 {
			return 0, false, fmt.Errorf("invalid consistency checkpoint LSN %d", legacy.VisibleLSN)
		}
		return legacy.VisibleLSN, true, nil
	}

	var (
		lastLSN int64
		found   bool
	)
	lines := bytes.Split(data, []byte{'\n'})
	terminated := bytes.HasSuffix(data, []byte{'\n'})
	for index, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var payload checkpointPayload
		if err := json.Unmarshal(line, &payload); err != nil {
			if index == len(lines)-1 && !terminated {
				continue
			}
			return 0, false, fmt.Errorf(
				"decode consistency checkpoint record %d: %w",
				index+1,
				err,
			)
		}
		if payload.VisibleLSN < 0 {
			return 0, false, fmt.Errorf("invalid consistency checkpoint LSN %d", payload.VisibleLSN)
		}
		if found && payload.VisibleLSN < lastLSN {
			return 0, false, fmt.Errorf(
				"checkpoint journal regression: current=%d next=%d",
				lastLSN,
				payload.VisibleLSN,
			)
		}
		lastLSN = payload.VisibleLSN
		found = true
	}
	if !found {
		return 0, false, fmt.Errorf("decode consistency checkpoint: no complete record")
	}
	return lastLSN, true, nil
}

func (s *FileCheckpoint) Save(lsn int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pathState := checkpointPathState(s.path)
	pathState.mu.Lock()
	defer pathState.mu.Unlock()
	if lsn < 0 {
		return fmt.Errorf("invalid consistency checkpoint LSN %d", lsn)
	}
	current, exists, err := s.refreshLocked(pathState)
	if err != nil {
		return err
	}
	if exists && lsn < current {
		return fmt.Errorf("checkpoint regression: current=%d next=%d", current, lsn)
	}
	if s.dirSyncPending {
		if err := syncDirectory(filepath.Dir(s.path)); err != nil {
			return err
		}
		s.dirSyncPending = false
		s.retryWrite = false
	}
	if exists && lsn == current && !s.retryWrite {
		return nil
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if s.tornTail {
		if err := os.Truncate(s.path, s.tornTailOffset); err != nil {
			return fmt.Errorf("repair torn consistency checkpoint tail: %w", err)
		}
		s.fileSize = s.tornTailOffset
		s.tornTail = false
		pathState.version++
		s.pathVersion = pathState.version
	}
	record, err := json.Marshal(checkpointPayload{VisibleLSN: lsn})
	if err != nil {
		return err
	}
	record = append([]byte{'\n'}, record...)
	record = append(record, '\n')
	if s.maxJournalBytes > 0 && s.fileSize+int64(len(record)) > s.maxJournalBytes {
		size, err := replaceCheckpointFile(s.path, dir, lsn)
		if err != nil {
			return err
		}
		pathState.version++
		s.pathVersion = pathState.version
		s.loaded = true
		s.lsn = lsn
		s.exists = true
		s.fileSize = size
		s.retryWrite = true
		s.dirSyncPending = true
		s.tornTail = false
		s.tornTailOffset = 0
		if err := syncDirectory(dir); err != nil {
			return err
		}
		s.dirSyncPending = false
		s.retryWrite = false
		return nil
	}

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	closeWithError := func(cause error) error {
		if closeErr := f.Close(); cause == nil {
			return closeErr
		}
		return cause
	}
	written, err := f.Write(record)
	if written > 0 {
		s.fileSize += int64(written)
		pathState.version++
		s.pathVersion = pathState.version
	}
	if err != nil {
		s.loaded = false
		return closeWithError(err)
	}
	s.retryWrite = true
	if err := f.Sync(); err != nil {
		return closeWithError(err)
	}
	s.loaded = true
	s.lsn = lsn
	s.exists = true
	if !exists {
		s.dirSyncPending = true
	}
	if err := closeWithError(nil); err != nil {
		return err
	}
	if s.dirSyncPending {
		if err := syncDirectory(dir); err != nil {
			return err
		}
		s.dirSyncPending = false
	}
	s.retryWrite = false
	return nil
}

func (s *FileCheckpoint) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pathState := checkpointPathState(s.path)
	pathState.mu.Lock()
	defer pathState.mu.Unlock()
	err := os.Remove(s.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err := syncDirectory(filepath.Dir(s.path)); err != nil {
			return err
		}
	}
	pathState.version++
	s.loaded = true
	s.lsn = 0
	s.exists = false
	s.fileSize = 0
	s.retryWrite = false
	s.dirSyncPending = false
	s.tornTail = false
	s.tornTailOffset = 0
	s.pathVersion = pathState.version
	return nil
}

func replaceCheckpointFile(path, dir string, lsn int64) (int64, error) {
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".compact-*")
	if err != nil {
		return 0, err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := json.NewEncoder(tmp).Encode(checkpointPayload{VisibleLSN: lsn}); err != nil {
		cleanup()
		return 0, err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return 0, err
	}
	info, err := tmp.Stat()
	if err != nil {
		cleanup()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return 0, err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return 0, err
	}
	return info.Size(), nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}
