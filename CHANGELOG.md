# Changelog

## [Unreleased]

## [0.6.0] - 2026-08-26

### Added

- **Bounded retrieval controls** — added public context, spatial, seam-scan, and filtered-embedding candidate limits. `CellQuery.EmbeddingCandidateLimit` supports progressive filtered-ANN widening within `MaxEmbeddingFilterCandidates`, and `ErrSpatialScanLimit` reports seam secondary/result exhaustion without returning a misleading partial result.
- **Validated remote-access ownership boundary** — added a standard-library-only owner-service example that keeps the root library network-free and preserves exclusive database-file ownership. The loopback-only service requires authenticated-v3 encryption and constant-time bearer authentication, strictly bounds JSON, rate, concurrency, headers, and timeouts, and shuts down gracefully. HTTP boundary tests prove cell put/get while a competing file owner remains locked out. Its explicit threat model and operations guide require TLS termination and application-owned authorization, tenancy, audit, monitoring, and secret rotation; it is reference boundary code, not a production server or distributed database.
- **Candidate API compatibility gate** — every exported root file is now classified as a v1 candidate, consumer-facing signatures consistently spell root aliases instead of private implementation types, and `task api-check` compares the complete documented root surface plus aliased type definitions with a committed v0.6 baseline. Any additive, documentation, or signature change requires explicit review and baseline regeneration. This resolves local provisional-API uncertainty without claiming v1: named production adoption, operator recovery evidence, supported-scale regeneration, and a signed release rehearsal remain required.
- **Generation-safe page reuse and bounded tail reclaim** — authenticated format v3 now persists reusable B+ tree and overflow-page ids inside the authenticated transaction/WAL boundary, keeps small free sets in the header, and generation-links larger allocator metadata chains. Allocation consumes the lowest free id before extending; abort, reopen, stale-WAL, overflow replacement, duplicate release, and tree-collapse paths retain fail-closed behavior. `DB.ReclaimTail` durably lowers the allocator boundary before truncating a contiguous allocator-owned suffix, so injected crashes can leave only harmless excess bytes for a retry. `StorageStats` now distinguishes allocator and reusable pages. Plaintext and legacy formats remain extend-only, and compaction remains explicit for fragmentation and low page fill.
- **Authenticated primary-page format** ⚠️ **breaking pre-v1 creation behavior** — new databases opened with `EncryptionKey` or `Passphrase` now use MVCC engine format v3 with XChaCha20-Poly1305 page envelopes, an authenticated header/current-root generation, keyed WAL records, and an authenticated WAL header commit marker that recovers page and root publication together. Existing AES-XTS v1/v2 files remain readable without silent rewrite. `PreflightMigrateToAuthenticated`, `MigrateToAuthenticated`, and `hexxladb migrate-to-authenticated` create verified, source-preserving encrypted v3 candidates from v1 or v2 with independent credentials and explicit downgrade refusal. Fault coverage includes generation/nonce/ciphertext/tag modification, page-id and WAL substitution, stale-root replay, truncation, wrong keys, WAL-only root recovery, interrupted migration, backup/restore, compaction, and rotation. Same-slot replay of an older valid non-root page and coordinated recovery-set rollback remain documented external-anchor limits.
- **Rebuildable HNSW lifecycle** — `PutEmbeddingWithOptions` can defer graph maintenance during bounded bulk ingestion while keeping authoritative vectors exactly searchable. `RebuildEmbeddingIndex` preflights vector count, a conservative memory/transient-WAL budget, and filesystem capacity; advances a persisted revision; builds and structurally validates HNSW outside the write transaction; and atomically publishes only if no embedding changed after the snapshot. Cancellation, concurrent mutation, corrupt lifecycle state, and failed publication preserve the old graph records and keep exact flat search active. Defaults cap rebuilds at 10,000 vectors and 2 GiB estimated peak usage; the evidence-backed hard vector limit is 20,000.
- **Embedding-index downgrade constraint** ⚠️ **breaking pre-v1** — databases with deferred embedding writes now carry an `hnsw/state` lifecycle row. Older releases preserve but do not interpret that row and may query the stale graph while it is dirty; complete `RebuildEmbeddingIndex` before downgrading. Clean indexes and database engine formats remain compatible.
- **Embedding-aware compaction** — copy-compaction now propagates the source embedding dimension and distance metric into the destination header. Previously the embedding and HNSW rows were copied but vector search was disabled after opening the compacted database.
- **Bounded write-path evidence** — added an aggregate-only runner for ordinary MVCC commits, 100-cell batches, and 32-dimensional cell/embedding commits, with p50/p95/p99 latency, throughput, allocation, heap, primary-growth, write-phase, and WAL-batch measurements against conservative reference-host gates. Workloads and temporary storage are hard-bounded, temporary databases are removed, and benchmark output now attributes lock, callback, durability, and finalization time through `WriteStats`.
- **Source-preserving maintenance orchestration** — added `PreflightCompactTo` and `PreflightMigrateV1ToV2` with canonical path/collision checks, source storage reporting, exact migration format/credential/changelog/resume validation, and conservative filesystem-capacity gates. Migration snapshots now default to the destination filesystem and recheck capacity while the source lock is held, avoiding accidental unbounded use of system temporary storage; `MigrationOptions` adds `SnapshotDirectory` and `OnPreflight`. `CompactOptions.VerifyDestination` fails and removes an unhealthy candidate, while an incomplete feature bit makes interrupted compaction artifacts fail closed through `ErrCompactionIncomplete`. The operator CLI adds resumable `migrate-v1-to-v2` and upgrades `compact` with dry runs, bounded durable progress, signal cancellation, environment-only passphrase/base64-key input, candidate reopen/health verification, and explicit source/rollback retention guidance; neither command replaces or deletes the source.
- **Bounded cell placement** — `Tx.FindFreeCellPlacement` selects the first coordinate without a visible cell in deterministic ring order around a caller-owned semantic anchor. The writable-only helper returns both coordinate forms and the occupied-probe count, rejects unbounded or out-of-range searches, reports bounded exhaustion through `ErrNoFreeCellPlacement`, composes with `PutCell` in the same update without a writer race, and preserves prior MVCC values when reusing a tombstoned coordinate. The placement evidence example now uses this public API instead of a duplicated allocation loop.
- **Conservative pilot qualification** — added a bounded five-minute, minimum-sample reference workload for the documented Linux/amd64 production profile. It exercises 10,000 encrypted MVCC cells and 32-dimensional HNSW vectors under a 95/5 read/write mix with separately gated cell writes and vector updates, durable changelog consumption, encrypted backup/restore, primary reopen, health checks, explicit latency/throughput/RPO/RTO gates, and hard 1 GiB heap and 2 GiB storage caps. The aggregate-only report and exact temporary-directory cleanup make release-host evidence reproducible without opening an existing database.
- **Offline format-v1 to MVCC migration** — `MigrateV1ToV2` performs a source-preserving logical copy into an exclusively created format-v2 destination, rebuilding mutable primary and secondary indexes while retaining edges, embeddings/HNSW, raw application rows, storage limits, and embedding configuration. Batch checkpoints are atomic with copied rows, cancellation is resumable only against the same source digest, incomplete destinations are refused by ordinary `Open`, independent source/destination encryption is supported, changelog reset requires explicit authorization, and a full logical verification precedes publication.
- **Compatibility and v1 readiness contract** — `VERSIONING.md` now records library/engine/encryption/changelog compatibility, explicit newer-format refusal, a root-API stability inventory, required pre-v1 migration notes, and evidence-backed v1 release gates. The lint/security toolchain is pinned to Go 1.27.0 and reviewed analyzer versions through one local/hosted runner, eliminating skipped PATH-only checks and `@latest` policy drift.
- **Durable changefeed consumers** — named consumer cursors are stored as authoritative, non-emitting primary metadata with monotonic compare-and-advance and explicit deletion. Restart tests prove at-least-once redelivery without omission; a minimum acknowledged sequence exposes the advisory retention floor. The primary also records a representation-independent logical-history digest, so backup preserves registered consumers while missing or replaced changelogs fail closed on open instead of silently resuming against unrelated history. Compaction recovery and explicit re-bootstrap are documented; exactly-once handlers and in-process push delivery remain outside the database contract.
- **Consistent online backup** — `DB.BackupTo` captures an open database's matched primary and WAL plus its complete enabled changelog under one read lock, preserving encrypted bytes and MVCC history without retaining credentials or changing the on-disk format. Writes pause for the bounded, cancellable file copy; destination components are exclusive and failed calls remove only newly created files. Active MVCC, encrypted restore, lazy at-least-once redelivery, writer exclusion, destination collision, cancellation, and health-check coverage define the supported recovery contract while replication and high availability remain out of scope.
- **Observable bounded storage maintenance** — `DB.StorageStats` now reports primary, WAL, and optional changelog sizes plus persistent page-graph reachability and whole-page reclaimable bytes. `CompactWithOptions` and `CompactToWithOptions` keep copy transactions at or below 4096 keys and emit cumulative progress only after a destination batch is durable; cancellation removes partial output so operators can retry safely. A deterministic puts/tombstones/prune/compact workload established the reclamation baseline later used to validate authenticated page reuse and bounded tail reclaim.
- **Vector-search observability and scale evidence** — `SearchByEmbeddingWithStats` reports whether HNSW or the exact flat fallback served a query and exposes the effective search breadth; `EmbeddingSearchConfig.EfSearch` provides bounded recall/latency tuning with a dimension-aware default. A deterministic aggregate-only runner measures build throughput, exact-oracle recall@k, latency, close/reopen, update/delete churn, memory, and storage at 10,000 vectors. Persisted HNSW levels are now deterministic for the same coordinates and insertion order; the on-disk encoding is unchanged.
- **Reproducible lattice placement guidance and evidence** — a public-API-only example compares stable first-free topic clustering with intentionally interleaved placement, checks collisions before writes, preserves initial coordinates during incremental insertion, and models relocation as an explicit successor plus supersession seam. Its result-bounded report measures neighborhood precision, useful-content fraction, semantic precision, semantic/lattice topic-distribution divergence, and diagnostic grids. The evidence showed existing walks, context loading, vector search, and rendering are sufficient to expose poor placement, so no placement engine or new inspection API was added.
- **Public write-path timing** — `DB.WriteStats` returns cumulative accepted-call and committed-transaction counts plus time spent waiting for the database lock, running callbacks, making the authoritative engine commit durable, and completing post-commit projection/finalization. Snapshots are lock-free and remain available after close so embedders can export interval metrics without a library-owned metrics subsystem.
- **Editor and Codex project workflow** — tracked VS Code and Zed settings expose the repository's existing build and verification commands, and `.agents/skills/` is reserved for focused HexxlaDB-specific Codex skills. The obsolete Windsurf configuration and generated multi-assistant setup scripts have been removed.
- **Aperture-7 super-hex occupancy summaries** — `NewSuperHexSummaryIndex` builds a consistent in-memory materialized index from a database snapshot, then `Sync` incrementally applies the existing logical changelog. `Summary`, `SummaryForCoord`, and `Summaries` provide O(1) occupancy reads and deterministic exports; `LastSeq` exposes the applied changelog cursor. The derived index is rebuildable and does not change `PackedCoord` or the on-disk engine format.
- **Spatial and graph benchmark matrices** — compare ring-order vs Morton-order point reads across radius, density, and page-cache modes; measure `FindEdgePath` as graph out-degree grows.
- **Reproducible spatial evidence suite** — `task evidence-controlled` runs the seeded super-hex oracle soak plus focused Dijkstra, deterministic FOV, and super-hex benchmarks; `task evidence-observe` emits aggregate-only JSON for a bounded production-style synthetic workload.
- **Bounded changelog cursor reads** — sparse in-memory sequence-to-offset checkpoints are built during the existing validation scan and maintained after appends. Tail reads now decode at most 255 historical frames before the requested cursor instead of rescanning the full changelog; the public API and on-disk format are unchanged.

