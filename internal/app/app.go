package app

import (
	"context"
	"errors"

	"github.com/hexxla/hexxladb/internal/domain"
	"github.com/hexxla/hexxladb/internal/record"
)

// ErrNoStorage is returned when a use case needs persistence but [Service.Storage] was never wired.
var ErrNoStorage = errors.New("app: storage port not configured")

// Service orchestrates use cases and outbound ports. Wire concrete adapters in cmd only.
type Service struct {
	// Storage is optional; nil when the process runs without a database (e.g. no HEXXLA_DB_PATH).
	Storage domain.Storage
}

// New constructs the application service with no outbound adapters.
func New() *Service {
	return &Service{}
}

// NewWithStorage constructs the service with persistence wired (hexagonal outbound port).
func NewWithStorage(st domain.Storage) *Service {
	return &Service{Storage: st}
}

// PutCell persists a cell through the outbound [domain.Storage] port (product-layer orchestration entrypoint).
func (s *Service) PutCell(ctx context.Context, rec record.CellRecord) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.PutCell(ctx, rec)
}

// AscendCellsByTag scans cells by tag via [domain.Storage].
func (s *Service) AscendCellsByTag(ctx context.Context, tag string, fn func(record.CellRecord) bool) error {
	if s == nil || s.Storage == nil {
		return ErrNoStorage
	}
	return s.Storage.AscendCellsByTag(ctx, tag, fn)
}
