# Roadmap

## Quick Wins

Low effort, high value. No design required.

- QueryStats on ContextPack — visibility into why cells were included/excluded during context assembly ([audit](./context/audits/HEXXLA_SERVICE_QUICK_WINS.md))
- RingDensity API — count cells per ring for dashboard visualization and memory density maps
- Cell Template Factory — standardized constructors for UserMessage, AssistantResponse, SystemPrompt, Fact cells
- Bulk Cell Import/Export (JSON/CSV) — migration, testing data seeding, backup/restore pipelines
- ASCII Hex Grid Renderer — debug/logging visualization of the lattice
- Filtered Changelog Reading — watch only cell writes, seams, or specific tags
- Tag Analytics — tag counts, co-occurrences, untagged cell detection

## Near-term

Requires design + benchmarks before implementation.

- Batch MVCC prune (`PruneCellVersions`) — coalesce deletes under single engine write txn to reduce WAL pressure ([`DURABILITY.md`](./hexxladb/DURABILITY.md))
- Database Health Check API — integrity verification, orphaned seam detection, index consistency ([audit](./context/audits/HEXXLA_SERVICE_QUICK_WINS.md))
- Event Hooks / Callbacks — react to cell writes, seam detection, facet rotation (needs architecture RFC)
- Content Search (substring/prefix) — brute-force search within `RawContent` for small-medium DBs (benchmark first)

## Future

Spec exists; implementation deferred.

- `embed/` keyspace for ANN/hybrid retrieval ([`HEXXLA_DB.md`](./hexxladb/HEXXLA_DB.md))
- Materialized views / super-hex aggregation as engine algorithms
- Materialized changefeed consumers with automated prune policy
- Changelog Subscription (push mode) — real-time reactions via channels
- MVCC Snapshot Diff — compare state between two `CommitSeq` values
- Cell Relationship Graph Export — nodes/edges/seams for external analysis
- Confidence Decay Policy — time-based confidence reduction with audit trail

## Future exploration

Interesting but unvalidated. Needs user demand or benchmark data before committing.

- Hot Cell Tracking — LRU-based access frequency tracking for cache warming (overhead concerns)
- Content Compression — gzip/zstd compression for large cells >512B (benchmark first)
- Edge Weight Decay — connections strengthen with traversal, weaken with disuse (speculative)
- Facet Diff/Compare — see what changed between facet versions (audit utility)
- Shortest Path Between Cells — graph traversal via edges (BFS implementation)

## Out of Scope

Intentional boundaries for embedded library v1.

- Distributed replication / HA — product-tier orchestration
- Freelist / primary file shrink — extend-only allocator by design ([`OPERATIONS.md`](./hexxladb/OPERATIONS.md))
- Online re-encryption — offline rotation only ([`VERSIONING.md`](../VERSIONING.md))
- Third-party KV backends (SQLite, etc.) — Hex-native engine is the direction

---

## Audit Log

| Date       | Scope                                                                                                          |
| ---------- | -------------------------------------------------------------------------------------------------------------- |
| 2026-04-24 | v0.1.0 initial release                                                                                         |
| 2026-04-24 | Roadmap consolidated to priority-based format                                                                  |
| 2026-04-24 | Added HEXXLA service quick wins from audit (QueryStats, RingDensity, Templates, Bulk ops, Health check, Hooks) |
