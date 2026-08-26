# Roadmap

This file contains pending or deliberately deferred work. Completed work belongs in [`CHANGELOG.md`](../CHANGELOG.md); session-level tasks belong in [`TODO.md`](../TODO.md).

## Active limitation-reduction programme

These workstreams are ordered by implementation dependency. “Active” means each
is ready for bounded planning and evidence collection, not that an engine
redesign ships without first demonstrating the need. Complete and move one
workstream to `CHANGELOG.md` before beginning an overlapping implementation.
Workstream numbers remain stable when completed items are removed.

| README caveat | Active response |
| --- | --- |
| Serialized write throughput | Retain serialized correctness, use bounded caller batching, and rerun the write-path gates before considering commit-pipeline changes. |
| Measured HNSW envelope | Retain the measured 20,000×32d and 10,000×384d limits; rerun bounded vector evidence before raising them. |
| Sparse coordinates | Retain sparsity as a logical property; use authenticated page reuse, bounded tail reclaim, and explicit compaction for physical storage. |
| Caller-owned semantic placement | Retain this as a product boundary: applications own meaning and anchor selection; the database must not infer either. |
| Extend-only storage | Authenticated v3 reuses generation-safe pages and supports explicit tail reclaim; plaintext/legacy formats retain documented compaction. |
| Pre-v1 API stability | Preserve the candidate baseline while collecting the graduation evidence below. |

### 7. Eventual v1 graduation evidence

**Outcome:** Graduate only after production and release evidence supports the
already classified candidate API, without accelerating the version number.

- Preserve the exact exported-API baseline, migration notes, and at least one
  minor-release deprecation window except for urgent correctness or security
  failures.
- Complete a named limited-production adoption, recovery drill,
  deployment-specific supported-scale evidence, and signed release rehearsal on
  the eventual candidate commit.

**Completion evidence:** compatibility checks and migration notes pass, and
every measurable v1 gate in
[`VERSIONING.md`](../VERSIONING.md) has current evidence. Until then the project
remains on the `v0.y.z` line.

Sparse coordinates remain an intentional logical-namespace property: empty
coordinates consume no records and existing coordinates are never renumbered
for cosmetic density. Improvements belong in bounded allocation, diagnostics,
page reuse, and compaction rather than coordinate-space rewriting.

## Other near-term work

- **Bulk cell deletion** — add a `BatchPutCells`-style helper for bounded or chunked deletion, with MVCC-aware per-row outcomes, progress, and documented changelog behavior. Disk reclamation remains an explicit prune-then-compact operation.

- **Secondary-index contract isolation** — investigate the smallest test seam that exercises cell and seam secondary maintenance without requiring every assertion to construct a complete database. Keep receiver methods in the root package and avoid exporting raw transaction internals.

## Evidence-gated candidates

These are plausible extensions, not commitments. Promote one only when a representative workload establishes requirements and a measurable benefit.

- **Persistent, content-bearing super-hex summaries** — extend the rebuildable occupancy prototype only after aggregation, freshness, storage, and recovery semantics are demonstrated.
- **Push changefeed subscription** — in-process notification layered over the durable at-least-once log when polling is a demonstrated bottleneck.
- **Graph export** — a standard external representation when consumers need topology inspection without custom traversal code.
- **Record-encoding allocation reduction** — pool or capacity-hint encoding only after benchmarks show material write-path GC pressure.
- **Large-graph shortest paths** — reconsider advanced SSSP algorithms such as BMSSP only when graph sizes and profiles show Dijkstra expansion is the limiting cost. Current bounded workloads do not justify the implementation complexity.
- **Page-level corruption fault injection** — extend current decoder, WAL, overflow-chain, and B+ tree invariant tests with byte-level primary-file faults when a defined recovery or fail-closed requirement identifies coverage that structural unit tests do not provide.
- **Resource-failure injection** — add deterministic ENOSPC, sync, allocation, or file-descriptor fault seams only when an operator requirement defines the expected transaction outcome and recovery evidence; avoid non-reproducible host-exhaustion tests.
- **Complete same-slot replay detection** — prototype a Merkle or equivalent authenticated per-page generation catalog only when an adopter requires defense against replay of an older valid non-root page on hostile storage. Require bounded metadata growth, crash-consistent root publication, no recursive trust cycle, representative read/write amplification, migration, and external-anchor semantics before changing format v3.

## Research experiments

These experiments test research-derived hypotheses without committing to a public API, storage-format change, or production implementation. Stop an experiment when its representative workload shows no material benefit.