### Performance

- **Bounded authenticated churn** — five 100-cycle same-host samples compared the authenticated extend-only control with generation-safe reuse for a 20 KiB incompressible overflow value. Reuse reduced steady-state primary growth from exactly 29,008 bytes per delete/put cycle to zero after the allocator metadata high-water mark. Median latency was 180.2 µs versus 186.8 µs for the control (overlapping ranges, treated as unchanged); allocations rose from a median 217 to 232 per cycle due to bounded freelist materialization.
- **Authenticated-page evidence** — five-sample page-transform benchmarks on the reference i9-14900HX found XChaCha20-Poly1305 materially faster than legacy AES-XTS at 4 KiB and 64 KiB with the same one-allocation count. The 48-byte envelope adds 1.171875% per 4 KiB data page. A five-sample public point-read comparison measured authenticated MVCC v3 within 0.55% of plaintext MVCC v2 with identical allocation counts; the paired control attributes the larger absolute lookup allocation count to the existing MVCC version-seek path rather than encryption.
- **Faster bounded HNSW bulk construction** — five interleaved 1,000×32d runs improved median end-to-end build throughput 8.9× and reduced cumulative allocation 11.5× with unchanged recall, query-latency, and steady-heap ranges. Retained scale evidence passes 20,000×32d at .992 recall and 10,000×384d at .952 recall before/reopen (.956 after churn), with sampled peak build heap and temporary storage explicitly bounded and cleaned.
- **Lower write-page allocation** — engine write transactions now retain their one owned page copy when an unencrypted or in-place transform returns it, instead of cloning the same page twice. A transform returning separate storage still receives a defensive clone, preserving caller/hook ownership. Five same-host Btrfs runs reduced median allocation by 11.3% for single-cell MVCC commits and 16.8% per cell in 100-cell commits while retaining one WAL sync per commit, zero public multi-job batches, unchanged file growth, and passing race and crash-barrier checks.
- **Bounded HNSW and B+ tree read amplification** — page-cache hits now copy directly into pooled caller buffers, B+ tree point reads select only the required child or value without materializing every page entry, and one graph operation caches decoded HNSW metadata, nodes, and vectors. The finished 500×32d search benchmark measured about 14.8 ms, 16.4 MB, and 3,966 allocations versus the 72.1 ms, 133.0 MB, and 514,980-allocation baseline. On the reference 10,000-vector runs with 4 KiB pages and a 64 MiB cache, recall@10 measured 0.992 at 32 dimensions and 0.956 at 384 dimensions before reopen, remaining 0.992 and 0.960 respectively after update/delete churn and reopen.
- **Immediate default group-WAL flush** — the zero-value `GroupWALMaxBatchWait` no longer inserts an ineffective 2 ms collection delay into serialized public updates. Explicit positive waits remain available for direct engine users that can enqueue concurrent jobs. In the controlled matrix, minimal single-write latency fell from about 2.24 ms to 75–81 µs and reader blocking with a fixed 1 ms callback fell from about 3.42 ms to 1.25–1.46 ms; public operations continued to report one batch, zero multi-job batches, and one WAL sync. Full indexed-record evidence still shows `BatchPutCells` at about 0.063 ms/cell versus about 0.53 ms for individual MVCC writes. Durability ordering and lock scope are unchanged.
- **Bounded MVCC hot-key lookup** — cell, facet, and seam point reads now reverse-seek to the greatest version at or below the transaction snapshot instead of scanning and copying the full version chain. The benchmark matrix covers latest and historical snapshots through 6,000 versions; the 6,000-version latest case improved from about 2.02 ms and 3.43 MB allocated per lookup to about 13.4 µs and 31 KB on the reference host, with snapshot, tombstone, secondary-index, prune, race, and reopen coverage retained. No public API or on-disk format change.

### Changed

