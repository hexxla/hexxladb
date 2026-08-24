# Performance evidence

This suite answers two separate questions about Dijkstra pathfinding,
deterministic field of view (FOV), MVCC hot-key reads, HNSW vector search, and
the aperture-7 super-hex occupancy prototype:

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

## Quick run

```bash
task evidence
```

This writes:

- `.tmp/evidence/fov-bench.txt`
- `.tmp/evidence/changelog-read-bench.txt`
- `.tmp/evidence/api-bench.txt`
- `.tmp/evidence/superhex-sync-bench.txt`
- `.tmp/evidence/storage-churn.txt`
- `.tmp/evidence/workload.json`

Run the streams separately when iteration time matters:

```bash
task evidence-controlled
task evidence-observe
task evidence-vector-scale
```

The default observation workload uses 2,000 cells, 100 samples, seed `1`, FOV
radius `10`, and super-hex level `2`. Override it without changing source:

```bash
task evidence-observe \
  EVIDENCE_ARGS='-cells 10000 -samples 500 -seed 7 -fov-radius 20 -superhex-level 3'
```

Input bounds are deliberate: at most 100,000 cells, 10,000 samples, and FOV
radius 512. This keeps an accidentally oversized observation run bounded.

The vector runner defaults to 10,000 32-dimensional unit vectors, 25 exact-
oracle query samples, recall@10, 100 updates plus 100 deletes, 4 KiB pages, and
a 64 MiB page cache. It writes aggregate JSON to
`.tmp/evidence/vector-scale.json`. Override bounded inputs and the output path:

```bash
task evidence-vector-scale \
  VECTOR_EVIDENCE_ARGS='-cells 10000 -dimension 384 -batch-size 100' \
  VECTOR_EVIDENCE_OUTPUT='.tmp/evidence/vector-scale-10000-384d.json'
```

## What is measured

| Area              | Controlled evidence                                                                                                                                        | Observation evidence                                                                                      |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Dijkstra          | API latency and allocations as graph out-degree grows                                                                                                      | p50/p95/max/mean latency, paths found, and aggregate hop count over seeded route queries                  |
| Deterministic FOV | Shadowcast algorithm, retained raycast comparison, and full `LoadContextFOV` API latency                                                                   | p50/p95/max/mean latency and aggregate number of returned cells with deterministic blockers               |
| Super-hex         | Rebuild, O(1) coordinate lookup, deterministic export, direct changelog tail reads across 512–100k historical records, and fixed-history one-shot catch-up | rebuild/write/sync distributions, changes processed, summary count, applied sequence, and caught-up state |
| Vector search     | Final 500×32d query latency and allocation benchmark versus the recorded pre-change baseline                                                               | Batched graph build, exact-oracle recall@k, query latency/path/breadth, reopen, churn, memory, and file sizes |
| MVCC hot keys     | Latest and historical point-read latency and allocations at 10, 100, 1,000, and 6,000 versions                                                           | Not included                                                                                                |
| Public writes     | Single, batched, callback-delayed, fdatasync, and reader-blocking latency with group-WAL batch/sync counts                                               | Not included                                                                                                |
| Storage churn     | Exact primary/live/reclaimable page bytes after puts, tombstones, pruning, and compaction; bounded progress and interruption tests                      | Final primary, WAL, and changelog sizes                                                                     |
| Resources         | Go allocation counts per benchmark operation                                                                                                               | total bytes allocated plus final database, WAL, and changelog sizes                                       |

### Vector-search evidence

The runner generates seeded unit vectors and queries in memory, builds the
persisted HNSW graph through the public transaction API, and compares each ANN
result with an exact cosine top-k oracle. It closes and reopens the database,
then updates and deletes the requested number of vectors, closes and reopens a
second time, and repeats the oracle comparison. It also requires the reported
execution path to be HNSW. The JSON contains aggregate durations and resource
counts only; the temporary database is removed.

The 2026-08-24 reference runs used an Intel Core i9-14900HX, Linux/amd64,
Go 1.27.0, seed 1, 25 queries, recall@10, 100 updates and 100 deletes, 4 KiB
pages, and a 64 MiB cache:

| Workload | Batch | Build | Query before reopen p50/p95 | Recall before/reopen/churn | Query after churn p50/p95 | Heap after build/churn | Primary/live/reclaimable |
| -------- | ----- | ----- | ---------------------------- | -------------------------- | --------------------------- | ---------------------- | ------------------------ |
| 10k×32d  | 500   | 78.1 s; 128.1 vectors/s | 5.75/8.65 ms | .992/.992/.992 | 4.80/7.03 ms | 18.4/25.0 MB | 23.87/23.07/0.80 MB |
| 10k×384d | 100   | 141.2 s; 70.8 vectors/s | 32.81/37.05 ms | .956/.956/.960 | 29.37/35.62 ms | 97.8/68.1 MB | 66.06/64.50/1.56 MB |

The 32-dimensional run used the minimum default `ef_search=100`; the
dimension-aware 384-dimensional run used `ef_search=384`. The latter setting
replaced a fixed-100 result of .63 recall@10 while retaining bounded query
latency. Total Go allocation over the complete build/query/reopen/churn process
was 132.1 GB for 32 dimensions and 237.9 GB for 384 dimensions. Those cumulative
allocation figures make graph construction an explicit offline or batched
write cost even though steady heap and database size remain bounded.

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

These measurements support 10,000 vectors at 32 and 384 dimensions with the
tested settings. They are not a claim of an unbounded capacity or a service-
level objective. For larger sets, other dimensions, non-random vector
distributions, stricter recall targets, or different hardware, rerun the same
command with representative inputs and retain the JSON before choosing
`EfSearch`, batch size, cache, or page settings. A new index or bulk builder
remains unjustified until that evidence misses an explicit target.

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

### Storage-maintenance evidence

`TestStorageMaintenanceRepresentativeChurn` applies four generations to 48 MVCC cells, tombstones half, prunes every eligible non-latest cell version, and compacts with 16-key destination batches. It reports `StorageStats` after each phase and verifies that physical bytes never shrink during tombstone/prune operations, durable progress is monotonic and bounded, and the compacted database has fewer physical and unreachable bytes. Run the cancellation/retry contract beside it:

```bash
go test -count=3 -run '^(TestStorageMaintenanceRepresentativeChurn|TestCompactWithOptionsCancellationCanRetry)$' -v .
```

On the 2026-08-24 reference run (Go 1.26.3, 4 KiB pages), all three churn samples produced the same byte counts: puts `2,523,136` primary / `1,036,288` live / `1,486,848` reclaimable; tombstones `2,650,112` / `1,064,960` / `1,585,152`; prune `2,650,112` / `122,880` / `2,527,232`; and compact `106,496` / `106,496` / `0`. Pruning removed 168 stale cell rows. These are deterministic layout measurements for this fixture, not a production capacity forecast.

The result supports explicit compaction: it recovered all whole unreachable pages and additional low-fill space without a freelist or format change. Persistent reuse remains evidence-gated because it would add allocator state that must be ordered with WAL replay; reconsider it only if measured compaction windows or peak disk budgets fail an operator requirement.

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
