# Roadmap

For completed work, see `CHANGELOG.md` and `TODOS.md` (Recently Completed).

## Near-term

- **Extract `TxWriter` interface for secondary index testing** — `cell_secondary.go` and `seam_secondary.go` must remain in `package hexxladb` (receiver methods on `*Tx` using unexported fields); extracting a `TxWriter` interface would let the secondary-index logic be unit-tested without a real DB.
  _Impact: faster, cheaper test cycles for secondary-index changes; catches regressions without spinning up a full B+ tree; reduces barrier to contributors._

## Future

Spec exists; implementation deferred.

- **Move `rotation.go` to `internal/tooling/rotation`** — uses `DB.Open`, `Tx.putDirect`, root error sentinels; cycle is hard to break without significant restructuring or exposing `UnsafePut`; in-root placement is not architecturally wrong.
  _Impact: cleaner root package surface; rotation is a maintenance utility, not a user-facing primitive; separating it reduces noise in `pkg.go.dev` docs._

- **Materialized views / super-hex aggregation** — precomputed summaries over hex rings stored as first-class pages; updated incrementally on write.
  _Impact: eliminates repeated ring-walk computation for dashboards, analytics, and agent state summaries; enables O(1) reads of aggregated context without full ring scans._

- **Materialized changefeed consumers with automated prune policy** — persistent consumer offsets + server-driven retention; consumers declare a `min_retained_seq` that blocks pruning until they have processed it.
  _Impact: enables reliable CDC pipelines and audit consumers without external coordination; prevents data loss when a downstream processor falls behind._

- **Changelog Subscription (push mode)** — real-time `chan ChangelogEntry` delivery on commit; no polling.
  _Impact: agents can react to memory changes instantly (e.g. invalidate a cached prompt when a preference cell is updated); eliminates polling overhead in reactive architectures._

- **Cell Relationship Graph Export** — export cells, seams, and edges as a standard graph format (JSON-LD, GraphML, or dot).
  _Impact: enables external graph visualisation, graph ML pipelines, and auditing tools without reimplementing traversal logic; unblocks users who want to inspect the memory topology._

- **Confidence Decay Policy** — time-based automatic confidence reduction; cells older than a configurable window decay toward zero and become eligible for eviction from context assembly.
  _Impact: stale facts naturally fall out of context without manual intervention; particularly valuable for long-running agents where old preferences or facts become misleading._

- **MVCC version chain optimisation** — replace O(n) linear scan in `SelectVisible` with a skip list or compact index for cells with many versions (>100).
  _Impact: prevents latency regression on long-running databases with heavy update workloads; keeps `ViewAt` and time-travel reads fast at scale. Defer until profiling confirms this is a real hot path ([LEAN audit](./context/audits/LEAN_ARCHITECTURE_AUDIT.md))._

## Future exploration

Interesting but unvalidated. Needs user demand or benchmark data before committing.

- **Hot Cell Tracking** — LRU-based access frequency tracking; frequently-retrieved cells get priority in context assembly tie-breaking.
  _Impact: context quality improves over time as the assembler learns which memories are actually useful; no manual tuning required. Overhead concerns need profiling._

- **Record encoding allocation reduction** — pool or capacity-hint `AppendEnvelope` in `internal/record` to reduce per-encode allocations.
  _Impact: lower GC pressure under write-heavy workloads (batch ingestion, embedding reindex); potentially significant at >1000 writes/sec. Needs benchmark validation before committing ([LEAN audit](./context/audits/LEAN_ARCHITECTURE_AUDIT.md))._

- **Edge Weight Decay** — directed edges strengthen with traversal frequency and weaken with disuse over time.
  _Impact: graph structure self-organises around actually-useful relationships; retrieval via `AscendEdgesFrom` naturally surfaces the most relevant associations without manual curation._

- **Facet Diff/Compare** — diff two facet versions for a cell to see exactly what changed between commits.
  _Impact: enables audit trails for annotations and summaries; agents can present "what changed in my understanding of X since yesterday" explanations to users._

- **Shortest Path Between Cells** — BFS/Dijkstra over the edge graph between two coordinates.
  _Impact: enables "how did we get from belief A to belief B?" reasoning chains; useful for explainability and for agents that need to trace causal links between memories._

## Out of Scope

Intentional boundaries for embedded library v1.

- **Distributed replication / HA** — product-tier orchestration; HexxlaDB is embedded by design.
- **Freelist / automatic primary file shrink** — extend-only allocator by design; use `DB.Compact` for offline file size reduction ([`OPERATIONS.md`](./hexxladb/OPERATIONS.md)).
- **Online re-encryption** — offline rotation only ([`VERSIONING.md`](../VERSIONING.md)).
- **Third-party KV backends (SQLite, etc.)** — Hex-native engine is the direction; abstractions over foreign storage engines add complexity without matching HexxlaDB's spatial key model.
