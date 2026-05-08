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
//     Configurable page size via [Options.PageSize] (4096/8192/16384/65536; default 4096); readable at runtime via [DB.PageSize].
//     Per-database value size limit via [Options.MaxValueBytes] (512–1048576 bytes; default 8192); readable at runtime via [DB.MaxValueBytes].
//     Transparent per-value DEFLATE compression is always-on (compress/flate, Go stdlib; values ≥ 64 bytes).
//     MVCC on new databases via [Options.EnableMVCC] / [Options.MVCCRetention];
//     lifecycle: [DB.StatsMVCC], [DB.PruneCellVersions], [DB.SuggestedPruneBeforeSeq], [PruneScheduler]
//     (see docs/hexxladb/OPERATIONS.md). Encryption operations include [RotateEncryption].
//   - [DB.SnapshotDiff] — MVCC change diff between two commit sequences; yields [SnapshotDiff] with [CellDiff]/[SeamDiff] slices.
//   - [DB.View], [DB.ViewAt], [DB.ViewAtTime], [DB.Update], [DB.Batch], [Tx] —
//     Bolt-style transactions; [DB.Batch] is an alias for [DB.Update]; see docs/hexxladb/TX.md.
//   - [DB.Compact], [CompactTo] — copy-compaction to reclaim dead pages (see docs/hexxladb/OPERATIONS.md).
//   - [Tx.Get], [Tx.Put], [Tx.AscendRange] — byte-key ordered store.
//     Lattice primitives: [Tx.PutCell], [Tx.GetCell], [Tx.DeleteCell], [Tx.DeleteCellWithOutcome], [Tx.WalkRing], [Tx.PutSeam],
//     [Tx.FindSeams], [Tx.FindSeamsAt], [Tx.LoadContext], [Tx.ResolveSeam] (see [primitives.go]);
//     facets/edges: [Tx.PutFacet], [Tx.GetFacet], [Tx.AscendFacetsForCell],
//     [Tx.PutEdge], [Tx.GetEdge], [Tx.AscendEdgesFrom] ([facets_edges.go]);
//     embedding-only names: [FacetWalkRecord], [EdgeWalkRecord] ([walk_export_aliases.go]);
//     [NewFacetDerived], [NewProvenanceWire] ([templates.go]) for callers outside this module;
//     spec-named sugar: [Tx.MarkConflict], [Tx.UpdateFacet], [Tx.LinkCells];
//     validity read filters: [record.ValidAt], [Tx.WalkRingAt], [Tx.LoadContextAt], [Tx.WalkRingFacets];
//     secondary index walks: [Tx.AscendCellsBySource], [Tx.AscendCellsInTimeBucket],
//     [Tx.AscendCellsByTag], [Tx.AscendDistinctTags], [Tx.ListExistingTopics],
//     [Tx.AscendSeamsBySource], [Tx.AscendSeamsInTimeBucket];
//     HEXXLA-shaped views ([views.go]): [CellView], [ContextPack], [Tx.AssembleCellView],
//     [Tx.LoadContextWithBudgeting], [Tx.LoadContextPack], [CellViewPredicate],
//     [FilterCellViews], [TruncateCellViewsToTokenBudget], [TokenBudgeter].
//   - Embedding / vector search: dimension auto-detected from first [Tx.PutEmbedding]
//     (or pre-set via [Options.EmbeddingDimension]); [Options.DistanceMetric] (default cosine);
//     [Tx.PutEmbedding], [Tx.GetEmbedding], [Tx.DeleteEmbedding];
//     [Tx.SearchByEmbedding] (HNSW-accelerated ANN, flat-scan fallback), [EmbeddingSearchConfig];
//     [Tx.ReindexEmbeddings]; [CellQuery.Embedding] / [CellSearchConfig.Embedding] integrate
//     vector search into [Tx.QueryCells] / [Tx.SearchCells] (see docs/hexxladb/API_REFERENCE.md).
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
