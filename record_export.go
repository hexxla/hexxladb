package hexxladb

import (
	"github.com/hexxla/hexxladb/internal/record"
)

// Public type aliases for internal record types.
// These allow external packages (e.g., Mosaic) to work with wire formats
// without importing internal packages.

// ProvenanceWire is provenance stored in v1 payloads (times as Unix nanoseconds UTC).
// Re-exported from internal/record for public API access.
type ProvenanceWire = record.ProvenanceWire

// ValidityWire is an optional validity window (nil = open-ended on that side).
// Re-exported from internal/record for public API access.
type ValidityWire = record.ValidityWire

// CellRecord is the v1 wire shape for cell/<packed_coord> blobs.
// Re-exported from internal/record for public API access.
// Used by LoadContextByEdges and LoadContextLOD for direct cell access.
type CellRecord = record.CellRecord
