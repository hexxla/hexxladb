// Package lattice implements pure hex lattice types and geometry (stdlib only).
//
// Normative geometry and ring / context ordering: docs/hexxladb/HEXXLA.md.
// The locked v1 PackedCoord layout (zigzag widths, Morton order, Hi/Lo map) is
// documented in PACKED_COORD.md (this directory) and docs/hexxladb/HEXXLA_DB.md.
//
// This package must not perform I/O or import adapters.
package lattice
