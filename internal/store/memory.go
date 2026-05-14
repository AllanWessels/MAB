package store

import (
	"context"
	"sync"

	"mab/internal/algo"
)

// Memory is an in-process Store used by tests and as a fallback when no
// DynamoDB endpoint is configured.
type Memory struct {
	mu    sync.Mutex
	items map[string]*algo.State
}

func NewMemory() *Memory {
	return &Memory{items: map[string]*algo.State{}}
}

func (m *Memory) Load(_ context.Context, id string) (*algo.State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.items[id]; ok {
		return cloneState(s), nil
	}
	return algo.NewState(), nil
}

func (m *Memory) Save(_ context.Context, id string, s *algo.State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[id] = cloneState(s)
	return nil
}

func cloneState(s *algo.State) *algo.State {
	c := algo.NewState()
	c.TotalPulls = s.TotalPulls
	for k, v := range s.Counts {
		c.Counts[k] = v
	}
	for k, v := range s.Sums {
		c.Sums[k] = v
	}
	return c
}
