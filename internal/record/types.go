package record

import "github.com/hexxla/hexxladb/internal/lattice"

// ProvenanceWire is provenance stored in v1 payloads (times as Unix nanoseconds UTC).
type ProvenanceWire struct {
	SourceID   string
	Confidence float64
	CreatedAt  int64 // unix nano
	UpdatedAt  int64 // unix nano
}

// ValidityWire is an optional validity window (nil = open-ended on that side).
type ValidityWire struct {
	ValidFrom *int64 // unix nano UTC, or nil
	ValidTo   *int64 // unix nano UTC, or nil
}

// CellRecord is the v1 wire shape for cell/<packed_coord> blobs.
type CellRecord struct {
	Key         lattice.PackedCoord
	RawContent  string
	Provenance  ProvenanceWire
	Validity    ValidityWire
	Tags        []string
	ClusterHint *lattice.PackedCoord
}

// FacetRecord is the v1 wire shape for facet/<packed_coord>/<facet_id> payloads.
type FacetRecord struct {
	Key            lattice.PackedCoord
	FacetID        byte // 0..5
	DerivedContent string
	LastRotated    int64 // unix nano UTC
	DerivationHash [32]byte
}

// EdgeRecord is the v1 wire shape for edge/<from>/<to>/<type> payloads.
type EdgeRecord struct {
	From         lattice.PackedCoord
	To           lattice.PackedCoord
	RelationType string
	Weight       float64
	Provenance   ProvenanceWire
}

// SeamRecord is the v1 wire shape for seam/<ulid> payloads.
type SeamRecord struct {
	ID               string // ULID string (26 Crockford chars)
	CellA            lattice.PackedCoord
	CellB            lattice.PackedCoord
	SeamType         string
	Reason           string
	ConfidenceDelta  float64
	DetectedAt       int64 // unix nano UTC
	ResolutionStatus string
	ResolutionNote   string
	// Validity optional half-open window [ValidFrom, ValidTo); encoded as a trailing suffix on v1 payload when present (see FORMAT.md).
	Validity ValidityWire
	// Provenance optional; SourceID drives seam-source/ secondary keys (see HEXXLA_DB.md). Encoded after Validity when present.
	Provenance ProvenanceWire
}
