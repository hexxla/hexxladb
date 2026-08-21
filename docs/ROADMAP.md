# Roadmap

This file contains pending or deliberately deferred work. Completed work belongs in [`CHANGELOG.md`](../CHANGELOG.md); session-level tasks belong in [`TODO.md`](../TODO.md).

## Near-term

- **Bulk cell deletion** — add a `BatchPutCells`-style helper for bounded or chunked deletion, with MVCC-aware per-row outcomes, progress, and documented changelog behavior. Disk reclamation remains an explicit prune-then-compact operation.

- **Secondary-index contract isolation** — investigate the smallest test seam that exercises cell and seam secondary maintenance without requiring every assertion to construct a complete database. Keep receiver methods in the root package and avoid exporting raw transaction internals.

## Evidence-gated candidates

These are plausible extensions, not commitments. Promote one only when a representative workload establishes requirements and a measurable benefit.

- **Persistent, content-bearing super-hex summaries** — extend the rebuildable occupancy prototype only after aggregation, freshness, storage, and recovery semantics are demonstrated.
- **Persistent changefeed consumers** — durable consumer offsets and retention coordination for materialized projections.
- **Push changefeed subscription** — in-process notification layered over the durable at-least-once log when polling is a demonstrated bottleneck.
- **Graph export** — a standard external representation when consumers need topology inspection without custom traversal code.
- **MVCC version lookup acceleration** — replace linear version selection only if profiles show long version chains materially affecting snapshot reads.
- **Record-encoding allocation reduction** — pool or capacity-hint encoding only after benchmarks show material write-path GC pressure.
- **Large-graph shortest paths** — reconsider advanced SSSP algorithms such as BMSSP only when graph sizes and profiles show Dijkstra expansion is the limiting cost. Current bounded workloads do not justify the implementation complexity.

## Engine and retention investigations

- **`DB.Path()`** — small API ergonomics candidate for embedders that must inspect the backing file.
- **Partial file reclaim** — evaluate freelist reuse, tail truncation, or platform-specific sparse-file techniques against the durability model. The supported path remains [`PruneCellVersions`](./hexxladb/OPERATIONS.md#mvcc-retention-and-pruning) followed by compaction.
- **Physical coordinate purge** — define whether removing the latest tombstone can coexist with explicit historical-snapshot guarantees before considering an API.
- **Changefeed-coordinated pruning** — consider alternative pruning modes only with a precise retained-sequence contract.

## Out of scope

- Distributed replication and high availability; HexxlaDB is an embedded single-owner database.
- Automatic primary-file shrinking; compaction is explicit.
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
