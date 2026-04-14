package app

import "github.com/hexxla/hexxladb/internal/domain"

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
