# Roadmap

For completed work, see `TODOS.md` (Recently Completed) and `CHANGELOG.md`.

## Completed

- ~~**Embeddings keyspace (`embed/`)**~~ — Flat-scan + HNSW ANN search, query engine integration via `CellQuery.Embedding` / `CellSearchConfig.Embedding`, benchmarks, docs. See `CHANGELOG.md`.

## Near-term

- **B+ tree leaf-page-full at high embedding counts** — HNSW nodes + embedding values overflow leaf pages when count exceeds ~500 (32d) or ~100 (128d) even at 65536 page size; likely a split-index edge case in `btree.go:leafSplitIndex`; benchmarks capped at safe sizes; blocks scaling to production embedding counts.
- **Extract `TxWriter` interface for secondary index testing** — `cell_secondary.go` and `seam_secondary.go` must remain in `package hexxladb` (receiver methods on `*Tx` using unexported fields); extracting a `TxWriter` interface would let the secondary-index logic be unit-tested without a real DB. Low priority given contract tests now cover the public surface.

## Future

Spec exists; implementation deferred.

- **Move `rotation.go` to `internal/tooling/rotation`** — uses `DB.Open`, `Tx.putDirect`, root error sentinels; cycle is hard to break without significant restructuring or exposing `UnsafePut`; in-root placement is not architecturally wrong; reclassified from Near-term.
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
