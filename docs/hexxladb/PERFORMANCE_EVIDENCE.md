# Performance evidence

This suite answers two separate questions about Dijkstra pathfinding,
deterministic field of view (FOV), MVCC hot-key reads, HNSW vector search,
lattice placement quality, and the aperture-7 super-hex occupancy prototype:

1. **Does the implementation remain correct and how does each operation scale
   under controlled inputs?** The controlled stream runs a seeded randomized
   oracle soak and focused Go benchmarks.
2. **What distribution does a combined, production-style workload show on the
   target host?** The observation stream runs a bounded synthetic workload and
   emits one aggregate JSON report.

Neither stream sends telemetry or opens an existing database. Generated
databases and reports stay under temporary or gitignored paths. The observation
report contains durations, counts, runtime metadata, allocation totals, and file
sizes only; it does not contain cell content, coordinates, database paths, or
individual query inputs.

Toolchain-transition comparisons are collected separately from `task evidence`
because they require running the identical source tree with two Go releases.

## Quick run

```bash
task evidence
```

This writes:

- `.tmp/evidence/fov-bench.txt`
- `.tmp/evidence/changelog-read-bench.txt`
- `.tmp/evidence/api-bench.txt`
- `.tmp/evidence/encryption-page-bench.txt`
- `.tmp/evidence/superhex-sync-bench.txt`
- `.tmp/evidence/storage-churn.txt`
- `.tmp/evidence/workload.json`
- `.tmp/evidence/write-path.json`
- `.tmp/evidence/lattice-placement.json`

Run the streams separately when iteration time matters:

```bash
task evidence-controlled
task evidence-observe
task evidence-write-path
task evidence-vector-scale
task evidence-lattice-placement
task soak-pilot
```

The default observation workload uses 2,000 cells, 100 samples, seed `1`, FOV
radius `10`, and super-hex level `2`. Override it without changing source:

```bash
task evidence-observe \
  EVIDENCE_ARGS='-cells 10000 -samples 500 -seed 7 -fov-radius 20 -superhex-level 3'
```

Input bounds are deliberate: at most 100,000 cells, 10,000 samples, and FOV
radius 512. This keeps an accidentally oversized observation run bounded.

The vector runner defaults to deferred ingestion plus an atomic rebuild of
10,000 32-dimensional unit vectors, 25 exact-oracle query samples, recall@10,
100 updates plus 100 deletes, 4 KiB pages, and a 64 MiB page cache. It writes
aggregate JSON to
`.tmp/evidence/vector-scale.json`. Override bounded inputs and the output path:

```bash
task evidence-vector-scale \
  VECTOR_EVIDENCE_ARGS='-cells 10000 -dimension 384 -batch-size 100 -build-mode deferred-rebuild' \
  VECTOR_EVIDENCE_OUTPUT='.tmp/evidence/vector-scale-10000-384d.json'
```

Use `-build-mode synchronous` for a same-host comparison with per-write graph
maintenance. Deferred runs reject more than 20,000 vectors, and the rebuild API
also applies its vector, estimated-memory/transient-WAL, and filesystem-capacity
preflight before marking the current graph stale.

The placement runner defaults to six synthetic topics with 20 documents each,
places 12 per topic before an incremental append, and compares a stable
topic-clustered first-free policy with intentionally interleaved placement. It
uses only public APIs and writes `.tmp/evidence/lattice-placement.json`:

```bash
task evidence-lattice-placement \
  PLACEMENT_EVIDENCE_ARGS='-documents-per-topic 20 -initial-per-topic 12' \
  PLACEMENT_EVIDENCE_OUTPUT='.tmp/evidence/lattice-placement-120.json'
```

