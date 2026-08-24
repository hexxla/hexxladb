# Roadmap

This file contains pending or deliberately deferred work. Completed work belongs in [`CHANGELOG.md`](../CHANGELOG.md); session-level tasks belong in [`TODO.md`](../TODO.md).

## Limitation-remediation program

Execute these workstreams in order. Each workstream begins with a reproducible baseline and ends only when its stated evidence passes. A workstream may preserve an intentional product boundary, but it must leave users with an accurate contract and a supported operational path.

Completed workstreams are recorded in [`CHANGELOG.md`](../CHANGELOG.md). Remaining workstreams retain their original identifiers so session state and prior review references stay unambiguous.

### 6. Online backup and single-owner availability

**Outcome:** Preserve the embedded single-owner model while providing a supported consistent backup that does not require an unsafe live file copy.

- Specify a `BackupTo`-style snapshot contract, including writer blocking, cancellation, encryption credentials, WAL state, changelog inclusion, destination exclusivity, and restore validation.
- Reuse the consistent copy/compaction machinery where possible; do not introduce a server or replication subsystem to solve backup.
- Add restore tests from active MVCC and encrypted databases and failure tests for partial destinations.
- Keep cross-node replication and high availability explicitly out of scope unless a separate product requirement authorizes that architecture.

**Completion evidence:** the supported API produces a restorable point-in-time backup while the database remains open, never copies a mismatched primary/WAL pair, and clearly states the write pause and changefeed guarantees.

### 7. Durable changefeed consumers

**Outcome:** At-least-once consumers must have an optional durable cursor and a precise retention/backup relationship without pretending to provide distributed exactly-once processing.

- Define consumer identity, cursor monotonicity, compare-and-advance semantics, deletion, and corruption behavior.
- Store offsets in the authoritative database only if doing so does not recursively emit changefeed records; otherwise document the external-store contract.
- Coordinate minimum retained sequence with pruning, compaction, backup, and changelog replacement.
- Keep handlers idempotent and make push notification an optional wake-up mechanism rather than a second delivery truth.

**Completion evidence:** crash/restart tests resume without omission, duplicate delivery remains safe and documented, cursor regression is rejected, and compaction/backup cannot silently invalidate a registered consumer.

### 8. Search and vector scale

**Outcome:** Replace the approximate HNSW warning with a measured supported envelope and remove page-split pressure or fail predictably outside that envelope.

- Benchmark vector insertion, query recall/latency, reopen, delete/update churn, and file growth at 10K and larger representative sets across supported dimensions.
- Profile whether B+ tree page layout, HNSW graph maintenance, or encoding dominates before changing storage structures.
- Evaluate bulk index construction and graph-record layout before adding a new index. Add a lexical inverted index only if representative full-scan search misses an agreed latency target.
- Preserve the flat-scan fallback and report which path served a query when observability requires it.

**Completion evidence:** published evidence states tested scale, recall, latency, memory, and disk bounds; the prior 10K caveat is either removed by passing tests or replaced by an enforced, actionable limit.

### 9. Coordinate placement and lattice quality

**Outcome:** Applications must have a reproducible way to make hex proximity meaningful and to detect when vector and lattice neighborhoods diverge, without hidden database mutation.

- Define placement invariants, collision behavior, stable relocation/supersession rules, and the boundary between application policy and database primitives.
- Build a reference placement example using existing public APIs rather than embedding product policy in the engine.
- Measure useful context per token, neighborhood precision, stability under incremental insertion, and divergence between semantic seeds and lattice expansion.
- Add inspection/export tooling only when the evaluation shows that users cannot diagnose placement quality with existing walks and queries.

**Completion evidence:** a repeatable example produces and evaluates a lattice from a representative corpus, poor placement is observable, and the documentation states what HexxlaDB guarantees versus what the application must decide.

### 10. Product-policy and token-budget ergonomics

**Outcome:** Keep truth, confidence, contradiction detection, and ranking policy explicit while making correct application integration easier.

- Add an example adapter for a real model tokenizer and quantify the default byte budgeter's expected error; keep `ByteLenBudgeter` as an explicitly approximate dependency-free fallback.
- Demonstrate seam detection/resolution, confidence-aware ranking, and supersession as caller-owned policies with auditable writes.
- Add no automatic truth adjudication or hidden confidence mutation to the database. Improve interfaces only when the example exposes repeated, error-prone application plumbing.

**Completion evidence:** context packs respect a real tokenizer budget in tests, examples make policy ownership explicit, and no database operation mutates product meaning without a caller request.

### 11. Compatibility and v1 readiness

**Outcome:** Users must have a tested path from format v1 to MVCC-capable storage and a clear API/on-disk compatibility promise before v1.0.0.

- Define an offline, resumable v1-to-v2 migration built on logical copy with source preservation, destination exclusivity, progress, cancellation, encryption, and post-copy verification.
- Publish a compatibility matrix for library versions, format versions, encryption/changelog versions, and downgrade refusal.
- Inventory the root API, mark provisional areas, and require documented deprecation or migration notes for pre-v1 breaking changes.
- Pin a lint toolchain that can decode the minimum Go version's export data, and reconcile the standalone Gosec baseline with specific fixes or justified suppressions so local and hosted security checks enforce the same policy.
- Turn the existing v1 graduation criteria into measurable release gates; do not declare stability based on version number alone.

**Completion evidence:** migration fixtures restore equivalent visible data and indexes, interrupted migrations leave the source untouched, compatibility tests enforce open/refusal behavior, the complete lint and security suite runs without an unreconciled baseline on the supported Go toolchain, and every v1 gate has recorded evidence.

### Program sequencing

1. Complete MVCC and write-path measurement/fixes before setting scale claims.
2. Complete storage, backup, and durable-consumer workflows before expanding deployment guidance.
3. Complete search and lattice-quality evidence before adding new indexing or placement abstractions.
4. Complete migration and compatibility gates after the preceding contracts have stabilized.

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
- **Persistent changefeed consumers** — durable consumer offsets and retention coordination for materialized projections.
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
