package app

import (
	"context"
	"strings"

	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/domain"
	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/domain/hash"
)

// TextStore is a secondary port: persistence for demo text lines (in-memory adapter in this template).
type TextStore interface {
	Save(ctx context.Context, text string) error
	List(ctx context.Context) ([]string, error)
}

// Service orchestrates domain logic and outbound ports.
type Service struct {
	store TextStore
}

// New constructs the application service.
func New(store TextStore) *Service {
	return &Service{store: store}
}

// HashMessage returns the SHA-256 hex digest of message after [strings.TrimSpace].
// Empty or whitespace-only input yields [domain.ErrInvalidInput].
func (*Service) HashMessage(_ context.Context, message string) (string, error) {
	m := strings.TrimSpace(message)
	if m == "" {
		return "", domain.ErrInvalidInput
	}
	return hash.SHA256Hex(m)
}

// StoreText appends trimmed text to the configured store.
// Empty or whitespace-only input yields [domain.ErrInvalidInput].
func (s *Service) StoreText(ctx context.Context, text string) error {
	t := strings.TrimSpace(text)
	if t == "" {
		return domain.ErrInvalidInput
	}
	if len(t) > domain.MaxContentLen {
		return domain.ErrContentTooLarge
	}
	return s.store.Save(ctx, t)
}

// ListMessages returns all stored texts (order preserved).
func (s *Service) ListMessages(ctx context.Context) ([]string, error) {
	return s.store.List(ctx)
}
