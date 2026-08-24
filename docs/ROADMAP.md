# Roadmap

This file contains pending or deliberately deferred work. Completed work belongs in [`CHANGELOG.md`](../CHANGELOG.md); session-level tasks belong in [`TODO.md`](../TODO.md).

## Limitation-remediation program

Execute these workstreams in order. Each workstream begins with a reproducible baseline and ends only when its stated evidence passes. A workstream may preserve an intentional product boundary, but it must leave users with an accurate contract and a supported operational path.

Completed workstreams are recorded in [`CHANGELOG.md`](../CHANGELOG.md). Remaining workstreams retain their original identifiers so session state and prior review references stay unambiguous.

### 11. Compatibility and v1 readiness

**Outcome:** Users must have a tested path from format v1 to MVCC-capable storage and a clear API/on-disk compatibility promise before v1.0.0.

- Define an offline, resumable v1-to-v2 migration built on logical copy with source preservation, destination exclusivity, progress, cancellation, encryption, and post-copy verification.
- Publish a compatibility matrix for library versions, format versions, encryption/changelog versions, and downgrade refusal.
- Inventory the root API, mark provisional areas, and require documented deprecation or migration notes for pre-v1 breaking changes.
- Pin a lint toolchain that can decode the minimum Go version's export data, and reconcile the standalone Gosec baseline with specific fixes or justified suppressions so local and hosted security checks enforce the same policy.
- Turn the existing v1 graduation criteria into measurable release gates; do not declare stability based on version number alone.

**Completion evidence:** migration fixtures restore equivalent visible data and indexes, interrupted migrations leave the source untouched, compatibility tests enforce open/refusal behavior, the complete lint and security suite runs without an unreconciled baseline on the supported Go toolchain, and every v1 gate has recorded evidence.

### Program sequencing

1. Complete migration and compatibility gates after the preceding contracts have stabilized.

For every workstream: run the narrowest regression first, then package tests and race checks, then `task ci`; update only the owning reference documents and record completed behavior under `CHANGELOG.md` `[Unreleased]`.

## Deferred security-format work

### Authenticated primary pages

**Outcome:** Replace confidentiality-only AES-XTS data pages with a versioned authenticated format that detects primary-file modification without nonce reuse, silent downgrade, or weaker crash recovery.

- Define the page-level threat model and representative latency, throughput, space, and recovery budgets before selecting an AEAD construction.
- Persist a unique rewrite generation or nonce and authentication tag for every page image; bind the page identity, format version, and generation as associated data.
- Integrate authenticated page images with WAL replay and header publication so interruption at each write boundary recovers one valid generation and never reuses a nonce.
- Provide an offline, source-preserving migration and explicit downgrade refusal. Keep the current XTS format readable only through the documented legacy path.
- Exercise ciphertext, tag, nonce, generation, page-swap, WAL-swap, truncation, wrong-key, interrupted-migration, reopen, and backup/restore faults.

**Completion evidence:** all primary-page modifications fail closed before decoded data is returned; crash-injection tests recover a single authenticated state without nonce reuse; migration fixtures preserve logical data and indexes; and published benchmarks show the supported performance and space envelope.

## Near-term

- **Bulk cell deletion** — add a `BatchPutCells`-style helper for bounded or chunked deletion, with MVCC-aware per-row outcomes, progress, and documented changelog behavior. Disk reclamation remains an explicit prune-then-compact operation.

- **Secondary-index contract isolation** — investigate the smallest test seam that exercises cell and seam secondary maintenance without requiring every assertion to construct a complete database. Keep receiver methods in the root package and avoid exporting raw transaction internals.

## Evidence-gated candidates

These are plausible extensions, not commitments. Promote one only when a representative workload establishes requirements and a measurable benefit.

- **Persistent, content-bearing super-hex summaries** — extend the rebuildable occupancy prototype only after aggregation, freshness, storage, and recovery semantics are demonstrated.
- **Push changefeed subscription** — in-process notification layered over the durable at-least-once log when polling is a demonstrated bottleneck.
- **Graph export** — a standard external representation when consumers need topology inspection without custom traversal code.
- **Record-encoding allocation reduction** — pool or capacity-hint encoding only after benchmarks show material write-path GC pressure.
- **Large-graph shortest paths** — reconsider advanced SSSP algorithms such as BMSSP only when graph sizes and profiles show Dijkstra expansion is the limiting cost. Current bounded workloads do not justify the implementation complexity.

## Engine and retention investigations

- **`DB.Path()`** — small API ergonomics candidate for embedders that must inspect the backing file.
- **Partial file reclaim** — persistent freelist reuse, tail truncation, and platform-specific hole punching remain deferred until measured prune-then-compact windows miss an operator requirement. Any future design must generation-protect reused pages from stale WAL replay and prove allocator recovery before replacing the supported [`PruneCellVersions`](./hexxladb/OPERATIONS.md#mvcc-retention-and-pruning) followed by compaction path.
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