- **Documentation and runnable examples** — align MVCC guidance across plaintext v2 and authenticated v3, describe `SnapshotDiff` as a retained-history diagnostic rather than complete CDC, direct backups to `DB.BackupTo` instead of key rotation, correct current encryption and page-allocation descriptions, and align evidence/CLI guidance with implemented bounds and output.
- **Complete public API guidance** — package and task-oriented documentation now covers streaming cell JSON transfer and its partial-commit boundary, raw spatial compatibility scans, density/tag diagnostics and their work limits, embedding introspection, snapshot enumeration, durable changefeed cursor operations, MVCC prune planning, and interrupted-rotation recovery.
- **Strict typed-write invariants** ⚠️ **breaking pre-v1** — cell, facet, edge, seam, embedding, delete, endpoint, and cluster-hint coordinates must now be valid `Pack` outputs; provenance confidence, edge weights, and seam confidence deltas must be finite. Invalid typed writes return `ErrInvalidArgument` before changing storage. Raw application key/value rows remain unchanged.
- **Explicitly bounded spatial work** ⚠️ **breaking pre-v1** — context loads now cap radius, seeds, results, hops, and combined coordinate probes; raw/ring/seam queries preflight the complete packable region and enforce documented radius, secondary-row, and result limits. Eager coordinate disks and per-ring allocations were replaced with lazy iterators.
- **Exact large-radius context** ⚠️ **breaking pre-v1** — removed automatic LOD coordinate substitution from `LoadContext`. Large radial loads now retain exact stored coordinates in deterministic nearest-first order; the independent coordinate-coarsening math utility remains available internally for future explicit multiresolution experiments.
- **Production correctness and release hardening** — hardened retained-history snapshot diagnostics, MVCC tag analytics, weighted Voronoi assignment, bounded queries and spatial work, embedding decoding/reindexing, A* traversal, lexical scoring, template generation, and UTF-8 rendering against corrupt, stale, duplicate, or unbounded inputs. New database, WAL, changelog, backup, and encryption-rotation paths now persist affected directory entries; interrupted rotation fails closed and has an explicit configuration-checked recovery path. Release automation is least-privilege, immutable-action pinned, tool-version pinned, GPG checksum-signed, and produces per-archive SPDX SBOMs after CI, integration, fuzz, and cross-build gates.
- **Deterministic validation gates** — the serialized race suite now has an explicit 30-minute package timeout and an isolated, run-scoped repository-local temporary workspace. Mutation analysis runs from a 256 MiB/20,000-file Git-visible source snapshot, keeps Gremlins worker storage outside that snapshot, times out each target, cleans successful runs, and refuses to stack work on an interrupted run. Cascading-tree integrity probes use bounded production-style write transactions while preserving their split, reachability, balance, ordering, and reopen assertions. Format and complexity discovery respect ignored/generated trees, and analyzer execution fails closed instead of treating missing output as success. Integration selects the consistently named `TestIntegration_` set, fuzz commands select only their named fuzz targets, and all workflow actions and installed tools are immutably versioned.
- **Published-format compatibility evidence** — added a SHA-256-locked primary/WAL fixture generated by the published v0.5.1 source and verified its migration to format v2, including cell, raw application, facet, edge, source, tag, and time-index data.
- **Research experiment roadmap** — added evidence-gated experiments for Bf-tree-inspired record caching, bidirectional shortest paths, hex-native spatial ordering, incremental FOV, structured derivation provenance, and external terrain analysis. Each experiment names its informing paper and defines representative measurements and stopping conditions without committing to a public API or storage-format change.
- **Bounded production-readiness profile** — defined the initial Linux/amd64, single-owner embedded deployment envelope, its measured vector and write boundaries, required site-specific SLO/RPO/RTO declarations and operational drills, and explicit exclusions. Production readiness remains separate from the eventual v1 API commitment.
- **Go 1.27 minimum** ⚠️ **breaking pre-v1** — the module, CI, and pinned analyzer runner now require Go 1.27.0 or newer; users must upgrade their Go toolchain before building this release, and macOS builds consequently require macOS 13 or newer. Existing database files remain format-compatible. An interleaved 10-sample comparison against Go 1.26.7 found statistically significant read and compression improvements, no significant durable-write latency change, and one small HNSW encoding microbenchmark regression; the method and trade-offs are recorded in `docs/hexxladb/PERFORMANCE_EVIDENCE.md`.
- **Current examples and active references** — all nine maintained examples compile through the stable module-root API, the build task produces every example binary, and the example index includes each interactive, service-boundary, and evidence workload. Seeded HexxlaDB facts, configuration snippets, changefeed operations, migration errors, package guidance, and maintained engine-format documents match this release; the obsolete standalone mega-stress plan was consolidated into the roadmap.
- **Current root guidance** — the README quick start handles query, context, and transaction errors without assuming two results; feature claims distinguish opt-in MVCC, supersession, seams, and changefeeds; encryption guidance distinguishes authenticated v3 from legacy confidentiality-only pages; and write-performance context matches the published benchmark table.
- **Provider-neutral context retrieval** ⚠️ **breaking pre-v1** — `LoadContextConfig.MaxCells` replaces `MaxTokens` and `Budgeter`; `ContextAssemblyConfig` replaces `LoadContextBudgetConfig`; and token-specific `TokenBudgeter`, `ByteLenBudgeter`, `TruncateCellViewsToTokenBudget`, `ContextPack.TotalTokens`, explanation token counts, and budget-eviction statistics have been removed. Single-seed ring retrieval is deterministically nearest-first and multi-seed retrieval is deduplicated round-robin in caller-supplied seed order under one result limit. HexxlaDB no longer interprets confidence or approximates model tokens; the LLM example demonstrates caller-owned confidence ranking and checks the fully rendered request with an application-supplied provider/model counter.
- **Task-based repository workflow** — replaced the Makefile with `Taskfile.yml` while preserving the existing CI, test, benchmark, evidence, build, demo, fuzz, mutation, and pre-commit task names and variable overrides. GitHub Actions pins Task v3.53.1, and VS Code, Zed, contributor guidance, examples, and operational documentation now use `task`. No public Go API or on-disk format change.
- **Documentation ownership and scope** — replaced the inherited service-template architecture guide with the actual HexxlaDB package-boundary contract; reduced the manually duplicated exported-symbol inventory to a task-oriented API guide; separated product memory concepts from the database storage contract; removed milestone labels, completed-plan history, and speculative backlog material from current reference documents; and made `ROADMAP.md`, `TODO.md`, and `CHANGELOG.md` the sole homes for deferred work, session state, and completed history respectively. No public API or on-disk format change.

### Fixed

