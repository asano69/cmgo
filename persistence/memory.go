package persistence

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// MemoryPersistence keeps the state of rooms in memory. It is safe for
// concurrent use.
type MemoryPersistence struct {
	mu     sync.Mutex
	states map[string][]byte
}

func NewMemoryPersistence() *MemoryPersistence {
	return &MemoryPersistence{states: map[string][]byte{}}
}

func (m *MemoryPersistence) Load(_ context.Context, room string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.states[room]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrRoomNotFound, room)
	}
	return slices.Clone(state), nil
}

func (m *MemoryPersistence) Store(_ context.Context, room string, state []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[room] = slices.Clone(state)
	return nil
}
