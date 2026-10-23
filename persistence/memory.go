package persistence

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// MemoryStore keeps snapshots in memory. It is safe for concurrent use.
type MemoryStore struct {
	mu        sync.Mutex
	snapshots map[string][]byte
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{snapshots: map[string][]byte{}}
}

func (s *MemoryStore) LoadSnapshot(_ context.Context, docID string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.snapshots[docID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, docID)
	}
	return slices.Clone(data), nil
}

func (s *MemoryStore) SaveSnapshot(_ context.Context, docID string, snapshot []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[docID] = slices.Clone(snapshot)
	return nil
}