- **Record-granular caching and update buffering** — use *Bf-Tree: A Modern Read-Write-Optimized Concurrent Larger-Than-Memory Range Index* to design a larger-than-memory comparison of the current page cache with a bounded record/negative-cache prototype. Exercise real cell, secondary-index, MVCC, and HNSW record shapes under uniform and skewed point reads, scans, and mixed writes; report cache hits, bytes read/decrypted/copied, durable write amplification, p50/p95 latency, and throughput. Consider mini-page write buffering only if record-granular caching isolates fixed-page granularity as a material bottleneck without weakening WAL, recovery, encryption, or snapshot semantics.
- **Bidirectional exact shortest paths** — use *Bidirectional Search That Is Guaranteed to Meet in the Middle* to compare current Dijkstra with bidirectional Dijkstra on directed persisted graphs across scale, degree, path length, weight distribution, relation filters, cache state, and disconnected components. Require identical optimal costs and paths under caller-owned cost semantics; consider an incoming-edge index only when reduced expansions and latency materially exceed its write, storage, MVCC, and maintenance costs.
- **Hex-native spatial ordering and batched reads** — use *Hierarchical Hexagonal Clustering and Indexing* to compare current Morton ordering with Morton-sorted batched fetching and an offline Node-Gosper candidate across dense, sparse, clustered, and adversarial layouts. Measure page reads, cache misses, splits, write amplification, file size, and latency for rings, context, FOV, and summary rebuilds. Preserve nearest-first result semantics, and do not change `PackedCoord` or the on-disk format unless a format-migration proposal is justified by a material end-to-end gain.
- **Incremental FOV projection** — use *New Algorithms for Field of View on 2D Grids* to prototype a process-local visibility cache for repeated large-radius queries with moving origins or limited blocker changes. Compare full recomputation with incremental update, including memory use and invalidation after opacity, validity-time, and snapshot changes; retain the idea only if a representative repeated-FOV workload is bottlenecked by recomputation and the hex adaptation remains deterministic and correct.
- **Structured derivation provenance** — use *Provenance Semirings* and *The Rationale of PROV* to model one concrete lineage query over cells, facets, seams, or query results before designing storage or APIs. Compare the explanation value with added record size, index maintenance, write amplification, retention, and query cost; reject a general provenance algebra unless a bounded representation materially improves an identified audit or conflict-resolution workflow.
- **External hex-terrain analysis** — use *Priority-Flood: An Optimal Depression-Filling and Watershed-Labeling Algorithm for Digital Elevation Models* to build a read-only prototype over existing cells and edges for depression handling, flow direction, accumulation, and watershed labeling. Validate six-neighbor correctness, boundary/nodata semantics, provenance, memory bounds, and reproducibility on synthetic fixtures before deciding whether any reusable helper belongs in this repository; terrain mutation and simulation remain outside the transactional core.

## Engine and retention investigations

- **`DB.Path()`** — small API ergonomics candidate for embedders that must inspect the backing file.
- **Physical coordinate purge** — define whether removing the latest tombstone can coexist with explicit historical-snapshot guarantees before considering an API.
- **Changefeed-coordinated pruning** — consider alternative pruning modes only with a precise retained-sequence contract.

## Out of scope

- Distributed replication and high availability; HexxlaDB is an embedded single-owner database.
- Unbounded background primary-file rewriting or implicit compaction; maintenance remains explicit even if generation-safe page reuse and tail reclamation are added.
- Online re-encryption; rotation is offline.
- Pluggable SQLite or third-party key-value storage cores.
- Automatic truth assessment, confidence decay, relationship reinforcement, or other product-policy mutation inside the database.

## Research decisions

The research corpus contains useful techniques that do not match a demonstrated HexxlaDB storage requirement today:

- Grid route planners assume implicit movement and obstacle rules, while `FindEdgePath` traverses arbitrary directed stored edges.
- Incremental, anytime, and multi-agent planners require changing obstacle timelines, reservations, or agent objectives owned by an application layer.
- Contraction hierarchies, landmarks, and bidirectional routing add preprocessing or incoming-edge indexes whose maintenance cost is not justified by current graph profiles.
- Alternative spatial indexes would duplicate the Morton-keyed B+ tree without evidence of a better end-to-end access path.
- Planetary coordinate systems and incompatible hierarchy encodings solve a different coordinate domain and would change the stable packed representation.
- Hydrology, SLAM, meshing, signal processing, coverage planning, reinforcement learning, and neural hex convolution are application algorithms unless they produce a concrete database storage or retrieval requirement.

Revisit a decision only with a compatible data model, representative workload, and measurable acceptance criterion.