- **Fail-closed health and HNSW integrity** — `HealthCheck` now decodes visible cell and seam primaries, verifies key/record identities, rejects malformed secondary keys, and documents that its stable snapshot blocks writers. HNSW metadata, entry, node, neighbor, vector, and missing-component errors now propagate as `ErrCorruptDatabase` rather than returning partial or empty successful searches.
- **Cost-aware query planning and filtered recall** — small spatial disks now take precedence over unknown-cardinality source/tag scans, reducing the 2,000-cell combined benchmark from the prior approximately 62 ms plan to a 128 µs median (126–201 µs range) in five focused samples. Filtered embedding queries widen progressively so qualifying candidates beyond the former fixed `2×MaxResults` window can be found.
- **Lean storage and geometry paths** — removed a dead test-only leaf split implementation and its stale cascading-split TODO, removed test-only cascade result helpers, reused the canonical coordinate-distance method, and made `WalkRings` reuse `RingInto` rather than allocating each ring separately.
- **Current benchmark profile** — embedding API benchmarks and the LLM context example now use the validated 4 KiB HNSW page profile instead of retaining the obsolete 64 KiB setting, and benchmark fixtures fail on setup or query errors rather than emitting invalid measurements.
- **True cross-platform build qualification** — `task build-all` now propagates each requested `GOOS`, `GOARCH`, output directory, and Windows executable suffix through the CLI, TUI, and every example build instead of relabelling repeated host builds as macOS and Windows. `BUILD_ROOT` can isolate the three platform trees outside the repository during release validation.
- **Reconciled lint and CLI maintenance baseline** — pinned analyzers exposed and fixed changelog error-cause wrapping, provider-prompt slice aliasing, stale CLI use of deprecated overflow-only waste counters, HTTP no-body construction, and mechanical modernization findings. Standalone Gosec now uses the same medium-severity/confidence policy locally and in hosted security checks, with G115 retained under golangci-lint's line-specific justification policy. Complexity checks now classify changelog state machines explicitly and calculate receiver-method CRAP coverage correctly.
- **Unified context assembly** — ordinary single-seed `LoadContext` now propagates `LoadContextConfig.AsOf`, skips out-of-window cells, and continues scanning until the result limit is filled or the bounded walk ends. Graph and LOD dispatch now share the same validity, supersession, explanation, and seam assembly path, while multi-seed assembly preserves and deduplicates requested seams instead of dropping them during merge.
- **Encrypted changelog confidentiality and integrity** — encrypted databases now write logical changelog format v2 with XChaCha20-Poly1305 frames and database/log-specific derived keys, so record keys and inline payloads are no longer exposed beside encrypted data pages. Headers, frame lengths, clear sequences, wrong keys, swapped logs, and modified frames fail closed; legacy plaintext logs are rejected deterministically instead of receiving mixed-format appends. Offline encryption rotation preserves and re-encrypts changelog history when changelog settings remain enabled at the same path. Legacy AES-XTS v1/v2 primary pages remain confidentiality-only; new encrypted databases use authenticated v3.
- **Recoverable commit and changefeed finalization** — `CommitSeq`, versioned data, and bounded logical-event intents now share one engine commit. The external changelog is a recoverable primary-outbox projection, so append failures and process death after the engine commit converge on reopen without permanent feed gaps or sequence reuse. Known-durable projection failures additionally match `ErrCommitDurable` and block further writes until reopen; a crash after append but before acknowledgement may redeliver the same idempotent operation. Lazy mode retains intents until a bounded sync/cleanup barrier, and incomplete unacknowledged tail frames can be reconstructed from primary state.
- **Concurrent group-WAL update integrity** — public `Update` and `Batch` calls now remain exclusively serialized through engine commit and DB-level finalization, preventing a later writer from building on staged B+ tree state and losing either successful update. This is a pre-v1 behavioral compatibility change: views and writers no longer overlap a public update's commit wait. A regression test verifies both writes and commit sequences before and after reopen.
- **Stable seam API boundary** — `SeamRecord` is now exported from the root package and used by public seam writes, queries, hooks, secondary walks, and snapshot diffs, so external modules no longer need the inaccessible `internal/record` package.
- **Release and integration validation** — release jobs now run every tool required by the core CI script through the pinned repository tool runner, while scheduled integration jobs install only the additional tools required by `task ci-full`. The race-enabled integration task also has an explicit 30-minute budget for its 6,000-commit MVCC churn case, preventing deterministic missing-command and default-timeout failures.
- **Enforced security scans** — Trivy and Gosec now fail their jobs on unsuppressed findings, always retain SARIF reports as workflow artifacts, use pinned reviewed scanner versions, and run with only read access to repository contents.
- **Bounded Ollama calls** — the TUI and LLM context-engine example now use explicit 10-second HTTP clients and context-bearing requests for health and embedding calls, preventing an unavailable local model server from hanging commands indefinitely.
- **Accurate public guidance** — invalid `MaxValueBytes` errors now list the complete supported range through 1 MiB and retain the engine cause, while the README storage example uses only root-package coordinate and record types and handles write errors.
- **Super-hex error chains** — coordinate and changelog decoding failures now retain both the public classification error and their underlying cause for `errors.Is`/`errors.As` inspection.
- **Editor lint configuration** — corrected the case-sensitive Go extension identifier used by VS Code, prevented the optional Trunk extension from auto-initializing an incompatible local toolchain, added repository Markdown lint rules for intentional changelog and HTML patterns, normalized tracked documentation formatting, and repaired shell diagnostics including a syntax error in the coverage hook.
- **Exclusive database ownership** — `Open` now takes a non-blocking OS file lock and returns `ErrDatabaseLocked` when the same primary file is already open. This prevents two handles or processes from independently replaying and overwriting the shared primary/WAL state.
- **Safe, exact compaction** — `DB.Compact` reads one stable snapshot for the full copy; `CompactTo` owns the source exclusively; destinations are created with exclusive-create semantics so an existing file is never overwritten or deleted; copying is bounded to 4096-record batches without creating synthetic MVCC commit-timeline rows. Encrypted open-handle compaction now fails closed with `ErrEncryptionKeyRequired`; use offline `CompactTo` with credentials.
- **Complete query execution** — zero `CellQuery.MaxResults` now means unlimited as documented, unconstrained fallback scans cover the complete primary keyspace instead of radius 32, temporal queries correctly include pre-epoch timestamps, and unlimited embedding queries use an exact complete scan.
- **Graph and numeric correctness** — edge pathfinding is optimal for any finite positive weight (including weights below 1), traversal/decode errors are propagated, and non-finite edge weights, custom costs, and embedding components are rejected. Each expanded coordinate now performs one outgoing-edge scan; parallel relation types to the same destination are deduplicated and the minimum applicable stored weight is used.
- **Deterministic FOV budgets** — shadowcasting results are ordered by distance and coordinate before `MaxCells` is applied, so capped context loads consistently prefer the nearest visible cells.
- **Boundary-safe spatial APIs** — ring-density, untagged export, JSON export, and hex rendering validate packable coordinate/radius bounds and return `ErrInvalidArgument` instead of panicking.
- **Data and audit integrity** — batched writes report success and invoke progress only after commit; tag co-occurrences count each cell once; filtered changelog reads paginate past non-matches; snapshot diffs include cell tombstones as `DiffOpDelete` and report corrupt cell versions; JSON imports stream in bounded batches and reject non-array input.
- **Closed-handle consistency** — embedding configuration accessors remain safe after close, and `SnapshotDiff` returns `ErrDatabaseClosed` instead of dereferencing a closed engine.

---

## [0.5.1] - 2026-06-01

### Fixed

- **B+ tree cascading split completed (latent corruption removed)** — `cascadingLeafSplit` previously split a leaf only once and, on impossible inputs, force-wrote an oversized page that `buildLeafPage` rejected with `ErrCorruptTree: leaf page full`; its companion `insertIntoInternalCascade` was dead code, so multi-page leaf splits could orphan pages from the parent index. The split path is now a true bbolt-style spill: `splitRecursively` greedily left-fills so **every** emitted leaf page is guaranteed to fit; `insertAt`/`insertIntoInternal` thread all promoted children (`[]childRef`) up the tree; internal nodes spill into multiple fitting pages via `spillInternal`/`splitInternalGroups`; and `growRoot` adds one or more root levels as needed. The invariant _"every page serializes within pageSize and every page is reachable top-down"_ now holds for arbitrary inline-value size distributions, not just the bounded single-insert case. Removed dead `findOptimalSplit`, `splitInternal`, `splitInternalCascade`, `insertIntoInternalCascade`. New tests: `TestProbe_*`, `TestCascadeIntegrity_*` (full tree validator: balance, parent-pointer linkage, page-fit, top-down reachability, reopen) in `internal/engine`. No public API or on-disk format change.
- **Compression-magic value collision (round-trip corruption)** — a raw value whose first byte equals the compression magic (`0xFE`) and is longer than the 5-byte envelope header was misread as a compression envelope on read, failing with `flate: corrupt input`. `compressValue` now wraps such values in the envelope even when compression does not shrink them, guaranteeing byte-for-byte round-trip for arbitrary value bytes (e.g. embedding floats). Format-compatible; regression test `TestProbe_CompressMagicCollision`.

---

## [0.5.0] - 2026-05-09

### Added

