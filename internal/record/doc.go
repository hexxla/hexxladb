// Package record implements versioned binary envelopes and v1 codecs for Cell, Facet,
// Edge, and Seam blobs stored by the engine (see docs/hexxladb/HEXXLA_DB.md).
//
// Wire layout is locked in FORMAT.md; bump format_version and engine compatibility
// story if payloads change incompatibly.
//
// This package must not import adapters; it may import internal/lattice for PackedCoord.
package record
