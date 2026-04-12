package memory

import (
	"context"
	"slices"
	"sync"
)

// Store is a thread-safe in-memory [app.TextStore] for demos and fast tests.
type Store struct {
	mu    sync.Mutex
	texts []string
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{}
}

// Save implements [app.TextStore].
func (s *Store) Save(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.texts = append(s.texts, text)
	return nil
}

// List implements [app.TextStore].
func (s *Store) List(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.texts), nil
}
