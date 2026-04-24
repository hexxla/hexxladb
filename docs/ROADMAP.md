# Roadmap

## v0.1.0 Blockers (Active)

Must complete before release. Core functionality gaps.

- **Seam-aware context assembly** — when loading ContextPack, filter/supersede outdated cells via seam links; contradictions are useless without action; walk seam chains to current truth, exclude superseded data, preserve token budget ([discussion](./context/audits/HEXXLA_SERVICE_QUICK_WINS.md))

## Completed (v0.1.0)

Shipped with v0.1.0 release.

- **Increase max cell value to 8KB** — 8192 bytes handles typical prompts/conversation turns; updated `btree_page.go`, `API_REFERENCE.md` (no format_version bump needed - runtime validation only)
- **Rename `internal/mvccspike` → `internal/mvcc`** — promoted production MVCC visibility algorithm to stable package name ([audit](./context/audits/SOC_MODULARITY_AUDIT.md))
- **Extract prune profile helper** — `profileToMaxDelete` deduplicates `MVCCPrunePlan` and `PruneCellVersionsByProfile` switch blocks ([audit](./context/audits/SOC_MODULARITY_AUDIT.md))
- **Move MVCC key validation out of `Tx.Put`** — added `putDirect` for internal primitives; MVCC cell key guard stays on public `Tx.Put` only ([audit](./context/audits/SOC_MODULARITY_AUDIT.md))
- **Refactor `goto assembled`** — extracted `collectCandidates` helper from `LoadContextWithBudgeting` for cleaner control flow and isolated testability ([audit](./context/audits/SOC_MODULARITY_AUDIT.md))

## Quick Wins

Low effort, high value. No design required.

- Per-database MaxValueBytes — store limit in file header (default 8KB), expose in Options for 2KB/4KB/16KB use cases
- QueryStats on ContextPack — visibility into why cells were included/excluded during context assembly ([audit](./context/audits/HEXXLA_SERVICE_QUICK_WINS.md))
- RingDensity API — count cells per ring for dashboard visualization and memory density maps
- Cell Template Factory — standardized constructors for UserMessage, AssistantResponse, SystemPrompt, Fact cells
- Bulk Cell Import/Export (JSON/CSV) — migration, testing data seeding, backup/restore pipelines
- ASCII Hex Grid Renderer — debug/logging visualization of the lattice
- Filtered Changelog Reading — watch only cell writes, seams, or specific tags
- Tag Analytics — tag counts, co-occurrences, untagged cell detection
- Context Pack "Explain" Mode — per-cell inclusion reasons showing why each cell was included or evicted (budget_ok, low_confidence_evicted, ring_cutoff) for debugging token budget decisions ([audit](./context/audits/HEXXLA_SERVICE_QUICK_WINS.md))
- Batch PutCell with Progress — efficient ingestion of conversation history with progress callbacks and continue-on-error options; distinct from Import/Export for real-time streaming scenarios ([audit](./context/audits/HEXXLA_SERVICE_QUICK_WINS.md))
- Cell Validation Hooks — pre-write validation interface for enforcing content limits, required tags, and custom business rules; production-critical for data integrity ([audit](./context/audits/HEXXLA_SERVICE_QUICK_WINS.md))
- Relocate secondary index logic to `internal/` — `cell_secondary.go` and `seam_secondary.go` are unexported helpers that call `tx.db.btree.Delete` directly, bypassing Tx abstraction; move to `internal/txcore` or `internal/storage` to enforce boundary ([audit](./context/audits/SOC_MODULARITY_AUDIT.md))
- Extract `views.go` to `internal/views` or `internal/app` — `TokenBudgeter`, `ByteLenBudgeter`, `LoadContextWithBudgeting` are app-layer read projections with no storage I/O; re-export only types from module root ([audit](./context/audits/SOC_MODULARITY_AUDIT.md))
- ~~Move `rotation.go` to `internal/tooling/rotation`~~ — **deferred to Near-term**; rotation uses `DB.Open`, `Tx.putDirect`, error sentinels — moving to `internal/` creates an import cycle; needs interface extraction first ([audit](./context/audits/SOC_MODULARITY_AUDIT.md))
- ~~Encapsulate commit-time meta-key~~ — **closed: false finding**; meta-key is written once per transaction in `DB.Update` (`tx.go`), not per-cell in `PutCell`; placement is correct

## Near-term

Requires design + benchmarks before implementation.

- Batch MVCC prune (`PruneCellVersions`) — coalesce deletes under single engine write txn to reduce WAL pressure ([`DURABILITY.md`](./hexxladb/DURABILITY.md))
- Database Health Check API — integrity verification, orphaned seam detection, index consistency ([audit](./context/audits/HEXXLA_SERVICE_QUICK_WINS.md))
- Event Hooks / Callbacks — react to cell writes, seam detection, facet rotation (needs architecture RFC)
- Content Search (substring/prefix) — brute-force search within `RawContent` for small-medium DBs (benchmark first)
- Temporal Range Queries — "what changed this week?" time-series analysis vs point-in-time `ViewAtTime`; cells and seams in time buckets with timeline summaries ([audit](./context/audits/HEXXLA_SERVICE_QUICK_WINS.md))
- Snapshot Tags/Labels — human-friendly names ("v1.0 release", "pre-migration") for MVCC snapshots instead of raw sequence numbers; enables `ViewAtTag` for operational usability ([audit](./context/audits/HEXXLA_SERVICE_QUICK_WINS.md))
- Complete `app.Service` use-case layer — only 4 of ~30 `domain.Storage` methods implemented; `cmd/hexxladb/main.go` wires service then discards it (`_ = svc`); implement remaining delegations and move view assembly into service use-cases ([audit](./context/audits/SOC_MODULARITY_AUDIT.md))

## Future

Spec exists; implementation deferred.

- `embed/` keyspace for ANN/hybrid retrieval — vector storage and similarity search for semantic seed selection ([`HEXXLA_DB.md`](./hexxladb/HEXXLA_DB.md))
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

| Date       | Scope                                                                                                                                                                                                                                                               |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-04-24 | v0.1.0 initial release                                                                                                                                                                                                                                              |
| 2026-04-24 | Roadmap consolidated to priority-based format                                                                                                                                                                                                                       |
| 2026-04-24 | Added HEXXLA service quick wins from audit (QueryStats, RingDensity, Templates, Bulk ops, Health check, Hooks)                                                                                                                                                      |
| 2026-04-24 | **v0.1.0 scope locked:** 8KB cell size increase as release blocker                                                                                                                                                                                                  |
| 2026-04-24 | **v0.1.0 scope updated:** seam-aware context assembly added as release blocker — contradictions must be actionable                                                                                                                                                  |
| 2026-04-24 | **SoC audit validated:** mvccspike rename, prune profile dedup, MVCC key guard relocation added as v0.1.0 blockers; secondary index relocation, views.go extraction, app.Service completion added as Quick Wins ([audit](./context/audits/SOC_MODULARITY_AUDIT.md)) |