- **Lazy ring iterators** (`internal/lattice`) — `RingSeq`, `WalkRingsSeq`, `SpiralRangeSeq`, `WalkRingsPackedSeq`, `SpiralRangePackedSeq` return `iter.Seq[Coord]` / `iter.Seq[CoordPacked]`. No backing slice is ever allocated; callers break early when a budget is met. For `MaxRing=100` (31,401 cells) with a 16-cell budget, zero ring computation happens beyond the 16th cell — the same early-exit discipline Badger applies in its `Iterator` (Seek/Next cursor).
- **Streaming context assembly** (`internal/views`) — `assembleCoordsIntoContextPack` replaced with a sequential streaming loop: one cell read + assembled + budget-tested at a time. Assembly stops the moment the token budget is full; no goroutine fan-out, no full-slice sort, no cells assembled beyond what fits in the response. Coords are processed in ring order (nearest-first) which is already the correct priority for conversational memory.
- **Streaming LOD coord collection** — `collectLODCoords` now uses `WalkRingsPackedSeq` / `SpiralRangePackedSeq` for both inner and outer ring walks, eliminating the `O(3r²+3r+1)` packed-coord slice allocation for large `MaxRing` values.
- **Fused query scan pipeline** (`query_exec.go`) — `QueryCells` / `SearchCells` no longer materialise an intermediate `[]CellRecord` candidates slice. Tag, source, time, and radius scan paths now use a single-pass fused callback: each record is filtered and scored inline as it arrives from the B+ tree cursor, and only passing records enter the sort. The radius scanner (`scanByRadiusFused`) uses `WalkRingsPackedSeq` — a lazy `iter.Seq` — so no `O(3r²)` packed-coord slice is allocated for the scan radius. `scanByEmbedding` is unchanged (bounded `MaxResults×2`, needs the scores map).
- **`docs/context/STREAMING.md`** — new design document describing the full streaming architecture, per-layer status, data-flow diagrams, and what cannot be fully streamed (sorted query results require seeing all candidates before ordering).

- **`SpiralRange`** (`internal/lattice`) — annular ring walk `[minR, maxR]` from a center; O(cells) with no intermediate allocations. Replaces manual ring loops in `collectLODCoords`.
- **`FieldOfViewShadowcast`** (`internal/lattice`) — symmetric shadowcasting FOV (Albert Ford 2021, hex adaptation). Six sextants, explicit-stack iterative scan, O(visible cells). `FieldOfView` now delegates here. Original raycasting retained as `FieldOfViewRaycast` for regression comparison.
- **`WeightFunc`** (`internal/lattice`) — optional traversal-cost function for Voronoi. `Voronoi` upgraded to multi-source Dijkstra; `nil` WeightFunc preserves uniform-BFS behaviour.
- **`EuclideanHeuristic`** (`internal/pathfind`) — Euclidean distance in axial 2D embedding: `sqrt(dq²+dr²+dq·dr)`. Admissible for edge weights ≥ 1; provides tighter lower bound than `HexDistanceHeuristic` on diagonal paths. `FindEdgePath` now uses it by default.
- **`FindEdgePathConfig`** — config struct replacing `FindEdgePath`'s positional `filter`/`maxExpand` args. New `CostFunc func(from, to Coord) float64` field lets callers override edge-weight-based traversal cost (e.g. inject confidence, recency, or semantic distance). `nil` = existing edge-weight behaviour.
- **`VoronoiWeightFunc`** / **`VoronoiContextConfig.WeightFunc`** — exposes the internal `lattice.WeightFunc` on the public Voronoi config. Callers steer region boundaries by cost; `nil` = uniform geometric Voronoi.

- **`MVCCStats.WastedBytes`** — cumulative logical byte size of freed overflow-page chains since the DB was opened. In-memory counter (resets on reopen). Non-zero signals that `CompactTo` is warranted to reclaim dead space. Surfaced by both `DB.StatsMVCC` and `DB.HealthCheck`.

### Changed

- **`Voronoi` signature** — third parameter `weightFn WeightFunc` added (internal package only, no public API surface change). All callers updated to pass `nil`.
- **`Tx.FindEdgePath` signature** ⚠️ **breaking** — positional `(filter string, maxExpand int)` replaced by `FindEdgePathConfig` struct. Migrate: `tx.FindEdgePath(ctx, a, b, "", 0)` → `tx.FindEdgePath(ctx, a, b, hexxladb.FindEdgePathConfig{})`. All call sites updated.

### Performance

- **`Tx.AscendRange`** in read-only transactions no longer issues a `pread(page0)` per call — it uses the transaction's cached `BTreeRoot`. Each `QueryCells` / `SearchCells` / `HealthCheck` that previously issued N header reads now issues zero (N typically 3–10 per query).
- **`DB.ViewAtTime`** now resolves the wall-clock snapshot via a reverse B+ tree walk (`DescendRangeFromRoot`) and stops at the first hit. Previously O(commits before asOf); now O(log N) for recent queries.
- **`DB.HealthCheck`** merged the separate `StatsMVCC` full cell-scan into `healthScanCells` — one pass instead of two.
- **`CompactTo`** opens the source database with the page cache disabled (`PageCacheSize: -1`); a read-once sequential scan gets zero cache benefit and previously displaced 4 MiB of hot pages.
- **`Compact`** (and `CompactTo`) now copies in batches of 4096 keys per write transaction, capping WAL burst and releasing the write lock between batches.
- **`DB.Update`** reads `readSeq` from the in-memory `cachedHdr` instead of `eng.ReadHeader()` (eliminates one `pread(page0)` per write transaction on MVCC DBs).
- **`DB.Update`** uses `UpdateHeaderGet` to set `CommitSeq` and refresh `cachedHdr` in one engine call post-commit (eliminates a second `ReadHeader` pread per MVCC write transaction).
- **`StatsMVCC`** replaced the `map[PackedCoord]struct{}` logical-cell counter with a running `prevCoord` comparison — O(1) memory instead of O(logical cells).
- **`scoreCell`** tag lowercasing moved to `scoreRecord` (once per record) rather than being repeated inside the scoring function.

## [0.4.0] - 2026-05-08

### Added

- **Spatial algorithms** — Six new public `Tx` methods for advanced context loading and graph traversal:
  - **`LoadContextFOV`** — Field-of-view context loading with LOS-based hex ray casting. Empty cells act as opaque barriers, spending budget only on semantically reachable cells.
  - **`LoadContextLOD`** — Level-of-detail context loading. Full density near the center, progressively sparser at outer rings.
  - **`LoadContextVoronoi`** — Voronoi-partitioned multi-seed context. Each seed gets a non-overlapping, fair share of the budget.
  - **`FindEdgePath`** — A\* shortest path over edges between cells. Edge weights are traversal costs.
  - **`WalkEdges`** — BFS reachability over edges within a hop limit.
  - **`LoadContextByEdges`** — Context assembly via edge traversal instead of spatial ring walk.

- **`internal/pathfind`** package — A\*, Dijkstra, BFS, priority queue for graph-based pathfinding.

- **`internal/lattice`** additions — `FOVHexCoords`, `LODRingCoords`, `VoronoiPartition`, `WalkRingsPacked` for spatial math.

- **Embedding auto-detect** — `EmbeddingDimension: 0` (the default) now means "auto-detect on first `PutEmbedding`" instead of "disabled". The dimension and metric are persisted to the file header atomically on first use. All subsequent vectors must match.

- **`Engine.SetEmbeddingConfig`** — New internal method to persist embedding dimension and metric to the header at runtime.

- **`docs/hexxladb/CONFIGURATION.md`** — Comprehensive guide to all `Options` fields, common configurations, and immutable vs mutable fields.

- **`examples/spatial_algorithms`** — New demo exercising FOV, LOD, Voronoi, pathfinding, and edge-based context loading.

- **`(*Tx).DeleteCellWithOutcome`** — Like **`DeleteCell`** but returns **`removed bool`**: **`true`** when a visible cell was tombstoned (MVCC) or hard-deleted (v1); **`false`** on idempotent no-op (`delete_cell.go`).

- **Mutation testing** — `make mutation-test` via gremlins for `internal/lattice`, `internal/record`, `internal/changelog`.

### Changed

- **Embedding operations with `dim=0`** — `GetEmbedding`, `DeleteEmbedding`, `SearchByEmbedding`, and `ReindexEmbeddings` now return empty/nil when no embeddings have been stored yet, instead of returning `ErrEmbeddingsDisabled`. This is a **soft breaking change** for code that matched on `ErrEmbeddingsDisabled`.

- **`ErrEmbeddingsDisabled`** — Deprecated. Still exported for backward compatibility but no longer returned by standard embedding operations.

