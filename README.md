<div align="center">

<img src="assets/images/hexxladb_logo_shadow.svg" alt="HexxlaDB" width="240">

# HexxlaDB

**Embedded database — hex grid, vector search, provenance, contradiction tracking.**

[![CI](https://github.com/hexxla/hexxladb/actions/workflows/ci.yml/badge.svg)](https://github.com/hexxla/hexxladb/actions/workflows/ci.yml)
[![Integration](https://github.com/hexxla/hexxladb/actions/workflows/integration.yml/badge.svg)](https://github.com/hexxla/hexxladb/actions/workflows/integration.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/hexxla/hexxladb.svg)](https://pkg.go.dev/github.com/hexxla/hexxladb)
[![Go 1.27](https://img.shields.io/badge/go-1.27-00ADD8?logo=go)](https://go.dev/doc/go1.27)
[![Version](https://img.shields.io/github/v/tag/hexxla/hexxladb?label=version&color=7c3aed)](https://github.com/hexxla/hexxladb/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

</div>

---

HexxlaDB is an embedded Go library for structured, spatially-organised records. Items live at hex grid coordinates — retrieval expands outward in rings, stays within an explicit result limit, and can respect validity windows. Cells carry provenance, confidence, and optional temporal bounds. Contradictions and supersessions are stored explicitly as seams.

Single binary, zero network dependencies, no daemon.

---

## How it works

Cells sit at `(q, r)` hex coordinates. Related records sit nearby when the application supplies a meaningful semantic anchor; `FindFreeCellPlacement` can select the exact free coordinate deterministically, but HexxlaDB does not infer semantic position. `LoadContext` walks outward from seed coordinates, applies caller-selected assembly options such as supersession resolution and seam inclusion, and returns a deterministic context pack bounded by result count.

| Primitive     | Description                                                                   |
| ------------- | ----------------------------------------------------------------------------- |
| **Cell**      | A record at `(q, r)` — content, tags, provenance, confidence, validity window |
| **Seam**      | Conflict or supersession marker linking two cells                             |
| **Edge**      | Directed relationship between cells (graph overlay)                           |
| **Facet**     | Summary or annotation with a source-content hash checked by `UpdateFacet`      |
| **Embedding** | Vector stored alongside a cell for HNSW similarity search                     |

---

## Quick start

```bash
go get github.com/hexxla/hexxladb@v0.6.0
```

Nine runnable examples cover conversational memory, LLM context assembly,
spatial algorithms, a remote-access ownership boundary, and reproducible
evidence workloads: [`examples/`](examples/).

### Open a database

```go
db, err := hexxladb.Open("memory.db", &hexxladb.Options{
    EnableMVCC: true,
})
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### Store a record without choosing an exact destination

```go
var storedAt hexxladb.Coord

if err := db.Update(func(tx *hexxladb.Tx) error {
    // The application owns the semantic anchor. HexxlaDB atomically selects
    // the first free coordinate within eight rings of it.
    anchor := hexxladb.Coord{Q: 0, R: 0}
    placement, err := tx.FindFreeCellPlacement(ctx, anchor, 8)
    if err != nil {
        return err
    }

    cell := hexxladb.NewFactCell(
        placement.Key,
        "Use testcontainers-go for integration tests with real Postgres.",
        "session-2",
        "testing",
        0.95,
    )
    if err := tx.PutCell(ctx, cell); err != nil {
        return err
    }

    // Optional: vectors come from the application. Omit this call when using
    // lexical, tag, provenance, temporal, or spatial retrieval only.
    if vectorFromYourModel != nil {
        if err := tx.PutEmbedding(placement.Key, vectorFromYourModel); err != nil {
            return err
        }
    }

    storedAt = placement.Coord
    return nil
}); err != nil {
    log.Fatal(err)
}
log.Printf("stored at (%d,%d)", storedAt.Q, storedAt.R)
```

`FindFreeCellPlacement` and `PutCell` must remain in the same `Update`: the
writable transaction prevents another writer from claiming the selected
coordinate. HexxlaDB chooses geometric space around the anchor; it does not
infer the anchor from content, tags, embeddings, or an LLM. When the exact
domain coordinate is already known, call `Pack` and `PutCell` directly. A direct
write to an occupied coordinate intentionally replaces the currently visible
cell.

### Search without embeddings

```go
var lexicalResults []hexxladb.CellQueryResult
if err := db.View(func(tx *hexxladb.Tx) error {
    var err error
    lexicalResults, err = tx.QueryCells(ctx, hexxladb.CellQuery{
        Query:         "testcontainers postgres",
        RequireTags:   []string{"fact"},
        MinConfidence: 0.5,
        MaxResults:    8,
        MaxScanRows:   1_000,
        SortBy:        hexxladb.SortByScore,
    })
    return err
}); err != nil {
    log.Fatal(err)
}
```

### Search by meaning with an application-provided vector

```go
var results []hexxladb.CellQueryResult
if err := db.View(func(tx *hexxladb.Tx) error {
    var err error
    results, err = tx.QueryCells(ctx, hexxladb.CellQuery{
        Embedding:     queryVector,
        ExcludeTags:   []string{"preference"},
        MinConfidence: 0.5,
        MaxResults:    8,
        SortBy:        hexxladb.SortByScore,
    })
    return err
}); err != nil {
    log.Fatal(err)
}
```

The query vector must use the same dimension as stored embeddings. HexxlaDB
maintains and searches the HNSW index locally but does not call an embedding
provider or track provider-specific tokenizers.

### Assemble bounded context candidates

```go
if len(results) == 0 {
    log.Fatal("no matching cells")
}
seeds := []hexxladb.Coord{results[0].Cell.Coord}
if len(results) > 1 {
    seeds = append(seeds, results[1].Cell.Coord)
}

var pack hexxladb.ContextPack
if err := db.View(func(tx *hexxladb.Tx) error {
    var err error
    pack, err = tx.LoadContext(ctx, hexxladb.LoadContextConfig{
        Seeds:     seeds,
        MaxRing:   2,
        MaxCells:  32,
        Assembly: hexxladb.ContextAssemblyConfig{
            FilterSuperseded: true,
            IncludeSeams:     true,
        },
    })
    return err
}); err != nil {
    log.Fatal(err)
}
log.Printf("assembled %d cells", len(pack.Cells))
```

`MaxCells` is an operational retrieval bound, not an LLM token estimate. Rank and render the returned candidates in the application, then apply the target provider/model tokenizer to the complete request—including instructions, history, tools, separators, and reserved output capacity.

### Track contradictions and supersessions

```go
if err := db.Update(func(tx *hexxladb.Tx) error {
    // Mark that newRecord supersedes oldRecord (e.g. updated belief, revised fact, preference change)
    return tx.MarkSupersedes(newCoord, oldCoord, "revised based on new evidence")
}); err != nil {
    log.Fatal(err)
}
```

> The library remains embedded and makes no network calls. Remote clients can
> use an application-owned service boundary without sharing database files; see
> the [remote-access example](examples/remote_access/).

### Safe file maintenance

The operator CLI preflights copy capacity and path collisions before migration
or compaction. Dry runs do not create a candidate or copy new migration batches:

```bash
hexxladb compact --dry-run -o memory.compacted.db memory.db
hexxladb migrate-v1-to-v2 --dry-run -o memory-v2.db memory-v1.db
HEXXLA_DESTINATION_PASSPHRASE='...' \
  hexxladb migrate-to-authenticated --dry-run -o memory-v3.db memory.db
```

Remove `--dry-run` to create a distinct candidate with durable progress and
post-copy health verification. Sources are never replaced or deleted. Encrypted
credentials come from named environment variables—not command arguments; see
the [`OPERATIONS.md` runbook](docs/hexxladb/OPERATIONS.md) before publication or
replacement.

---

## Use cases

The core primitives — spatial locality, provenance, contradiction tracking, result-bounded retrieval, MVCC snapshots, hybrid search — compose into patterns that are awkward to build on top of general-purpose stores.

- **Agent and LLM memory** — store conversation turns, facts, and preferences at hex coordinates; retrieve bounded candidates, rank and fit complete prompts in the application, and include stored contradictions when requested
- **Game world state** — hex-native tile storage with FOV for visibility queries, bounded radial context, Dijkstra pathfinding over weighted cell edges, and MVCC snapshots for save/rollback and replay
- **Knowledge graphs with temporal validity** — facts that expire or get superseded; belief revision via seams; time-travel to any past snapshot with `ViewAt`
- **Spatial annotation layers** — sensor readings, events, or annotations at coordinates; proximity queries via ring walks; confidence-weighted retrieval for noisy data
- **Audit trails and event sourcing** — optional recoverable at-least-once changelog with durable named consumer cursors; MVCC typed writes support point-in-time views and retained-history `SnapshotDiff` diagnostics
- **Personal knowledge management** — notes arranged spatially by topic proximity; contradiction surfacing between linked notes; supersession chains for evolving understanding
- **Simulation state** — reproducible snapshots between runs, diff for regression detection, spatial queries for proximity-based interactions

---

## Comparison

| Capability                         | HexxlaDB | Vector DBs | Graph DBs | General KV |
| ---------------------------------- | :------: | :--------: | :-------: | :--------: |
| Semantic search (HNSW)             |    ✓     |     ✓      |     —     |     —      |
| Structured filters in same query   |    ✓     |  partial   |     ✓     |     ✓      |
| Contradiction tracking             |    ✓     |     —      |     —     |     —      |
| Supersession chains                |    ✓     |     —      |     —     |     —      |
| Deterministically bounded context  |    ✓     |     —      |     —     |     —      |
| Spatial locality (ring walks)      |    ✓     |     —      |     —     |     —      |
| Graph pathfinding (Dijkstra, BFS)  |    ✓     |     —      |     ✓     |     —      |
| MVCC time-travel                   |    ✓     |     —      |     —     |  partial   |
| Provenance + confidence per memory |    ✓     |     —      |     —     |     —      |
| Embedded (no network)              |    ✓     |     —      |     —     |     ✓      |
| Encryption at rest                 |    ✓     |   varies   |     —     |     ✓      |

New encrypted databases use authenticated XChaCha20-Poly1305 engine format v3,
including authenticated headers and keyed WAL publication. Legacy encrypted
v1/v2 AES-XTS files remain readable but confidentiality-only until migrated.
See [`ENCRYPTION.md`](docs/hexxladb/ENCRYPTION.md) for the exact threat model and
rollback limits.

---

## API

Public API guide: [`docs/hexxladb/API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md)

| Operation                                               | What it does                                                            |
| ------------------------------------------------------- | ----------------------------------------------------------------------- |
| `FindFreeCellPlacement`                                 | Select a collision-safe cell key near a caller-owned semantic anchor    |
| `PutCell` / `GetCell` / `DeleteCell`                    | Store, retrieve, or tombstone a memory                                  |
| `PutEmbedding` / `SearchByEmbedding`                    | Store a vector; HNSW nearest-neighbor search                            |
| `PutEmbeddingWithOptions` / `RebuildEmbeddingIndex`     | Defer graph work for bulk ingestion, then validate and publish atomically |
| `SearchByEmbeddingWithStats`                            | Search with the selected HNSW/flat path and effective breadth           |
| `QueryCells` / `SearchCells`                            | Hybrid ANN+filter search or multi-term lexical search                   |
| `LoadContext` / `LoadContextFOV` / `LoadContextVoronoi` | Result-bounded context assembly — ring walk, FOV, or multi-seed Voronoi |
| `FindEdgePath` / `WalkEdges`                            | Weighted shortest path and BFS reachability over cell edges             |
| `NewSuperHexSummaryIndex` + summary methods             | Rebuildable aperture-7 occupancy summaries maintained from changelog    |
| `MarkConflict` / `MarkSupersedes` / `FindSeams`         | Record and retrieve contradictions and supersessions                    |
| `ViewAt` / `ViewAtTime` / `SnapshotDiff`                | MVCC time-travel and change detection                                   |
| `ReadChangelogSince` / durable consumer cursor methods  | Consume and checkpoint the optional recoverable at-least-once changefeed |
| `WriteStats` / `GroupWALStats`                          | Observe write contention, phase timing, and WAL batching                |
| `BackupTo` / `StorageStats` / `ReclaimTail` / `CompactWithOptions` | Back up an open database and manage physical storage          |
| `PreflightCompactTo` / migration preflights             | Validate maintenance paths, source state, credentials, and conservative capacity |
| `MigrateV1ToV2` / `MigrateToAuthenticated`              | Source-preserving offline migration into MVCC v2 or authenticated encrypted v3 |
| `HealthCheck`                                           | Fail closed on malformed visible records and optionally validate secondary indexes |

---

## Performance

```bash
task bench-api
```

The current release-candidate snapshot below was measured after the bounded-work,
query-planner, integrity, and HNSW remediation on 2026-08-26. Values are medians
of five independent samples; ranges show the observed minimum and maximum.

| Operation | Median | Five-sample range | Workload |
| --------- | ------ | ----------------- | -------- |
| `GetCell`, plaintext v1 | 2.08 µs | 1.48–4.79 µs | 2,000 cells |
| `GetCell`, plaintext MVCC v2 | 17.9 µs | 15.4–22.2 µs | 2,000 cells |
| `GetCell`, authenticated MVCC v3 | 16.4 µs | 10.3–18.8 µs | 2,000 cells |
| `PutCell`, MVCC | 122 µs | 93.1–139 µs | one indexed cell and durable commit |
| `BatchPutCells`, MVCC | 51.1 µs/cell | 48.9–55.1 µs/cell | 100 cells per durable commit |
| `WalkRing` | 17.7 µs | 14.9–27.9 µs | radius 2, 2,000-cell database |
| `QueryCells`, combined filters | 128 µs | 126–201 µs | radius 5 + source + confidence, 2,000 cells |
| `LoadContext` | 470 µs | 406–496 µs | radius 5, 64-cell result limit, 2,000 cells |
| `LoadContextFOV` | 132 µs | 125–142 µs | open radius 5, 2,000 cells |
| `LoadContextVoronoi` | 604 µs | 505–729 µs | four seeds, radius 4, 2,000 cells |
| `FindSeams` | 610 µs | 585–616 µs | 100 seams, radius 3 |
| `SearchCells`, lexical | 4.84 ms | 4.41–5.34 ms | one term, 2,000 cells |
| `QueryCells`, embedding | 837 µs | 667 µs–1.04 ms | 500 identical 32d vectors, unfiltered top 10 |
| `HealthCheck` | 1.26 ms | 1.18–1.36 ms | 2,000 cells, all optional checks enabled |
| MVCC latest resolution | 4.74 µs | 4.49–7.76 µs | one key with 6,000 retained versions |
| `SnapshotDiff` | 430 µs | 383–589 µs | 500 retained writes |

These are synthetic, warm-cache measurements on an Intel Core i9-14900HX,
Linux/amd64, Go 1.27.0, and the local filesystem—not service-level objectives.
Writes include the durable commit barrier. The embedding row intentionally does
not claim filtered recall or large-scale behavior. Exact commands, allocation
counts, dated vector-scale recall evidence, and interpretation are in
[`PERFORMANCE_EVIDENCE.md`](docs/hexxladb/PERFORMANCE_EVIDENCE.md). Benchmark
representative application data before setting latency, throughput, or capacity
expectations.

---

## Examples

| Example                                                                    | Run                               | What it covers                                                        |
| -------------------------------------------------------------------------- | --------------------------------- | --------------------------------------------------------------------- |
| [Conversational Memory](examples/conversational_memory/)                   | `task demo`                       | Cells, seams, tags, MVCC, queries, context, FOV, pathfinding          |
| [LLM Context Engine](examples/llm_context_engine/)                         | `task demo-llm`                   | Ollama embeddings, semantic search, supersession, FOV                 |
| [Spatial Algorithms](examples/spatial_algorithms/)                         | `task demo-spatial`               | FOV, radial context, Voronoi, Dijkstra, BFS — side-by-side            |
| [Remote Access Owner Service](examples/remote_access/)                     | `task demo-remote`                | Loopback service, authentication, admission, and single file owner   |
| [Performance Evidence](examples/performance_evidence/)                     | `task evidence-observe`           | Dijkstra, FOV, super-hex sync, allocation, and storage observations   |
| [Write-path Evidence](examples/write_path_evidence/)                       | `task evidence-write-path`        | Bounded write latency, throughput, allocation, and file growth       |
| [Vector Scale Evidence](examples/vector_scale_evidence/)                   | `task evidence-vector-scale`      | Synchronous/deferred HNSW build, recall, reopen, churn, memory, and disk |
| [Lattice Placement Evidence](examples/lattice_placement_evidence/)         | `task evidence-lattice-placement` | Placement stability and semantic/spatial divergence                   |
| [Conservative Pilot Soak](examples/pilot_soak/)                            | `task soak-pilot`                 | Sustained mixed load, SLO gates, encrypted backup/restore, resources  |

The LLM example requires [Ollama](https://ollama.com/): `ollama pull all-minilm && task demo-llm`

---

## Caveats

- **Write throughput** — B+ tree commits remain serialized and are not suited for high-volume independent random writes. Use bounded batch writes (`db.Update` with many operations) where possible, and run `task evidence-write-path` on representative storage to measure single-cell, 100-cell, and 32-dimensional cell/vector commit gates.
- **Embedded, single-owner files** — the core has no built-in network server,
  and exactly one process owns a database's files. Remote clients must use an
  application-owned transport with authentication, authorization, admission,
  and TLS. The bounded [remote-access example](examples/remote_access/)
  validates that boundary but is not a production service product.
- **Measured HNSW envelope** — the deferred lifecycle passes 20,000 vectors at 32 dimensions and 10,000 at 384 dimensions with 4 KiB pages and a 64 MiB page-cache budget. Deferred writes use exact flat search until a bounded `RebuildEmbeddingIndex` validates and atomically publishes HNSW; the default/hard rebuild bounds are 10,000/20,000 vectors and preflight also enforces a memory estimate and available filesystem space. This is evidence for those tested tiers, not an unbounded capacity claim; run `task evidence-vector-scale` with representative vectors before relying on other sizes, dimensions, or distributions. `SearchByEmbeddingWithStats` reports the selected path.
- **Filtered ANN is approximate and bounded** — structured filters are applied to progressively widened semantic candidates, up to `CellQuery.EmbeddingCandidateLimit` or its adaptive default. A deliberately low limit can underfill results even when qualifying vectors exist outside the window; raise it within `MaxEmbeddingFilterCandidates` and measure filter-selective recall when that matters.
- **Coordinates are sparse** — hex grid is a logical namespace, not a dense array. No compaction of coordinate space happens automatically.
- **Semantic placement is caller-owned** — the database does not infer anchors from content, tags, embeddings, or model providers. `FindFreeCellPlacement` resolves the bounded geometric collision search around an anchor; preserve existing coordinates during incremental insertion and measure semantic/lattice divergence with representative records.
- **Storage reclaim is format-dependent** — authenticated v3 transactions reuse whole freed B+ tree and overflow pages before extending the primary; `ReclaimTail` safely truncates a contiguous reusable suffix. Plaintext and legacy formats remain extend-only, and every format still needs explicit compaction to repack low-fill pages or fragmented historical layouts. Inspect `StorageStats` before maintenance.
- **Rollback integrity** — authenticated v3 detects page modification, cross-page swaps, header/WAL tampering, and stale-root replay. Same-slot replay of an older valid non-root page and coordinated rollback of the complete recovery set require a trusted per-page catalog or external monotonic anchor; use independently authenticated, versioned backups. Legacy AES-XTS pages remain confidentiality-only.
- **Pre-v1** — API may change between minor versions during the v0.y.z phase;
  the candidate/provisional inventory and measurable graduation gates are in
  [`VERSIONING.md`](VERSIONING.md).
- **Explicit format upgrade** — format-v1 files are never auto-upgraded. Use
  `MigrateV1ToV2` or the preflighted `hexxladb migrate-v1-to-v2` workflow with
  a distinct destination. Use `MigrateToAuthenticated` or
  `hexxladb migrate-to-authenticated` for a source-preserving v1/v2-to-v3
  upgrade. Incomplete candidates are refused or removed; older libraries refuse
  v3 and there is no downgrade writer.

---

## Documentation

| Document                                                           | What's inside                                            |
| ------------------------------------------------------------------ | -------------------------------------------------------- |
| [`CONFIGURATION.md`](docs/hexxladb/CONFIGURATION.md)               | All `Options` fields, common configs, encryption setup   |
| [`API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md)               | Task-oriented public API guide                           |
| [`HEXXLA.md`](docs/hexxladb/HEXXLA.md)                             | Memory model: hex lattice, seams, validity, supersession |
| [`HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md)                       | Storage layout, key encoding, HNSW keyspace              |
| [`OPERATIONS.md`](docs/hexxladb/OPERATIONS.md)                     | Production ops, benchmarks, backup, encryption           |
| [`PERFORMANCE_EVIDENCE.md`](docs/hexxladb/PERFORMANCE_EVIDENCE.md) | Correctness, performance, and storage evidence            |
| [`ROADMAP.md`](docs/ROADMAP.md)                                    | What's next and what's out of scope                      |
| [`VERSIONING.md`](VERSIONING.md)                                   | Compatibility matrix, API inventory, and v1 gates       |

---

## Projects using HexxlaDB

- **[Hexxla](https://github.com/hexxla/hexxla)** — spatial LLM memory and reasoning stack
- **[Mosaic](https://github.com/hexxla/mosaic)** — local MCP server for structured agent memory on HexxlaDB

---

## Contributing

See **[CONTRIBUTING.md](CONTRIBUTING.md)**.

```bash
git clone https://github.com/hexxla/hexxladb.git
cd hexxladb
go mod tidy
task test
```

---

## Contact

- [Issues](https://github.com/hexxla/hexxladb/issues) — bugs and feature requests
- [Discussions](https://github.com/hexxla/hexxladb/discussions) — questions and ideas
