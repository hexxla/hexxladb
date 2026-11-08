package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/hexxla/hexxladb/examples/ports_adapters/internal/domain"
)

// Service validates and runs the contradiction workflow.
type Service struct {
	store domain.ConflictStore
}

// New wires the service to its required contradiction store.
func New(store domain.ConflictStore) *Service {
	return &Service{store: store}
}

// Record validates the two observations and records their contradiction.
func (s *Service) Record(ctx context.Context, left, right domain.Observation, reason string) (string, error) {
	if left.At == right.At {
		return "", fmt.Errorf("%w: observations need distinct positions", domain.ErrInvalidInput)
	}
	if strings.TrimSpace(left.Text) == "" || strings.TrimSpace(right.Text) == "" {
		return "", fmt.Errorf("%w: both observations need text", domain.ErrInvalidInput)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", fmt.Errorf("%w: reason is required", domain.ErrInvalidInput)
	}
	return s.store.RecordConflict(ctx, left, right, reason)
}

// Resolve validates and saves an operator's resolution note.
func (s *Service) Resolve(ctx context.Context, id, note string) error {
	id, note = strings.TrimSpace(id), strings.TrimSpace(note)
	if id == "" || note == "" {
		return fmt.Errorf("%w: conflict ID and resolution note are required", domain.ErrInvalidInput)
	}
	return s.store.ResolveConflict(ctx, id, note)
}
