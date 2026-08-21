# Roadmap

For completed work, see `CHANGELOG.md`.

## Documentation (operator-facing)

Recent clarity-only updates (no API change): **`docs/hexxladb/OPERATIONS.md`** (MVCC retention vs **`SuggestedPruneBeforeSeq`**); **`delete_cell.go`** package doc (tombstones vs **`PruneCellVersions`**).

## Near-term

- **Bulk / batched cell delete helper** — `BatchPutCells`-style surface for deletes (e.g. chunked `DeleteCell` in one or many `Update` passes, optional progress/`ContinueOnError`, changelog semantics per row). MVCC-aware; does not imply automatic file shrink — operators still **`PruneCellVersions`** + **`Compact`** when reclaiming disk is the goal.
  _Impact: ergonomics for “forget this session” / GDPR-style erasure without N separate transactions from callers; aligns Mosaic MCP or apps that today loop `delete_cell`._

- **Extract `TxWriter` interface for secondary index testing** — `cell_secondary.go` and `seam_secondary.go` must remain in `package hexxladb` (receiver methods on `*Tx` using unexported fields); extracting a `TxWriter` interface would let the secondary-index logic be unit-tested without a real DB.
  _Impact: faster, cheaper test cycles for secondary-index changes; catches regressions without spinning up a full B+ tree; reduces barrier to contributors._

## Future

Spec exists; implementation deferred.

- **Move `rotation.go` to `internal/tooling/rotation`** — uses `DB.Open`, `Tx.putDirect`, root error sentinels; cycle is hard to break without significant restructuring or exposing `UnsafePut`; in-root placement is not architecturally wrong.
  _Impact: cleaner root package surface; rotation is a maintenance utility, not a user-facing primitive; separating it reduces noise in `pkg.go.dev` docs._

- **Persistent/content-bearing materialized super-hex views** — the shipped `SuperHexSummaryIndex` prototype provides rebuildable in-memory aperture-7 occupancy summaries updated from the changelog. Future work may persist richer summary payloads as first-class pages after workloads establish freshness, storage, and aggregation requirements.
  _Impact: extends the proven O(1) occupancy lookup into durable agent-state or content summaries without changing `PackedCoord`._

- **Materialized changefeed consumers with automated prune policy** — persistent consumer offsets + server-driven retention; consumers declare a `min_retained_seq` that blocks pruning until they have processed it.
  _Impact: enables reliable CDC pipelines and audit consumers without external coordination; prevents data loss when a downstream processor falls behind._

- **Changelog Subscription (push mode)** — real-time `chan ChangelogEntry` delivery on commit; no polling.
  _Impact: agents can react to memory changes instantly (e.g. invalidate a cached prompt when a preference cell is updated); eliminates polling overhead in reactive architectures._

- **Cell Relationship Graph Export** — export cells, seams, and edges as a standard graph format (JSON-LD, GraphML, or dot).
  _Impact: enables external graph visualisation, graph ML pipelines, and auditing tools without reimplementing traversal logic; unblocks users who want to inspect the memory topology._

- **Confidence Decay Policy** — time-based automatic confidence reduction; cells older than a configurable window decay toward zero and become eligible for eviction from context assembly.
  _Impact: stale facts naturally fall out of context without manual intervention; particularly valuable for long-running agents where old preferences or facts become misleading._

- **MVCC version chain optimisation** — replace O(n) linear scan in `SelectVisible` with a skip list or compact index for cells with many versions (>100).
  _Impact: prevents latency regression on long-running databases with heavy update workloads; keeps `ViewAt` and time-travel reads fast at scale. Defer until profiling confirms this is a real hot path._

- **Sub-linear SSSP via Duan et al. 2025 (BMSSP)** — Deterministic `O(m log^(2/3) n)` single-source shortest paths replacing Dijkstra for large sparse graphs. Relevant to `LoadContextVoronoi` (multi-source Dijkstra) and any future large-graph `FindEdgePath`. Implementation requires a custom block-list data structure (Lemma 3.3: two block sequences + Red-Black tree on upper bounds), `FindPivots` (bounded Bellman-Ford), and the recursive `BMSSP` divide-and-conquer.
  _Practical crossover vs A\*: estimated n ≥ 10⁵–10⁶ nodes; current HexxlaDB workloads peak at ~31k cells (MaxRing=100). Defer until large-graph use cases emerge. Reference: arXiv:2504.17033._

## Future exploration

Interesting but unvalidated. Needs user demand or benchmark data before committing.

### Engine shell & MVCC exploration

Research spikes; default operator guidance stays **`Compact`** + **`PruneCellVersions`** ([`OPERATIONS.md`](./hexxladb/OPERATIONS.md)) until something here graduates.

- **`DB.Path()`** (ergonomics) — expose the primary file path from an opened `*DB` so embedders (health checks, optional **`os.Stat`**) do not thread path in parallel with the handle. Low risk; small API surface.
- **Partial primary reclaim without a full `Compact` copy** — investigate freelist trimming, tail truncation after internal compaction, or OS sparse-file / punch-hole techniques. **Today extend-only allocation + offline `Compact` is intentional** (see **Out of scope** below); any faster shrink path needs durability proofs and likely stays platform-specific or remains optional.
- **`PurgeCoord` / physical removal of latest tombstone** — design spike: optional API or policy flag that removes the last MVCC row for a coordinate when operators accept weaker **`ViewAt`** guarantees (or explicit “no snapshot before seq _S_” invariants). Must not silently break existing MVCC contracts.
- **Alternative / extended `PruneCellVersions` semantics** — e.g. optional modes coordinated with changefeed retention, or bounded “forget coord” flows; requires RFC against current rule (**latest row per coord always retained** until superseded).

