// Package index encodes logical storage keys (prefixes, PackedCoord byte order) for
// HexxlaDB. See docs/hexxladb/HEXXLA_DB.md Storage Layout and internal/engine/ORDERED_STORE.md.
//
// Secondary seam index (M7): keys seam-by-cells/<packed_lo>/<packed_hi>/<ulid> where each
// packed segment is 16 bytes (Hi then Lo, big-endian uint64s per word) matching [CellKey]
// suffix order, endpoints in canonical min/max order, and ULID is 26 Crockford ASCII chars.
package index
