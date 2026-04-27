// Package hexxladb is the stable public API for the embedded HexxlaDB engine.
//
// Documentation map:
//   - docs/hexxladb/HEXXLA_DB.md — storage layout and key spec (how it works).
//   - docs/hexxladb/API_REFERENCE.md — full exported symbol inventory.
//   - docs/hexxladb/TX.md — transactions, primitives, MVCC temporal semantics.
//   - docs/hexxladb/OPERATIONS.md — embedding, backups, MVCC retention, incident response.
//   - docs/hexxladb/DURABILITY.md, ENCRYPTION.md, CHANGEFEED.md — focused feature refs.
//   - docs/hexxladb/HEXXLA.md — memory model reference (product context).
//   - docs/ROADMAP.md — roadmap, non-goals, spec-vs-code backlog.
//
// Implementation packages under internal/ are private to this module.
//
// Exported entrypoints:
//   - [Open], [Options], [DB], [DB.Close] — engine shell with durable WAL (see docs/hexxladb/DURABILITY.md).
//     Optional logical changefeed ([Options.ChangelogEnabled], [DB.ReadChangelogSince]; see docs/hexxladb/CHANGEFEED.md).
//     Per-database value size limit via [Options.MaxValueBytes] (512–16384 bytes; default 8192); readable at runtime via [DB.MaxValueBytes].
//     MVCC on new databases via [Options.EnableMVCC] / [Options.MVCCRetention];
//     lifecycle: [DB.StatsMVCC], [DB.PruneCellVersions], [DB.SuggestedPruneBeforeSeq], [PruneScheduler]
//     (see docs/hexxladb/OPERATIONS.md). Encryption operations include [RotateEncryption].
//   - [DB.SnapshotDiff] — MVCC change diff between two commit sequences; yields [SnapshotDiff] with [CellDiff]/[SeamDiff] slices.
//   - [DB.View], [DB.ViewAt], [DB.ViewAtTime], [DB.Update], [DB.Batch], [Tx] —
//     Bolt-style transactions; [DB.Batch] is an alias for [DB.Update]; see docs/hexxladb/TX.md.
//   - [DB.Compact], [CompactTo] — copy-compaction to reclaim dead pages (see docs/hexxladb/OPERATIONS.md).
//   - [Tx.Get], [Tx.Put], [Tx.AscendRange] — byte-key ordered store.
//     Lattice primitives: [Tx.PutCell], [Tx.GetCell], [Tx.DeleteCell], [Tx.WalkRing], [Tx.PutSeam],
//     [Tx.FindSeams], [Tx.FindSeamsAt], [Tx.LoadContext], [Tx.ResolveSeam] (see [primitives.go]);
//     facets/edges: [Tx.PutFacet], [Tx.GetFacet], [Tx.AscendFacetsForCell],
//     [Tx.PutEdge], [Tx.GetEdge], [Tx.AscendEdgesFrom] ([facets_edges.go]);
//     spec-named sugar: [Tx.MarkConflict], [Tx.UpdateFacet], [Tx.LinkCells];
//     validity read filters: [record.ValidAt], [Tx.WalkRingAt], [Tx.LoadContextAt], [Tx.WalkRingFacets];
//     secondary index walks: [Tx.AscendCellsBySource], [Tx.AscendCellsInTimeBucket],
//     [Tx.AscendCellsByTag], [Tx.AscendDistinctTags], [Tx.ListExistingTopics],
//     [Tx.AscendSeamsBySource], [Tx.AscendSeamsInTimeBucket];
//     HEXXLA-shaped views ([views.go]): [CellView], [ContextPack], [Tx.AssembleCellView],
//     [Tx.LoadContextWithBudgeting], [Tx.LoadContextPack], [CellViewPredicate],
//     [FilterCellViews], [TruncateCellViewsToTokenBudget], [TokenBudgeter].
//   - Sentinel errors: [ErrCorruptDatabase], [ErrDatabaseClosed], [ErrTxReadOnly],
//     [ErrNilCallback], [ErrNotImplemented], [ErrClosed], [ErrSeamNotFound],
//     [ErrSeamEndpointMismatch], [ErrInvalidArgument], [ErrEncryptionKeyRequired],
//     [ErrDatabaseNotEncrypted], [ErrEncryptionOptions], [ErrEncryptionKeyMismatch],
//     [ErrCellNotFound], [ErrFacetDerivationMismatch], [ErrChangelogDisabled],
//     [ErrChangelogCorrupt], [ErrReadSeqFuture], [ErrMVCCRequired],
//     [ErrSnapshotTagNotFound], [ErrSnapshotTagLabelTooLong].
//
// Lattice types ([Coord], [PackedCoord], [Pack], [Unpack], [Ring], [WalkRings]) are
// re-exported from internal/lattice; see docs/hexxladb/HEXXLA.md and
// internal/lattice/PACKED_COORD.md for geometry and key layout.
//
// # Embedding in your program
//
// Follow the wiring pattern in cmd/hexxladb/main.go: configuration, structured
// logging, [Open] with [DB.Close], inject dependencies into your application
// layer. Outbound adapters should call only types and functions exported from
// package hexxladb, not internal/engine (see docs/context/HEXAGONAL_ARCHITECTURE.md).
package hexxladb