- **`Options.EmbeddingDimension` doc** — Updated to reflect auto-detection: 0 means "auto-detect", not "disabled".

- **TUI** — Dashboard shows "none yet" instead of "off" for embeddings; embeddings tab shows "No Embeddings Stored" instead of error panel.

- **`examples/llm_context_engine`** — Removed explicit `EmbeddingDimension` from `Open` options (auto-detected from Ollama).

- **README** — Simplified quick-start `Open` example; added `CONFIGURATION.md` link; listed Mosaic project.

- **Complexity refactoring** — Reduced cognitive/cyclomatic complexity across ~10 files including `SearchByEmbedding`, `AssembleCellView`, `rebalanceLeaf`, `readPagePooled`.

### Fixed

- **HealthCheck double-counting MVCC seams** — The checker now groups versioned keys by ULID and applies `mvcc.SelectVisible` at the view's read_seq.

- **HealthCheck false positives on MVCC tag/source indexes** — The checker now parses MVCC suffixes on physical keys and validates the decoded cell carries the indexed tag/source.

## [0.3.0] - 2026-04-29

### Added

- **Exported walk alias types for embedding apps** — `FacetWalkRecord` and `EdgeWalkRecord` (now in [`export.go`](./export.go)) alias `internal/record` wire structs so MCP/adapters outside the module can type `AscendFacetsForCell` / `AscendEdgesFrom` closures without importing `internal/`.
- **External-call helpers** — `NewProvenanceWire` (timestamps `now`) and `NewFacetDerived` for modules that cannot name `internal/record` types when calling `Tx.LinkCells` / `Tx.PutFacet`.

## [0.2.0] - 2026-04-27

### Fixed

- **WAL unbounded growth** — both `CommitWriteTxn` (classic path) and `applyGroupBatch` (group WAL path, used by all `DB.Update` calls) now truncate the WAL to zero after all pages are durably applied to the primary. Previously the WAL was only truncated on the next `Open`, causing it to accumulate all redo records indefinitely (25 MB for a 128 KB DB after 20 embedding inserts). The WAL is now always zero-length between transactions.
- **B+ tree leaf-page-full on large inline value updates** — `insertIntoLeaf` unconditionally called `buildLeafPage` when replacing an existing key's value without checking whether the updated page still fit within `pageSize`. When HNSW node neighbor lists grow during `PutEmbedding` (e.g. 128-dim embeddings, 32-dim at >12 entries), the updated value is larger than the original, causing the page to overflow and returning `ErrCorruptTree: leaf page full`. Fix: added a `leafSerializedSize` guard on the update-in-place path; pages that exceed `pageSize` after an in-place update now fall through to the existing split path. Regression tests added: `TestPutEmbedding_HighCount_32d` (600 entries) and `TestPutEmbedding_HighCount_128d` (150 entries) in `btree_regression_test.go`.
- **`leafSplitIndex` right-half overflow** — hardened the split-point algorithm to scan until the left half would exceed `pageSize` (not `pageSize/2`), ensuring the right half always fits. Previously, with very large entries, `leafSplitIndex` could return a `mid` where the right half alone exceeded `pageSize`.

### Added (llm-context-engine example)

- New `examples/llm_context_engine` — realistic LLM memory retrieval demo
  - Scenario 1: Ingest 20 conversation turns with Ollama all-minilm embeddings
  - Scenario 2: Semantic retrieval — 3 distinct queries showing HNSW differentiation
  - Scenario 3: Multi-signal retrieval — embeddings + tag filters + confidence + source
  - Scenario 4: Preference supersession — MarkSupersedes + FilterSuperseded in context assembly
  - Scenario 5: Full LLM prompt assembly pipeline — search → preferences → LoadContextPackFrom
  - Scenario 6: Comparison table — what HexxlaDB enables vs stateless LLMs
- Moved embedding functionality out of conversational_memory demo (reverted to 12 phases)

### Added (benchmarks-docs)

- Embedding search benchmarks: `BenchmarkSearchByEmbedding_HNSW` (500×32d, 200×64d, 100×128d), `BenchmarkQueryCells_Embedding` (500×32d)
- Updated `doc.go` with embedding/HNSW entrypoints
- Updated `HEXXLA_DB.md` with HNSW keyspace layout and query engine integration
- Updated `API_REFERENCE.md` with HNSW-accelerated search and query planner integration
- Updated `ROADMAP.md` to mark embeddings keyspace as complete

### Added (query-engine-embedding)

- `CellQuery.Embedding` and `CellSearchConfig.Embedding` trigger ANN-accelerated seed selection
- `QueryCells` planner picks embedding index when `Embedding` is set (highest priority)
- Embedding similarity score added to composite relevance score alongside lexical scoring
- `scanByEmbedding` over-fetches 2× to leave room for post-filter narrowing
- All existing predicates (tags, temporal, spatial, confidence) apply as post-filters on embedding results
- 3 new integration tests: QueryCells + Embedding, Embedding + tag filter, SearchCells + Embedding

### Added (hnsw-graph)

- **HNSW graph** (`hnsw/` keyspace): sub-linear approximate nearest-neighbor search persisted in the B+ tree
- `internal/hnsw` package: `Node` and `Meta` encode/decode, `Graph` with Insert/Search/Delete
- `hnsw/meta`, `hnsw/entry`, `hnsw/node/<packed_coord>` keyspace (keys in `internal/index/hnsw_key.go`)
- HNSW insert with random layer selection, greedy descent, ef-bounded beam search, bidirectional linking
- HNSW search with greedy layer descent and ef-bounded beam at layer 0
- HNSW delete with neighbor repair and entry point promotion
- `SearchByEmbedding` uses HNSW when graph exists, flat-scan fallback otherwise
- `PutEmbedding`/`DeleteEmbedding`/`DeleteCell` cascade maintain HNSW graph automatically
- `Tx.getDirect` helper for internal reads bypassing public API guards
- `txHNSWStorage` adapter bridges `Tx` to `hnsw.Storage` interface
- 7 graph tests (insert, recall, delete, delete-all, delete-entry, update, empty) + 6 node/meta encoding tests

### Added (embeddings-keyspace)

- **Embedding keyspace** (`embed/<packed_coord>`): fixed-dimension float32 vector storage per cell
- `Options.EmbeddingDimension` / `Options.DistanceMetric` — dimension and metric locked at creation, persisted in file header (offsets 104–106)
- `DistanceMetric` type with `DistanceCosine`, `DistanceDotProduct`, `DistanceL2` constants
- Distance functions: cosine similarity, dot product, Euclidean distance (pure math, `internal/engine`)
- `DB.EmbeddingDimension()` / `DB.EmbeddingMetric()` introspection accessors
- `Tx.PutEmbedding`, `Tx.GetEmbedding`, `Tx.DeleteEmbedding` — embed/ keyspace CRUD
- `Tx.SearchByEmbedding` — flat-scan nearest-neighbor search with goroutine parallelism and min-heap top-K
- `Tx.ReindexEmbeddings` — bulk recompute all embeddings via user-supplied callback (model switch support)
- `DeleteCell` cascades to remove the cell's embedding automatically
- `ErrEmbeddingsDisabled`, `ErrEmbeddingDimension` sentinel errors
- 14 new tests covering: put/get round-trip, delete, dimension mismatch, disabled DB, cascade, search (top-K, empty, min-score), reindex, reindex-skip, DB accessors, persistence across reopen, dimension mismatch on reopen
- Distance function unit tests and benchmarks (384-dim, 768-dim)

### Added (content-compression)

- **Always-on transparent per-value DEFLATE compression** via `compress/flate` (Go stdlib, zero external dependencies)
- Compressed values carry a 5-byte `0xFE` envelope; uncompressed values coexist transparently
- Compression runs before overflow check — compressible values may fit inline even if raw size exceeds the threshold
- Values < 64 bytes and incompressible values stored raw (no overhead)
- 10 new engine tests: round-trip, skip-short, mixed long/short, AscendRange, overflow+compression, incompressible, reopen, delete
- No configuration required — compression is always-on with no public API surface

