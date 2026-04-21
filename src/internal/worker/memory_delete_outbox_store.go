package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type memoryDeleteOutboxStore struct {
	mu    sync.Mutex
	path  string
	tasks []memoryDeleteTask
}

type memoryDeleteOutboxSnapshot struct {
	Tasks []memoryDeleteTask `json:"tasks"`
}

func newMemoryDeleteOutboxStoreFromEnv() *memoryDeleteOutboxStore {
	path := strings.TrimSpace(os.Getenv("PLASMOD_ZEP_DELETE_OUTBOX_FILE"))
	if path == "" {
		dataDir := strings.TrimSpace(os.Getenv("PLASMOD_DATA_DIR"))
		if dataDir == "" {
			path = ".out/zep_delete_outbox.json"
		} else {
			path = filepath.Join(dataDir, "zep_delete_outbox.json")
		}
	}
	st := &memoryDeleteOutboxStore{path: path, tasks: make([]memoryDeleteTask, 0)}
	_ = st.load()
	return st
}

func (s *memoryDeleteOutboxStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *memoryDeleteOutboxStore) Pending() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tasks)
}

func (s *memoryDeleteOutboxStore) Enqueue(task memoryDeleteTask) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, task)
	return s.persistLocked()
}

func (s *memoryDeleteOutboxStore) Peek() (memoryDeleteTask, bool) {
	if s == nil {
		return memoryDeleteTask{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tasks) == 0 {
		return memoryDeleteTask{}, false
	}
	return s.tasks[0], true
}

func (s *memoryDeleteOutboxStore) Ack(taskID string) bool {
	if s == nil || strings.TrimSpace(taskID) == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tasks {
		if t.TaskID != taskID {
			continue
		}
		s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
		return s.persistLocked()
	}
	return false
}

func (s *memoryDeleteOutboxStore) MarkFailed(taskID string, errText string) bool {
	if s == nil || strings.TrimSpace(taskID) == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range s.tasks {
		if s.tasks[i].TaskID != taskID {
			continue
		}
		s.tasks[i].Attempts++
		s.tasks[i].LastError = strings.TrimSpace(errText)
		s.tasks[i].UpdatedAt = now
		return s.persistLocked()
	}
	return false
}

func (s *memoryDeleteOutboxStore) load() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil
	}
	if _, err := os.Stat(s.path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	var snap memoryDeleteOutboxSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return err
	}
	s.tasks = snap.Tasks
	return nil
}

func (s *memoryDeleteOutboxStore) persistLocked() bool {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return false
	}
	snap := memoryDeleteOutboxSnapshot{Tasks: s.tasks}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return false
	}
	return os.WriteFile(s.path, b, 0o644) == nil
}
