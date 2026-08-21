# Spatial performance evidence

This suite answers two separate questions about Dijkstra pathfinding,
deterministic field of view (FOV), and the aperture-7 super-hex occupancy
prototype:

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
make evidence
```

This writes:

- `.tmp/evidence/fov-bench.txt`
- `.tmp/evidence/changelog-read-bench.txt`
- `.tmp/evidence/api-bench.txt`
- `.tmp/evidence/superhex-sync-bench.txt`
- `.tmp/evidence/workload.json`

Run the streams separately when iteration time matters:

```bash
make evidence-controlled
make evidence-observe
```

The default observation workload uses 2,000 cells, 100 samples, seed `1`, FOV
radius `10`, and super-hex level `2`. Override it without changing source:

```bash
make evidence-observe \
  EVIDENCE_ARGS='-cells 10000 -samples 500 -seed 7 -fov-radius 20 -superhex-level 3'
```

Input bounds are deliberate: at most 100,000 cells, 10,000 samples, and FOV
radius 512. This keeps an accidentally oversized observation run bounded.

## What is measured

| Area              | Controlled evidence                                                                                                                                        | Observation evidence                                                                                      |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Dijkstra          | API latency and allocations as graph out-degree grows                                                                                                      | p50/p95/max/mean latency, paths found, and aggregate hop count over seeded route queries                  |
| Deterministic FOV | Shadowcast algorithm, retained raycast comparison, and full `LoadContextFOV` API latency                                                                   | p50/p95/max/mean latency and aggregate number of returned cells with deterministic blockers               |
| Super-hex         | Rebuild, O(1) coordinate lookup, deterministic export, direct changelog tail reads across 512–100k historical records, and fixed-history one-shot catch-up | rebuild/write/sync distributions, changes processed, summary count, applied sequence, and caught-up state |
| Resources         | Go allocation counts per benchmark operation                                                                                                               | total bytes allocated plus final database, WAL, and changelog sizes                                       |

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
3. Run `make evidence-controlled` at least five times. Compare full benchmark
   output, including allocation counts; do not promote a change from one run.
4. Run `make evidence-observe` on a staging host whose CPU and storage resemble
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