---

- **Hot Cell Tracking** — LRU-based access frequency tracking; frequently-retrieved cells get priority in context assembly tie-breaking.
  _Impact: context quality improves over time as the assembler learns which memories are actually useful; no manual tuning required. Overhead concerns need profiling._

- **Record encoding allocation reduction** — pool or capacity-hint `AppendEnvelope` in `internal/record` to reduce per-encode allocations.
  _Impact: lower GC pressure under write-heavy workloads (batch ingestion, embedding reindex); potentially significant at >1000 writes/sec. Needs benchmark validation before committing._

- **Edge Weight Decay** — directed edges strengthen with traversal frequency and weaken with disuse over time.
  _Impact: graph structure self-organises around actually-useful relationships; retrieval via `AscendEdgesFrom` naturally surfaces the most relevant associations without manual curation._

- **Facet Diff/Compare** — diff two facet versions for a cell to see exactly what changed between commits.
  _Impact: enables audit trails for annotations and summaries; agents can present "what changed in my understanding of X since yesterday" explanations to users._

- **~~Shortest Path Between Cells~~** — ✅ Done. `FindEdgePath` (Dijkstra), `WalkEdges` (BFS), and graph-aware `LoadContext` via `LoadContextConfig.EdgeFilter`. See API reference.

- **~~Field of View~~** — ✅ Done. `LoadContextFOV` — symmetric shadowcasting; empty cells block visibility. See API reference.

- **~~Level of Detail + Voronoi~~** — ✅ Done. `LoadContext` automatically selects LOD for a large single-seed radius; `LoadContextVoronoi` provides non-overlapping multi-seed regions. See API reference.

## Out of Scope

Intentional boundaries for embedded library v1.

- **Distributed replication / HA** — product-tier orchestration; HexxlaDB is embedded by design.
- **Freelist / automatic primary file shrink** — extend-only allocator by design; use `DB.Compact` for offline file size reduction ([`OPERATIONS.md`](./hexxladb/OPERATIONS.md)). _Exploration_ of partial reclaim or punch-hole tricks may appear under **Future exploration → Engine shell & MVCC exploration**; they do not change this default until promoted with benchmarks and durability review.
- **Online re-encryption** — offline rotation only ([`VERSIONING.md`](../VERSIONING.md)).
- **Third-party KV backends (SQLite, etc.)** — Hex-native engine is the direction; abstractions over foreign storage engines add complexity without matching HexxlaDB's spatial key model.

### Research-corpus ideas rejected for now

These are useful algorithms in their intended domains, but they do not match a demonstrated HexxlaDB workload today:

- **Grid-specialized route planners** (JPS/JPS+, Theta*/Anya/Polyanya, Mesh A*, visibility/subgoal graphs) assume implicit geometric movement and obstacle rules. `FindEdgePath` traverses an arbitrary directed edge overlay with long-range and subunit-weight edges, so these pruning and heuristic assumptions do not hold. Reconsider only if HexxlaDB adds a separate implicit-grid routing API.
- **Incremental, anytime, and multi-agent planning** (LPA*/D*/ARA*/AD*, SIPP, CBS/ECBS, PIBT, LaCAM, MAPF-LNS) requires mutable obstacle timelines, reservations, agent objectives, or bounded-suboptimality contracts that HexxlaDB does not model. These belong in an application layer unless repeated database-native demand appears.
- **Contraction hierarchies, landmarks, and bidirectional routing indexes** add preprocessing and invalidation costs to a mutable graph; bidirectional search also needs an incoming-edge index that is not currently stored. Reconsider after representative graphs show path expansion, rather than edge decoding or I/O, is the bottleneck.
- **Alternative spatial indexes** (kd/R/R*-trees, quad/skip-quadtrees, BVHs, spatial hashes, learned multidimensional indexes) would duplicate the stable Morton-keyed B+ tree. The spatial-order benchmark showed mixed Morton reordering results, so there is no evidence for another production index or access-order rewrite.
- **Global DGGS/ellipsoidal coordinate systems and incompatible hierarchical encodings** (IGEO7, Hex9, lattice quadtrees) solve planetary geometry and sharding, not the bounded axial coordinate model. Adopting them would change `PackedCoord` and the engine format; the in-memory aperture-7 summary captures the useful hierarchy without that migration.
- **Domain algorithms** for hydrology, SLAM, meshing, signal processing, coverage planning, reinforcement learning, and neural hex convolutions remain application concerns. They should enter the library only with a concrete storage or retrieval requirement, deterministic semantics, and representative tests.

The persistent/content-bearing super-hex view remains a **future** candidate rather than a rejection because the shipped occupancy prototype now provides a compatible validation path. BMSSP remains separately deferred above until graph sizes justify its implementation complexity.
