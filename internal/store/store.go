package store

import (
	"context"

	"mab/internal/algo"
)

// Store persists per-experiment bandit state. Implementations must be safe
// for concurrent calls on different experiment_ids; the server serializes
// reads/writes for a single experiment_id via a per-key mutex.
type Store interface {
	Load(ctx context.Context, experimentID string) (*algo.State, error)
	Save(ctx context.Context, experimentID string, s *algo.State) error
}