The separate pilot qualification uses the bounded production-readiness profile
and hard pass/fail gates documented in
[`OPERATIONS.md`](OPERATIONS.md#pre-release-soak-checklist).
Its five-minute default must also meet per-operation minimum sample counts, so
elapsed time alone cannot produce a passing report. The aggregate report is written to
`.tmp/evidence/pilot-soak.json`, while the database and backup drills stay in
one isolated run directory that is removed on exit.

## What is measured

| Area              | Controlled evidence                                                                                                                                        | Observation evidence                                                                                      |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Dijkstra          | API latency and allocations as graph out-degree grows                                                                                                      | p50/p95/max/mean latency, paths found, and aggregate hop count over seeded route queries                  |
| Deterministic FOV | Shadowcast algorithm, retained raycast comparison, and full `LoadContextFOV` API latency                                                                   | p50/p95/max/mean latency and aggregate number of returned cells with deterministic blockers               |
| Super-hex         | Rebuild, O(1) coordinate lookup, deterministic export, direct changelog tail reads across 512–100k historical records, and fixed-history one-shot catch-up | rebuild/write/sync distributions, changes processed, summary count, applied sequence, and caught-up state |
| Vector search     | Final 500×32d query latency and allocation benchmark versus the recorded pre-change baseline                                                               | Batched graph build, exact-oracle recall@k, query latency/path/breadth, reopen, churn, memory, and file sizes |
| Lattice placement | Deterministic repeat test for placement, incremental insertion, collision handling, and successor substitution                                           | Neighborhood precision, useful-context ratio, semantic precision, semantic/lattice divergence, and labelled diagnostic grids |
| MVCC hot keys     | Latest and historical point-read latency and allocations at 10, 100, 1,000, and 6,000 versions                                                           | Not included                                                                                                |
| Public writes     | Single, batched, callback-delayed, fdatasync, and reader-blocking latency with group-WAL batch/sync counts                                               | Not included                                                                                                |
| Storage churn     | Exact primary/live/reclaimable page bytes after puts, tombstones, pruning, and compaction; bounded progress and interruption tests                      | Final primary, WAL, and changelog sizes                                                                     |
| Resources         | Go allocation counts per benchmark operation                                                                                                               | total bytes allocated plus final database, WAL, and changelog sizes                                       |

### Go 1.26.7 to Go 1.27.0 comparison

The 2026-08-25 toolchain decision used the same source tree and an Intel Core
i9-14900HX Linux/amd64 host. Each toolchain used a separate
build cache and one logical CPU (`GOMAXPROCS=1`, `-cpu=1`). After an untimed
warm-up, ten 500 ms samples per benchmark were collected with toolchain order
alternated between rounds. `benchstat` from `golang.org/x/perf` at revision
`ebcb4798430d` compared the samples with its default Mann-Whitney test and 95%
confidence threshold. The focused set covered public reads, context assembly,
durable and batched writes, value compression, record encoding, and HNSW node
encoding rather than aggregating every repository benchmark.

Statistically significant time changes from Go 1.26.7 to Go 1.27.0 were:

| Operation | Change |
| --------- | -----: |
| `GetCell`, 512 / 2,000 preloaded cells | -79.21% / -81.08% |
| Raw context scan, 512 / 2,000 cells | -58.64% / -57.00% |
| Context assembly across tested radii and sizes | -16.81% to -47.42% |
| Compressible / incompressible 1 KiB value compression | -25.31% / -97.74% |
| 1 KiB value decompression | -30.44% |
| Cell-record encode/decode | -13.23% |
| HNSW node encoding | +7.66% (about +24 ns/op) |

None of the five durable-write timings or three `BatchPutCells` sizes changed
at the 95% confidence threshold. HNSW decoding was also unchanged. Public point
reads allocated 98.7% fewer bytes per operation, while context scans allocated
96.9% fewer bytes. `LoadContext` allocation counts were mixed: they fell about
24–25% with 512 preloaded cells but rose about 26–27% with 2,000, even as bytes
allocated fell 52–56% and execution time improved in every case. Durable-write
bytes per operation also shifted by small but significant amounts (-2.58% to
+5.00%) without a corresponding timing regression, so both allocation metrics
remain watch items for representative application workloads.

The HNSW result is a compiler microbenchmark effect rather than a changed graph
or wire format. A separate 15-sample alternating rerun measured 280.3 ns/op on
Go 1.26.7 and 320.3 ns/op on Go 1.27.0 (+14.27%, p<0.001), with the same one
896-byte allocation. Twelve-sample attribution probes found no significant
allocation-cost change, a 9.6% faster size calculation, and an 18.53% slower
allocation-free buffer fill. Both compilers made the same inlining and escape
decisions; amd64 disassembly showed that Go 1.27 emitted smaller code but lowered
the tight range loops differently. The evidence therefore localizes the delta
to Go 1.27 compiler code generation in the byte-fill loop, without establishing
an end-to-end HNSW build regression. At roughly 40 ns per encoded node, changing
the clear existing encoder is not justified without a representative HNSW build
benchmark showing a material application-level impact.

These read-path changes include a representation trade-off:
the representative 88-byte benchmark cell encoded as a 70-byte compressed value
under Go 1.26.7, but Go 1.27.0's `flate.BestSpeed` result did not beat the raw
size, so HexxlaDB correctly stored the 88-byte value uncompressed. That avoids
decompression on this hot read but can reduce page density for small,
moderately-compressible values. Larger compressible values remain compressed;
compressed and raw values already coexist in the same format.

The comparison supports the Go 1.27.0 minimum because the material wins occur
in real public read paths, the storage behavior follows the existing
store-compressed-only-when-smaller rule, and no measured write-path regression
was established. Capacity-sensitive users should still rerun representative
data-size and page-density workloads; the benchmark does not turn a toolchain
upgrade into a storage-size guarantee.

### Authenticated-page format evidence

The format-v3 decision used two focused comparisons on the 2026-08-26
Linux/amd64 Intel Core i9-14900HX host:

```bash
go test -run '^$' -bench '^BenchmarkEncryptionPageTransform$' \
  -benchtime=500ms -count=5 .
go test -run '^$' \
  -bench 'BenchmarkAPI_GetCell_(MVCC|MVCC_Encrypted)/cells_2000$' \
  -benchtime=1s -count=5 .
```

The preselected acceptance budgets were no more than 30% median regression in
the page transform, no additional transform allocation count, and no more than
2% primary-file overhead at the default 4 KiB logical page size. Median direct
transform results were:

| Logical page | Transform | Legacy AES-XTS | V3 XChaCha20-Poly1305 |
| --- | --- | ---: | ---: |
| 4 KiB | seal | 9,257 ns | 2,630 ns |
| 4 KiB | open | 8,562 ns | 2,411 ns |
| 64 KiB | seal | 143,039 ns | 39,622 ns |
| 64 KiB | open | 141,767 ns | 35,885 ns |

Both paths used one allocation per transform. XChaCha was materially faster on
this CPU, so the latency gate passed without a storage-specific optimization.
The authenticated envelope adds 48 bytes per allocated data page: 1.171875% at
4 KiB and 0.0732421875% at 64 KiB, passing the space gate.

The public point-read comparison controls for v3's mandatory MVCC behavior by
comparing plaintext MVCC v2 with authenticated MVCC v3, not with unversioned
v1. The five-sample medians at 2,000 cells were 15,733 ns/op for v2 and 15,819
ns/op for v3 (+0.55%); both reported 451 allocations and approximately 34.7 KiB
allocated per operation. The difference is within run variation and establishes
no read-path regression from authenticated decryption. It also shows that the
larger cost in the README table belongs to the current MVCC version-seek path,
not the page cipher; future MVCC allocation work requires its own profile and
acceptance target.

These are in-process CPU and synthetic point-read measurements, not a durability
or storage-device throughput claim. Crash-marker recovery, tamper faults,
migration, compaction, backup/restore, and rotation are correctness gates and
remain covered by their dedicated tests.

### Vector-search evidence

The runner generates seeded unit vectors and queries in memory, builds the
persisted HNSW graph through either synchronous public writes or deferred
authoritative writes plus `RebuildEmbeddingIndex`, and compares each ANN result
with an exact cosine top-k oracle. Deferred mode additionally requires exact
flat search before publication. It closes and reopens the database,
then updates and deletes the requested number of vectors, closes and reopens a
second time, and repeats the oracle comparison. It also requires the reported
execution path to be HNSW. The JSON contains aggregate durations and resource
counts, including sampled peak build heap, only; the temporary database is
removed.

The 2026-08-25 synchronous-construction baseline runs on commit `7c94398` used an Intel Core
i9-14900HX, Linux/amd64, Go 1.27.0, seed 1, 25 queries, recall@10, 100 updates
and 100 deletes, 4 KiB pages, and a 64 MiB cache:

| Workload | Batch | Build | Query before reopen p50/p95 | Recall before/reopen/churn | Query after churn p50/p95 | Heap after build/churn | Primary/live/reclaimable |
| -------- | ----- | ----- | ---------------------------- | -------------------------- | --------------------------- | ---------------------- | ------------------------ |
| 10k×32d  | 500   | 74.4 s; 134.4 vectors/s | 5.42/8.13 ms | .992/.992/.992 | 5.36/8.35 ms | 18.7/25.0 MB | 23.87/23.07/0.80 MB |
| 10k×384d | 100   | 156.5 s; 63.9 vectors/s | 30.64/39.39 ms | .956/.956/.960 | 32.68/39.80 ms | 99.3/67.9 MB | 66.06/64.50/1.56 MB |

The 32-dimensional run used the minimum default `ef_search=100`; the
dimension-aware 384-dimensional run used `ef_search=384`. The latter setting
replaced a fixed-100 result of .63 recall@10 while retaining bounded query
latency. Total Go allocation over the complete build/query/reopen/churn process
was 132.1 GB for 32 dimensions and 237.9 GB for 384 dimensions. Those cumulative
allocation figures make graph construction an explicit offline or batched
write cost even though steady heap and database size remain bounded.

The 2026-08-26 deferred-rebuild runs used the same machine, Go 1.27.0, seed,
query/churn counts, page size, and cache. A 10 ms sampler records peak heap and
therefore makes these build times conservative relative to an unsampled run:

| Workload | Batch | Ingest + rebuild + publish | Query before reopen p50/p95 | Recall before/reopen/churn | Query after churn p50/p95 | Peak build heap; heap after build/churn | Primary/live/reclaimable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 10k×32d | 500 | 9.28 s; 1,078 vectors/s | 6.03/8.72 ms | .992/.992/.992 | 5.32/8.10 ms | 451.7 MB; 12.8/15.0 MB | 13.57/13.32/0.25 MB |
| 20k×32d | 500 | 23.59 s; 848 vectors/s | 8.56/14.33 ms | .992/.992/.992 | 7.17/11.43 ms | 963.7 MB; 28.9/32.4 MB | 30.14/29.86/0.27 MB |
| 10k×384d | 100 | 35.99 s; 278 vectors/s | 43.14/61.69 ms | .952/.952/.956 | 40.25/49.87 ms | 571.2 MB; 68.5/57.5 MB | 55.76/54.66/1.10 MB |

Total Go allocation over the complete build/query/reopen/churn process was
8.93 GB, 19.43 GB, and 16.36 GB respectively. Five interleaved 1k×32d runs
isolated the lifecycle change: median end-to-end throughput increased from
271.4 to 2,420.2 vectors/s (8.9×), while median cumulative allocation fell from
10,222.7 to 888.4 MiB (11.5× lower). All ten samples retained recall 1.0 before
reopen, after reopen, and after churn, with overlapping query-latency and
steady-heap ranges.

The original *Efficient and Robust Approximate Nearest Neighbor Search Using
Hierarchical Navigable Small World Graphs* describes incremental HNSW
construction. *The DiskANN Library: Graph-Based Indices for Fast, Fresh and
Filtered Vector Search* highlights recall degradation from untreated graph
deletions and the need for repair/consolidation. HexxlaDB retains its existing
bounded neighbor repair for online mutations; the new lifecycle addresses bulk
construction by keeping embeddings authoritative, forcing exact search while
stale, and publishing only a complete revision. It does not import DiskANN,
add background workers, or claim DiskANN's streaming-update scale.

Page-layout trials identified 4 KiB as the supported HNSW profile rather than
the prior 64 KiB recommendation. At 2k×384d, 4 KiB pages built at 162.2
vectors/s with about 7 ms median query latency and a 14.07 MB primary; 64 KiB
pages built at 29.2 vectors/s with about 23.5 ms median latency and a 67.96 MB
primary. Two 10k×384d 64 KiB attempts were terminated by the reference host
before completion as transaction dirty pages amplified memory. The 4 KiB
10k×384d run completed without page-split or corruption errors.

The point-read fixes were also isolated with the existing final benchmark:

```bash
go test -run '^$' -bench '^BenchmarkSearchByEmbedding_HNSW/500_32d$' \
  -benchmem -benchtime=1x -count=1 .
```

The recorded baseline was 72.1 ms/op, 133.0 MB/op, and 514,980 allocations/op.
The finished implementation measured 14.8 ms/op, 16.4 MB/op, and 3,966
allocations/op on the reference host. Direct copies into pooled cache buffers,
allocation-free B+ tree page selection, and transaction-local HNSW decode
caches account for the reduction; the persisted B+ tree and HNSW encodings did
not change.

These measurements support deferred rebuilds through 20,000 vectors at 32
dimensions and 10,000 at 384 dimensions with the tested settings. They are not
a claim of an unbounded capacity or a service-level objective. For larger sets,
other dimensions, non-random vector
distributions, stricter recall targets, or different hardware, rerun the same
command with representative inputs and retain the JSON before choosing
`EfSearch`, batch size, cache, page settings, or rebuild resource ceilings.

### Lattice-placement evidence

The runner creates the same deterministic six-topic, 120-document corpus in two
temporary databases. The topic-clustered policy probes from a fixed per-topic
anchor in deterministic ring order. The intentionally interleaved policy probes
from one shared anchor while inserting topics round-robin. Both use
`FindFreeCellPlacement` followed by `PutCell` in the same update, append eight
documents per topic without moving the initial twelve, and use the same
deterministic embeddings. Evaluation uses
only read transactions; it does not repair or mutate poor placement.

The report defines:

- **neighborhood precision** as the fraction of occupied cells within two rings
  that share the seed's topic;
- **useful content fraction** as the same-topic share of raw-content bytes in
  the deterministically `MaxCells`-bounded `LoadContext` result; this measures
  placement quality and makes no model-token claim;
- **semantic precision** as the same-topic fraction among eight ANN neighbors;
- **semantic/lattice divergence** as total-variation distance between semantic
  and two-ring topic distributions, from zero (aligned) to one (disjoint);
- **coordinate stability** as the fraction of initial record IDs still found at
  their original coordinates after incremental insertion.

The 2026-08-25 reference run used an Intel Core i9-14900HX, Linux/amd64,
Go 1.27.0, seed 1, an eight-cell context limit, and the default workload:

| Policy          | Neighborhood precision | Useful content fraction | Semantic precision | Semantic/lattice divergence | Coordinate stability |
| --------------- | ---------------------- | ----------------------- | ------------------ | --------------------------- | -------------------- |
| Topic-clustered | 1.000                  | 1.000                   | 1.000              | 0.000                       | 1.000                |
| Interleaved     | 0.129                  | 0.248                   | 1.000              | 0.873                       | 1.000                |

The equal semantic precision isolates placement as the cause of the degraded
interleaved neighborhood and context results. The same report includes labelled
radius-two grids: the clustered grid contains one topic, while the interleaved
grid visibly mixes all six. The clustered lifecycle check also creates a
successor at a new free coordinate, preserves the predecessor, calls
`MarkSupersedes`, and verifies that `LoadContext` substitutes the successor only
when `FilterSuperseded` is enabled.

The acceptance check fails unless clustered precision is at least 0.8,
interleaved precision is at most 0.4, useful-content fraction separates by at least
0.4, semantic precision remains at least 0.8, clustered divergence is at most
0.25, interleaved divergence is at least 0.5, both stability scores are 1.0,
and the relocation/supersession contract passes. Existing `GetCell`, ring walks,
`LoadContext`, `SearchByEmbedding`, and `RenderHexGrid` exposed every failure, so
the evidence does not justify a placement engine or new inspection/export API.

### MVCC hot-key evidence

`BenchmarkAPI_MVCCVersionResolution` writes every version as a distinct durable commit to one coordinate, validates the expected record at each snapshot, and then measures `GetCell` for both the latest snapshot and the midpoint historical snapshot. Run it directly when evaluating version lookup changes:

```bash
go test -run '^$' -bench '^BenchmarkAPI_MVCCVersionResolution$' -benchmem -count=1 .
```

Use the same version matrix and machine for before/after comparisons. The acceptance gate is that lookup latency and allocations remain bounded by B+ tree traversal and page occupancy rather than increasing linearly with older irrelevant versions. Pair the benchmark with the race-enabled `TestIntegration_MVCC_sustainedPutCellSameKey`, which verifies latest and retained historical snapshots before pruning, after pruning, and after reopen.

### Public write-path evidence

`BenchmarkAPI_WritePath` compares default and explicitly delayed single commits, primary `fdatasync`, a controlled one-millisecond callback, and a 100-cell transaction. `BenchmarkAPI_WriteReaderBlocking` measures a view waiting behind that controlled callback and commit. Every case reports apply batches, multi-job batches, and WAL syncs per operation so a latency change cannot be mistaken for successful group coalescing. `BenchmarkAPI_BatchPutCells` uses fresh coordinates in one open MVCC database, keeping database lifecycle work outside the timed bulk-write path.

```bash
go test -run '^$' -bench '^(BenchmarkAPI_WritePath|BenchmarkAPI_WriteReaderBlocking|BenchmarkAPI_BatchPutCells)$' -benchmem -count=1 .
```

The acceptance gate is that zero-wait public writes report no artificial collection delay, an explicit positive window remains measurable, public multi-job batches remain zero under the serialized lock contract, batching retains lower per-cell latency, and race tests prove readers cannot enter before finalization.

For the limitation-reduction programme, `task evidence-write-path` adds bounded
per-commit distributions and resource measurements for three MVCC workloads:
one ordinary cell, 100 cells in one commit, and one cell plus a 32-dimensional
embedding after a 200-vector warm-up. The runner uses 4 KiB pages, removes its
temporary databases, emits aggregate JSON only, and rejects configurations
above 1,000 measured commits, 500 cells per batch, or 2,000 embedding warm-up
rows.

The following are conservative reference-host gates, not portable hardware
guarantees. An adopting operator must rerun the workload on representative
storage and may declare stricter service objectives. A code change may be
promoted only when at least five same-host before/after runs show a material
gain outside normal variation while all gates still hold.

| Workload | Latency and throughput | Allocation | Durability and growth |
| --- | --- | --- | --- |
| One MVCC cell per commit | p95 <= 25 ms; >= 40 commits/s | <= 512 KiB/commit | <= 1 WAL sync/commit; <= 2 MiB primary growth/commit |
| 100 MVCC cells per commit | p95 <= 30 ms; >= 4,000 cells/s | <= 256 KiB/cell | <= 1 WAL sync/commit; <= 2 MiB primary growth/cell |
| Cell + 32d embedding | p95 <= 100 ms; >= 10 commits/s | <= 64 MiB/commit | <= 1 WAL sync/commit; <= 8 MiB primary growth/commit |

For every workload, lock wait, callback/index work, durability, and changelog
finalization must remain attributable through `WriteStats`; heap after the
measured interval must remain bounded by the configured workload; and reopen,
race, and crash-barrier checks must retain their existing results. Missing an
absolute gate calls for profiling on the target host, not an automatic storage
engine redesign. In particular, Bf-tree-style mini-pages or an independent
commit pipeline remain experiments unless profiles isolate page-granularity
write amplification or serialized durability as the limiting cost.

The 2026-08-26 bounded run used Go 1.27.0 on an Intel Core i9-14900HX and an
encrypted Btrfs SSD volume. Five baseline and five changed-worktree runs used
the default workload, with each temporary database removed after its run. A
single page-ownership copy was eliminated only when the write transform
returned the engine-owned input buffer; transforms returning another buffer
retain the defensive copy. Median allocation fell from 454,266 to 402,740
bytes per single-cell commit (11.3%) and from 87,284 to 72,577 bytes per cell
in 100-cell commits (16.8%). Median single-cell p95 fell from 4.57 ms to 4.06
ms, while batch and embedding latency ranges overlapped and are treated as
unchanged. Every changed run retained one WAL sync per commit, zero multi-job
batches, bounded heap and file growth, and passed the calibrated gates. The
result justifies the ownership-copy reduction and caller batching, but not
Bf-tree buffering or a concurrent commit-pipeline redesign.

### Storage-maintenance evidence

`TestStorageMaintenanceRepresentativeChurn` applies four generations to 48 MVCC cells, tombstones half, prunes every eligible non-latest cell version, and compacts with 16-key destination batches. It reports `StorageStats` after each phase and verifies that physical bytes never shrink during tombstone/prune operations, durable progress is monotonic and bounded, and the compacted database has fewer physical and unreachable bytes. Run the cancellation/retry contract beside it:

```bash
go test -count=3 -run '^(TestStorageMaintenanceRepresentativeChurn|TestCompactWithOptionsCancellationCanRetry)$' -v .
```

On the 2026-08-24 reference run (Go 1.26.3, 4 KiB pages), all three churn samples produced the same byte counts: puts `2,523,136` primary / `1,036,288` live / `1,486,848` reclaimable; tombstones `2,650,112` / `1,064,960` / `1,585,152`; prune `2,650,112` / `122,880` / `2,527,232`; and compact `106,496` / `106,496` / `0`. Pruning removed 168 stale cell rows. These are deterministic layout measurements for this fixture, not a production capacity forecast.

That historical result established the amount explicit compaction could recover
before format-v3 reuse was added. Compaction remains necessary for low-fill and
fragmented layouts; authenticated v3 can now reuse whole freed pages and reclaim
only a contiguous free tail without copying the database.

The generation-safe allocator comparison uses a 20 KiB incompressible overflow
value and one durable delete transaction plus one durable put transaction per
cycle. The control is the same authenticated format and page transform with
reuse disabled internally, isolating allocator behavior. Run five fixed-work
samples:

```bash
go test -run '^$' -bench '^BenchmarkAuthenticatedOverflowChurn$' \
  -benchmem -benchtime=100x -count=5 ./internal/engine
```

On the 2026-08-26 reference host (Go 1.27.0, Intel Core i9-14900HX), every
extend-only sample grew by exactly 29,008 primary bytes per cycle and every
reuse sample grew by zero after setup. Median latency was 186,833 ns/op for the
control and 180,247 ns/op for reuse; the ranges overlap, so latency is treated
as unchanged. Median allocation count increased from 217 to 232 per cycle due
to bounded freelist validation/materialization. Reopen, abort, stale-WAL,
external-chain, steady-state, and SIGKILL-before/after-truncate tests provide
the correctness gate. This justifies allocator state for bounded disk growth,
not as a latency optimization.

The super-hex correctness soak applies deterministic randomized puts, repeated
updates, and deletes at hierarchy levels 1, 2, and 3. After every batch it fully
catches up the derived index and compares every summary with an independently
computed occupancy map. It also checks cursor monotonicity and that no changelog
record remains unapplied.

The one-shot super-hex sync benchmark intentionally constructs a fresh database
for every sample so changelog history is held constant. Its supported invocation
uses `-benchtime=1x`; repeating it inside one benchmark process would make setup
cost dominate and would no longer represent a fixed-history sample.

## Collect comparable evidence

For a decision-quality series:

1. Record `git rev-parse HEAD`, `go env GOVERSION`, CPU model, operating system,
   storage type, and whether the machine was otherwise idle.
2. Use the same seed and workload flags for every comparison.
3. Run `task evidence-controlled` at least five times. Compare full benchmark
   output, including allocation counts; do not promote a change from one run.
4. Run `task evidence-observe` on a staging host whose CPU and storage resemble
   production. Retain each JSON file with the commit SHA in its filename.
5. Treat p95 and maximum latency, catch-up status, and resource growth as
   constraints alongside mean latency. A faster mean does not justify lost
   determinism, incorrect paths, stale summaries, or unbounded storage.

The observation runner is synthetic by design. To validate actual application
usage, instrument the caller at the three public boundaries—`FindEdgePath`,
`LoadContextFOV`, and `SuperHexSummaryIndex.Sync`—and aggregate the same fields
into bounded histograms and counters. Do not attach coordinates, content, raw
queries, edge labels, or database paths. Sample for a declared interval, compare
with the synthetic baseline, then remove or disable temporary instrumentation.

## Decision gates

Keep the current design unless the evidence shows a material unmet need. Revisit
the deferred ideas only when all applicable gates are met:

- **Alternative pathfinding/caches:** a representative path workload misses its
  latency target, and profiles attribute the cost to repeated graph traversal.
- **A different FOV algorithm/order:** deterministic nearest-first correctness
  remains intact and a representative radius/blocker distribution shows a
  material improvement beyond run-to-run noise.
- **Persistent or richer super-hex summaries:** rebuild or catch-up misses the
  startup/freshness target, or measured usage needs aggregates beyond occupancy;
  any proposal must also bound storage, recovery, and invalidation cost.

Evidence files are not canonical performance claims. Published numbers belong
in release notes or [`OPERATIONS.md`](OPERATIONS.md) only after repeated runs on
a documented machine.
