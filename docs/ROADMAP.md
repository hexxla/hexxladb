# Roadmap

For completed work, see `TODOS.md` (Recently Completed) and `CHANGELOG.md`.

## Near-term

- **Content Compression** — zstd/gzip transparent compression of cell values. Addresses file size bloat (82 cells = 5.6 MB with 1.2% page utilization) and obscures plaintext in unencrypted databases. Compress on write, decompress on read, controlled by `Options`.
- **Configurable page size** — expose `Options.PageSize` (4/8/16/64 KiB, default 4 KiB for new databases). Stored in file header; existing 64 KiB databases continue to work. Combined with higher `maxLeafEntries` this brings typical small-database overhead from 98% wasted space to <20%.
- **Relocate `cell_secondary.go` / `seam_secondary.go` to `internal/`** — pure file move, no logic change. Both files contain only unexported `*Tx` methods.
- **`domain.Storage` interface contract tests** — `internal/domain/storage_test.go` with a fake implementation to validate port contracts independently of the adapter.

## Future

Spec exists; implementation deferred.

- **Move `rotation.go` to `internal/tooling/rotation`** — uses `DB.Open`, `Tx.putDirect`, root error sentinels; cycle is hard to break without significant restructuring or exposing `UnsafePut`; in-root placement is not architecturally wrong; reclassified from Near-term.
- `embed/` keyspace for ANN/hybrid retrieval — vector storage and similarity search for semantic seed selection ([`HEXXLA_DB.md`](./hexxladb/HEXXLA_DB.md)). When implemented, `CellSearchConfig.Embedding []float32` field will be added to Content Search API — existing `Query string` callers unaffected.
- Materialized views / super-hex aggregation as engine algorithms
- Materialized changefeed consumers with automated prune policy
- Changelog Subscription (push mode) — real-time reactions via channels
- Cell Relationship Graph Export — nodes/edges/seams for external analysis
- Confidence Decay Policy — time-based confidence reduction with audit trail
- **MVCC version chain optimisation** — for cells with many versions (>100), the current O(n) linear scan in `SelectVisible` may become a bottleneck; consider skip list or tree structure; defer until profiling shows this is a real hot path ([LEAN audit](./context/audits/LEAN_ARCHITECTURE_AUDIT.md))

## Future exploration

Interesting but unvalidated. Needs user demand or benchmark data before committing.

- Hot Cell Tracking — LRU-based access frequency tracking for cache warming (overhead concerns)
- **Record encoding allocation reduction** — `AppendEnvelope` in `internal/record` allocates a fresh buffer on every encode; pre-sizing via pool or capacity hint could reduce GC pressure under write-heavy workloads; needs benchmark validation before committing ([LEAN audit](./context/audits/LEAN_ARCHITECTURE_AUDIT.md))
- Edge Weight Decay — connections strengthen with traversal, weaken with disuse (speculative)
- Facet Diff/Compare — see what changed between facet versions (audit utility)
- Shortest Path Between Cells — graph traversal via edges (BFS implementation)

## Out of Scope

Intentional boundaries for embedded library v1.

- Distributed replication / HA — product-tier orchestration
- Freelist / automatic primary file shrink — extend-only allocator by design; use `DB.Compact` for offline file size reduction ([`OPERATIONS.md`](./hexxladb/OPERATIONS.md))
- Online re-encryption — offline rotation only ([`VERSIONING.md`](../VERSIONING.md))
- Third-party KV backends (SQLite, etc.) — Hex-native engine is the direction