### Added (overflow-pages)

- **Overflow pages**: values exceeding the inline leaf threshold are automatically stored in a chain of overflow pages; reads, scans, deletes, and compact all resolve overflow transparently
- `Options.MaxValueBytes` now accepts 32768, 65536, 131072, 262144, 524288, 1048576 (up to 1 MiB)
- 10 new engine tests: parametric round-trip at all page sizes, multi-page chain, overwrite, inline→overflow transition, delete, AscendRange, many-key stress
- Overflow pages are ordinary data pages — encryption, WAL, and compact work without changes

### Added (storage-contract-tests)

- `internal/domain/storagecontract` package — 22 reusable contract tests for the `domain.Storage` port interface
- Covers all port methods: cells, seams, facets, edges, walks, context assembly, time buckets, tags
- `RunAll(t, Factory)` harness: any adapter can validate conformance by providing a factory function
- Real `hexxladbout.Storage` adapter passes all contracts
- `record.UniqueSortedTags` extracted from root package for reuse; `cell_secondary.go` / `seam_secondary.go` documented in-place

### Added (efficient-storage)

- **Configurable page size**: `Options.PageSize` selects 4096, 8192, 16384, or 65536 bytes for new databases (default 4 KiB); existing databases read page size from the file header on open
- `DB.PageSize()` introspection method returns the active page size
- `engine.IsValidPageSize` public helper for callers that need to validate before Open
- Fill-based B+ tree leaf splitting (replaces fixed `maxLeafEntries=32`); leaves split when serialized size exceeds 50% of page capacity
- Dynamic internal node capacity derived from page size
- WAL record size adapts to runtime page size
- Instance-level page buffer pool sized to the database's page size
- `CompactTo` preserves source page size in destination database
- Parametric engine tests at all four valid page sizes

### Added (delete-compact)

- `Tx.DeleteCell` — remove cell + secondary indexes + facets + outbound edges atomically; idempotent (missing cell returns nil)
- MVCC tombstone support: zero-length value at `cell/<packed>/<writeSeq>` treated as deleted by visibility layer; facets tombstoned likewise
- `tx.cellDeleted` overlay for same-tx delete→get correctness; cleared on re-put
- `changelog.OpDeleteCell` / `ChangelogOpDeleteCell` — stable op code `6` for changefeed consumers
- `domain.Storage.DeleteCell` port + adapter + `app.Service` delegation (hex boundary)
- `DB.Compact` — copy-compact open database to destPath (holds read lock, preserves all data)
- `CompactTo` — standalone copy-compaction from srcPath to destPath; propagates format version, MVCC flag, encryption, MaxValueBytes
- Comprehensive tests for both features: v1/v2, MVCC snapshot isolation, facet/edge cleanup, same-tx overlay, encrypted compact, context cancellation, file size reduction, HealthCheck validation
- Demo Phase 12 in `examples/conversational_memory` — exercises DeleteCell (MVCC tombstone, ViewAt snapshot isolation, idempotent re-delete) and Compact (bulk write→delete→prune→compact with file size reduction)

### Fixed (delete-compact)

- `HealthCheck` on MVCC databases now correctly excludes tombstoned cells from `CellCount` — previously zero-length tombstone values were counted as live cells

### Added (tui-audit)

- `cmd/tui` interactive database explorer — tabs: Dashboard, Cells, Hex Grid, Inspector, Analytics, Seams, Health, Diff; lexical search in Cells tab (`/` to open, `Enter` to execute, `Esc` to clear); Inspector with context pack assembly and explain panel; neon-on-dark colour scheme
- `Consuming() bool` method on `view` interface — tabs signal text-input mode so global shortcuts (`q`, `1-8`, `Tab`) don't intercept keystrokes during search
- `noConsume` embedded struct — zero-overhead default `Consuming() false` for all non-input views

### Changed (tui-audit)

- `Init()` now batches `tea.WindowSize()` + `tabActivatedMsg` — ensures window dimensions and initial tab load fire correctly on startup
- Replaced all `tea.Tick(time.Millisecond, ...)` one-shot load patterns with plain `tea.Cmd` closures — eliminates spurious 1 ms delays and scheduler round-trips
- All view `Update` methods handle `tabActivatedMsg` explicitly for lazy loads; removed fallthrough `!v.loaded` guards that could fire duplicate load goroutines on every message
- Content area uses `MaxHeight` hard-clip — tab bar and status bar can never be pushed off screen by overflowing view content
- Tab bar height derived from `lipgloss.Height(renderTabBar())` — no hardcoded row count
- `renderContent` passes full terminal width to `lipgloss.Place` with `WithWhitespaceBackground` — right edge always filled regardless of inner content width
- Card-interior text styles (`styleCardDim`, `styleCardHeader`, `styleCardKey`, `styleCardValue`) added — eliminates `colorBg1` leaking into `colorBg2` stat/info cards across all views
- `barGraphBg` helper added — bar characters inside cards rendered with correct card background
- Removed unused `styleKey`, `styleValue`, `stylePink` variables

### Changed (docs)

- README rewritten — sharper introduction framing the spatial-locality-as-physical-property thesis; condensed to ~180 lines; benchmark section summarised with key bullet points; full tables remain in `OPERATIONS.md`
- `FUNDING.yml` added under `.github/` — enables GitHub Sponsors button on repo page
- Badges added to README header: CI, Integration, Go Reference, Go Report Card, Go version, License

### Added (health-check-rewrite)

- `BenchmarkAPI_HealthCheck` — measures full integrity scan; O(n) forward-scan implementation; 512 cells → 445 µs, 2000 cells → 1.6 ms

### Changed (health-check-rewrite)

- `DB.HealthCheck` — replaced O(ScanRadius²) `WalkRings`+`GetCell` cell scan with a single `cell/` prefix `AscendRange`; replaced `FindSeams` spatial call with `seam/` primary-key scan (covers all seams, no radius limit); replaced `ListExistingTopics`+`AscendCellsByTag`-per-tag O(tags×cells) loop with single `tag/` family `AscendRange`; replaced `AscendCellsBySource`-per-source loop with single `source/` prefix scan; all `GetCell` presence checks replaced with O(1) `liveCells` map lookup built during the initial cell scan — overall complexity reduced from O(ScanRadius²+tags×n+sources×n) to O(n)
- `HealthCheckConfig.ScanRadius` — deprecated; field retained for backward compatibility but has no effect; cell scan now covers all cells regardless of coordinate

### Added (bench-improvements)

- `CellQuery.MaxScanRows` — additive field to bound the number of index rows examined by `scanByTag` and `scanBySource`; zero = unlimited (existing behaviour unchanged)
- `lattice.RingInto(dst, center, k)` — buffer-reuse variant of `Ring`; eliminates per-ring heap allocation in tight loops; `Ring` unchanged for backward compatibility
- `BenchmarkAPI_BatchPutCells` — batch write throughput benchmark; sizes 10/100/500; reports `cells/op` metric

### Changed (bench-improvements)

- `mortonPack63` — replaced 21-iteration scalar bit loop with 128-entry lookup table (`mortonExpand7`); 3 passes × 7 bits = 21 axis bits; `mortonUnpack63` unchanged (scalar); wire format identical
- `collectCandidates` — pre-sizes `items` and `seen` with `min(3r²+3r+1, capCells)`; reuses a single `ringBuf` via `lattice.RingInto` across ring iterations; eliminates ~7 growth doublings at r=5
- `LoadContextWithBudgeting` eviction loop — O(1) token subtraction instead of O(n) full recalculation after each dropped item
- `AssembleCellView` — removed defensive `Tags` copy (`append([]string(nil), rec.Tags...)`); `CellView.Tags` is read-only post-assembly (all callers confirmed)
- `findSeams` — replaced `lattice.WalkRings` materialisation with inline `for ring / for _, c := range lattice.Ring` two-level loop (lazy iteration); added pre-flight presence check using `SeamByCellsScanUpperBound()` — a single `AscendRange` confirms index is empty, saving 74–182 B+ tree traversals at r=3–5 in seam-free databases

### Changed

