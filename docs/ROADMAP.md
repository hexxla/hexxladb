# Roadmap

For completed work, see `TODOS.md` (Recently Completed) and `CHANGELOG.md`.

## Near-term

- **`Tx.DeleteCell`** — remove a cell and all its associated data atomically: primary key (`cell/<packed>`), all secondary indexes (`source/`, `time/`, `tag/`), all facets (`facet/<packed>/<id>`), all edges (`edge/<packed>/...`). Seam cleanup is caller-controlled (seams reference two cells; deleting one endpoint does not auto-resolve the seam — caller should `ResolveSeam` or let the orphan surface via `HealthCheck`). New sentinel: `ErrCellNotFound` on delete of a missing cell (or silent no-op — needs decision). MVCC: on v2 databases, the primary key is tombstoned rather than hard-deleted so `as_of` snapshots before the delete remain consistent. Regression tests required covering: primary removal, secondary index cleanup, facet/edge cleanup, MVCC tombstone behaviour.
- **`DB.Compact`** — offline copy-compaction that shrinks the database file to the minimum size needed for live data. Walks all live B+ tree keys via `AscendRange`, writes them sequentially into a fresh file using a new `BTree`, then atomically swaps. No lattice reorganisation required — hex coordinates are encoded in keys (Morton-packed), not page positions; a page-level rewrite is sufficient. Should honour `Options` (encryption, `MaxValueBytes`). API: `DB.Compact(ctx, destPath string, opts *Options) error` or `DB.CompactInPlace(ctx) error` (write to temp file, rename). Produces an identical-content but smaller database; safe to run offline (caller closes DB first) or online with a read lock depending on design chosen.

## Future

Spec exists; implementation deferred.

- **Relocate `cell_secondary.go` / `seam_secondary.go` to `internal/`** — both files contain no exported symbols and only unexported `*Tx` methods; btree coupling is already cleanly abstracted via `putDirect`/`deleteDirect`; a `TxIndexWriter` interface would add indirection with no architectural gain at this point; reclassified from Near-term.
- **Move `rotation.go` to `internal/tooling/rotation`** — uses `DB.Open`, `Tx.putDirect`, root error sentinels; cycle is hard to break without significant restructuring or exposing `UnsafePut`; in-root placement is not architecturally wrong; reclassified from Near-term.
- `embed/` keyspace for ANN/hybrid retrieval — vector storage and similarity search for semantic seed selection ([`HEXXLA_DB.md`](./hexxladb/HEXXLA_DB.md)). When implemented, `CellSearchConfig.Embedding []float32` field will be added to Content Search API — existing `Query string` callers unaffected.
- Materialized views / super-hex aggregation as engine algorithms
- Materialized changefeed consumers with automated prune policy
- Changelog Subscription (push mode) — real-time reactions via channels
- Cell Relationship Graph Export — nodes/edges/seams for external analysis
- Confidence Decay Policy — time-based confidence reduction with audit trail
- **MVCC version chain optimisation** — for cells with many versions (>100), the current O(n) linear scan in `SelectVisible` may become a bottleneck; consider skip list or tree structure; defer until profiling shows this is a real hot path ([LEAN audit](./context/audits/LEAN_ARCHITECTURE_AUDIT.md))
- **`domain.Storage` interface contract tests** — `internal/domain/storage_test.go` with a fake implementation to validate port contracts independently of the adapter; low urgency since `internal/app` tests cover the interface indirectly ([LEAN audit](./context/audits/LEAN_ARCHITECTURE_AUDIT.md))

## Future exploration

Interesting but unvalidated. Needs user demand or benchmark data before committing.

- Hot Cell Tracking — LRU-based access frequency tracking for cache warming (overhead concerns)
- Content Compression — gzip/zstd compression for large cells >512B (benchmark first)
- **Record encoding allocation reduction** — `AppendEnvelope` in `internal/record` allocates a fresh buffer on every encode; pre-sizing via pool or capacity hint could reduce GC pressure under write-heavy workloads; needs benchmark validation before committing ([LEAN audit](./context/audits/LEAN_ARCHITECTURE_AUDIT.md))
- Edge Weight Decay — connections strengthen with traversal, weaken with disuse (speculative)
- Facet Diff/Compare — see what changed between facet versions (audit utility)
- Shortest Path Between Cells — graph traversal via edges (BFS implementation)

## Out of Scope

Intentional boundaries for embedded library v1.

- Distributed replication / HA — product-tier orchestration
- Freelist / primary file shrink — extend-only allocator by design ([`OPERATIONS.md`](./hexxladb/OPERATIONS.md))
- Online re-encryption — offline rotation only ([`VERSIONING.md`](../VERSIONING.md))
- Third-party KV backends (SQLite, etc.) — Hex-native engine is the direction
