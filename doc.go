// Package hexxladb is the stable public API for the embedded HexxlaDB engine.
// The specification is in docs/hexxladb/HEXXLA_DB.md. Implementation packages
// under internal/ are private to this module.
//
// Exported entrypoints:
//   - [Open], [Options], [DB], [DB.Close] — engine shell (M3); durability in [internal/engine].
//   - [DB.View], [DB.Update], [DB.Batch], [Tx] — Bolt-style transactions (M5); [DB.Batch] is an alias for [DB.Update]; see docs/hexxladb/TX.md.
//   - [Tx.Get], [Tx.Put], [Tx.AscendRange] — byte-key ordered store; M6 adds [Tx.PutCell], [Tx.GetCell],
//     [Tx.WalkRing], [Tx.PutSeam], [Tx.FindSeams], [Tx.LoadContext], [Tx.ResolveSeam] (see [primitives.go]);
//     Phase A adds [Tx.PutFacet], [Tx.GetFacet], [Tx.AscendFacetsForCell], [Tx.PutEdge], [Tx.GetEdge], [Tx.AscendEdgesFrom] ([facets_edges.go]);
//     Phase B adds [Tx.MarkConflict], [Tx.UpdateFacet], [Tx.LinkCells] (spec-named sugar; see docs/hexxladb/TX.md).
//     Phase C adds [record.ValidAt], [Tx.WalkRingAt], [Tx.LoadContextAt], [Tx.WalkRingFacets] (validity read filters + facet_mask ring walks; not MVCC).
//     Phase D adds [Tx.AscendCellsBySource], [Tx.AscendCellsInTimeBucket] and maintains source/ + time/ keys from [Tx.PutCell] ([cell_secondary.go]).
//   - [ErrCorruptDatabase], [ErrDatabaseClosed], [ErrTxReadOnly], [ErrNilCallback], [ErrNotImplemented], [ErrClosed],
//     [ErrSeamNotFound], [ErrSeamEndpointMismatch] (M7 seam dual-write), [ErrInvalidArgument],
//     [ErrEncryptionKeyRequired], [ErrDatabaseNotEncrypted], [ErrEncryptionOptions] (M9 optional at-rest encryption; see [docs/hexxladb/ENCRYPTION.md]),
//     [ErrCellNotFound], [ErrFacetDerivationMismatch] (Phase B facet update rules)
//
// Lattice types ([Coord], [PackedCoord], [Pack], [Unpack], [Ring], [WalkRings]) are
// re-exported from internal/lattice; see docs/hexxladb/HEXXLA.md and
// internal/lattice/PACKED_COORD.md for geometry and key layout.
//
// # Embedding in your program
//
// Follow the same wiring pattern as cmd/hexxladb/main.go in this repository:
// configuration, structured logging, [Open] with [DB.Close], inject dependencies
// into your application layer. This repository’s example command exits after
// wiring; long-running services add their own servers and shutdown logic.
// Outbound adapters should call only types and functions exported from package
// hexxladb, not internal/engine (see docs/context/HEXAGONAL_ARCHITECTURE.md).
//
// MVCC / as_of (future): see docs/hexxladb/MVCC_DESIGN.md. Phase E1 experiments live in internal/mvccspike (non-production).
package hexxladb