- Deleted orphaned `internal/config` package — `config.Load()` had zero callers; `cmd/tui` already handles log level inline
- `views.go` view-assembly logic extracted to `internal/views` — `TxReader` port interface (`GetCell`, `AscendFacetsForCell`, `AscendEdgesFrom`, `FindSeams`) breaks the import cycle; all types (`CellView`, `ContextPack`, `FacetView`, `EdgeView`, `SeamRef`, `CellExplanation`, `ContextPackStats`, `TokenBudgeter`, `ByteLenBudgeter`, `AssembleCellViewOpts`, `LoadContextBudgetConfig`, `CellViewPredicate`) re-exported as type aliases — **zero public API change**; `*Tx` methods are thin wrappers delegating to `internal/views`; `cell_secondary.go`, `seam_secondary.go`, `rotation.go` remain at repo root (reclassified to Future)

### Added

- `BenchmarkAPI_QueryCells` — 4 predicate shapes (tag-only, source-only, spatial, combined) × 2 preload sizes; performance baseline for query engine work
- `BenchmarkAPI_LoadContextPack` — radii 1/3/5 × 2 preload sizes; performance baseline for budgeting work
- `BenchmarkAPI_MVCCVersionResolution` — 10/50/100/500 versions of same coord; isolates `SelectVisible` O(n) scan under realistic MVCC load
- `make demo` Makefile target — runs `examples/conversational_memory` with DB defaulting to `.tmp/demo/memory.db`; override via `make demo DEMO_DB=/path/to/my.db`; DB reused across runs (seed skipped if file exists)
- `conversational_memory` demo expanded — corpus moved to `seed_data.go` (84 turns, 5 thematic sessions: preferences/workflow, HexxlaDB internals, Go patterns, LLM systems, security/ops); `-db` CLI flag for custom DB path; DB defaults to `.tmp/demo/memory.db` (no root pollution); `printSubHeader`/`printNote` helpers; all phase descriptions, metrics, and explain outputs improved for readability; `spiralCoord` widened to 11-column grid for 84-cell corpus; budgets increased to 600/800 bytes; Phase 11 demonstrates `DB.HealthCheck`, `AfterPutCell` telemetry, and `DB.SnapshotDiff` end-to-end

- `Options.MaxValueBytes uint32` — per-database maximum B+ tree value size; accepted values: 512, 1024, 2048, 4096, 8192, 16384 bytes; default 0 = 8192 (8 KB); persisted in the file header; enforced on every write via `BTree.Put`; readable via `DB.MaxValueBytes()`; `ErrInvalidArgument` on invalid value; 9 tests
- `(*DB).MaxValueBytes() uint32` — returns the effective limit read from the file header at `Open`

- `DB.SnapshotDiff(ctx, fromSeq, toSeq, SnapshotDiffConfig) (SnapshotDiff, error)` — MVCC change diff; returns all cell/seam writes in `(fromSeq, toSeq]`; `ErrMVCCRequired` on v1 databases; `ErrReadSeqFuture` if `toSeq` > head; `SnapshotDiff{Cells []CellDiff, Seams []SeamDiff}`; 9 tests
- `ErrMVCCRequired` — new sentinel error

- `AfterPutCellHook` / `AfterPutCellHookFunc` — post-write callback fired after each successful `Tx.PutCell`; error propagates to caller; set via `Options.AfterPutCell`
- `AfterPutSeamHook` / `AfterPutSeamHookFunc` — post-write callback fired after `Tx.PutSeam`, `Tx.MarkConflict`, `Tx.MarkSupersedes`; set via `Options.AfterPutSeam`; 9 tests

- `app.Service` use-case layer completed — all 23 `domain.Storage` port methods now delegated; compile-time interface satisfaction check (`var _ domain.Storage = (*Service)(nil)`) added to catch future drift; `ErrNoStorage` returned by every method when storage port not wired; 2 tests

- `DB.TagSnapshot(label string) error` — pin the current head `CommitSeq` under a human-friendly label; stored in B+ tree under `__meta/snap-tag/<label>`; overwrites existing tag with same name; label max 200 bytes
- `DB.ViewAtTag(label string, fn func(*Tx) error) error` — open a read-only snapshot pinned to the commit recorded by `TagSnapshot`; returns `ErrSnapshotTagNotFound` if label absent
- `DB.ListSnapshotTags() ([]SnapshotTag, error)` — enumerate all tags sorted by label
- `DB.DeleteSnapshotTag(label string) error` — remove a tag entry without affecting underlying data
- `SnapshotTag` — `Label string`, `CommitSeq uint64`
- `ErrSnapshotTagNotFound`, `ErrSnapshotTagLabelTooLong` — new sentinel errors; 11 tests

- `Tx.QueryCells(ctx, CellQuery) ([]CellQueryResult, error)` — composable query engine with index-aware planner; predicates: `Query` (lexical), `RequireTags` (AND), `AnyTags` (OR), `ExcludeTags` (NOT), `SourceID`, `MinConfidence`/`MaxConfidence`, `After`/`Before` (temporal via `time/` week-bucket index), `Center`+`Radius` (spatial), `MaxResults`, `SortBy`, `Explain`; 17 tests
- `CellQuery`, `CellQueryResult`, `SortOrder` — query predicate types; `SortByScore`, `SortByConfidence`, `SortByRecency`, `SortByCoord`
- `SearchCells` refactored to thin wrapper over `QueryCells` — no breaking change
- Temporal Range Queries delivered via `CellQuery.After`/`Before` (closes TODOS.md item)

- `DB.HealthCheck(ctx, HealthCheckConfig) (HealthReport, error)` — integrity scan: visible cell count, seam resolution summary (resolved/unresolved), orphaned seam detection, tag index consistency, source index consistency, MVCC stats snapshot; configurable `ScanRadius` and `MaxErrors`
- `HealthReport`, `HealthCheckConfig`, `DefaultHealthCheckConfig` — types and constructor for health check
- `Tx.SearchCells(ctx, CellSearchConfig) ([]CellSearchResult, error)` — scored full-scan search over visible cells; matches `RawContent` (substring), `Tags` (exact + prefix), `SourceID`; supports `RequireTags` (AND), `AnyTags` (OR), confidence range, spatial radius, and `MaxResults` cap; returns `[]CellSearchResult` sorted by composite score, each carrying a `Coord` for direct use as a context-pack seed
- `CellSearchConfig`, `CellSearchResult` — forward-compatible search API; `Embedding []float32` can be added later without breaking callers
- `Tx.LoadMultiContextPack(ctx, MultiContextConfig) (ContextPack, error)` — expand multiple seed coords, merge resulting cell views under a shared token budget, optionally deduplicate shared-neighbourhood cells; companion to `SearchCells` for multi-seed retrieval
- `MultiContextConfig` — `Centers []Coord`, `MaxR`, `MaxTokens`, `Budgeter`, `AssemblyConfig`, `DeduplicateCoords`
- `SeamTypeSupersedes` constant (`"supersedes"`) for directional supersession seams
- `Tx.MarkSupersedes(superseder, superseded Coord, reason string)` — records that a cell is the current truth and another is stale
- `LoadContextBudgetConfig.FilterSuperseded bool` — when true, `LoadContextWithBudgeting` / `LoadContextPack` walk supersession chains and replace stale cells with their current-truth successors (or exclude them if no live successor exists)
- Cycle detection and depth limit (16 hops) in supersession chain walks
- `CellView.SupersededFrom *Coord` — set when context assembly substituted this cell for a stale one
- `CellExplanation.SupersededBy *Coord` and `Reason: "superseded"` — Explain mode now records superseded exclusions and substitutions
- `conversational_memory` example Phase 4 demonstrates seam-aware assembly visually

## [0.1.0] - 2026-04-24

_First release._

[Unreleased]: https://github.com/hexxla/hexxladb/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/hexxla/hexxladb/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/hexxla/hexxladb/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/hexxla/hexxladb/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/hexxla/hexxladb/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/hexxla/hexxladb/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/hexxla/hexxladb/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/hexxla/hexxladb/releases/tag/v0.1.0
