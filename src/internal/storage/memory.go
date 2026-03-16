package storage

import (
	"sync"
	"time"
)

type memorySegmentStore struct {
	mu    sync.RWMutex
	items map[string]SegmentRecord
}

func newMemorySegmentStore() *memorySegmentStore {
	return &memorySegmentStore{items: map[string]SegmentRecord{}}
}

func (s *memorySegmentStore) Upsert(record SegmentRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.UpdatedAt = time.Now().UTC()
	key := record.Namespace + ":" + record.SegmentID
	s.items[key] = record
}

func (s *memorySegmentStore) List(namespace string) []SegmentRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SegmentRecord, 0, len(s.items))
	for _, item := range s.items {
		if namespace == "" || item.Namespace == namespace {
			out = append(out, item)
		}
	}
	return out
}

type memoryIndexStore struct {
	mu    sync.RWMutex
	items map[string]IndexRecord
}

func newMemoryIndexStore() *memoryIndexStore {
	return &memoryIndexStore{items: map[string]IndexRecord{}}
}

func (s *memoryIndexStore) Upsert(record IndexRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.UpdatedAt = time.Now().UTC()
	s.items[record.Namespace] = record
}

func (s *memoryIndexStore) List() []IndexRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]IndexRecord, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, item)
	}
	return out
}

type MemoryRuntimeStorage struct {
	segmentStore *memorySegmentStore
	indexStore   *memoryIndexStore
}

func NewMemoryRuntimeStorage() *MemoryRuntimeStorage {
	return &MemoryRuntimeStorage{segmentStore: newMemorySegmentStore(), indexStore: newMemoryIndexStore()}
}

func (s *MemoryRuntimeStorage) Segments() SegmentStore {
	return s.segmentStore
}

func (s *MemoryRuntimeStorage) Indexes() IndexStore {
	return s.indexStore
}
