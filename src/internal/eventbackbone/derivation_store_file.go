package eventbackbone

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type DerivationStore interface {
	Append(entry DerivationEntry) error
	Load() ([]DerivationEntry, error)
}

type FileDerivationStore struct {
	path string
	mu   sync.Mutex
}

func NewFileDerivationStore(path string) *FileDerivationStore {
	return &FileDerivationStore{path: path}
}

func (s *FileDerivationStore) Append(entry DerivationEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *FileDerivationStore) Load() ([]DerivationEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	out := make([]DerivationEntry, 0)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e DerivationEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
