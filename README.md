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
go get github.com/hexxla/hexxladb
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

### Store a record and its embedding

```go
if err := db.Update(func(tx *hexxladb.Tx) error {
    coord := hexxladb.Coord{Q: 3, R: 1}
    pk, err := hexxladb.Pack(coord)
    if err != nil {
        return err
    }

    err = tx.PutCell(ctx, hexxladb.CellRecord{
        Key:        pk,
        RawContent: "Use testcontainers-go for integration tests with real Postgres.",
        Tags:       []string{"fact", "testing", "database"},
        Provenance: hexxladb.ProvenanceWire{SourceID: "session-2", Confidence: 0.95},
    })
    if err != nil {
        return err
    }
    return tx.PutEmbedding(pk, vectorFromYourModel) // HNSW index maintained automatically
}); err != nil {
    log.Fatal(err)
}
```

### Search by meaning

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
- **Game world state** — hex-native tile storage with FOV for visibility queries, Dijkstra pathfinding over weighted cell edges, LOD for distant regions, MVCC snapshots for save/rollback and replay
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
| `HealthCheck`                                           | Validate visible records and secondary indexes                          |

---

## Performance

```bash
task bench-api
```

_Intel Core i9-14900HX · 16 GB · Linux. API benchmark rows: Go 1.26–1.27, `-benchtime=3s -count=1`; vector-scale rows: Go 1.27.0, seeded aggregate workload._

### Reads and ring traversal

| Operation                             | Latency | Notes                                                                     |
| ------------------------------------- | ------- | ------------------------------------------------------------------------- |
| `GetCell` plaintext v1 (2k cells)     | ~1.8 µs | O(log n) B+ tree                                                          |
| `GetCell` plaintext MVCC v2 (2k cells)| ~16.4 µs| Bounded MVCC version seek                                                 |
| `GetCell` authenticated v3 (2k cells) | ~16.3 µs| MVCC + XChaCha20-Poly1305; within run variation of plaintext MVCC          |
| `WalkRing` r=2 (19 cells/walk, 2k DB) | ~162 µs | Scales with ring area, not DB size                                        |
| `QueryCells` tag-only (2k cells)      | ~15 µs  | Index-only; no page reads                                                 |
| `QueryCells` spatial r=5 (2k DB)      | ~634 µs | 91-cell ring area walk + filter (3r²+3r+1)                                |
| `QueryCells` combined (2k cells)      | ~62 ms  | source + spatial + confidence + sort; use narrower predicates in practice |
| `FindSeams` zero-seam fast-path       | ~1.3 µs | Pre-flight check; effectively free                                        |
| `FindSeams` 100 seams                 | ~995 µs | Seam index scan                                                           |

### Context assembly

| Operation                                    | Latency  | Notes                                               |
| -------------------------------------------- | -------- | --------------------------------------------------- |
| `LoadContext` r=3 (37 cells/walk, 2k DB)     | ~185 µs  | Nearest-first; 64-cell result limit                 |
| `LoadContext` r=5 (91 cells/walk, 2k DB)     | ~419 µs  | Stops at 64 cells; ring area = 3r²+3r+1             |
| `LoadContextFOV` r=3 (≤37 cells/walk, 2k DB) | ~526 µs  | FOV prunes occluded cells; faster than plain r=3    |
| `LoadContextFOV` r=5 (≤91 cells/walk, 2k DB) | ~1.30 ms | Open-field (no occlusion) — worst case for FOV      |
| `LoadContextVoronoi` 2 seeds (2k DB)         | ~2.1 ms  | Each seed gets up to r=4, 61 cells; non-overlapping |
| `LoadContextVoronoi` 4 seeds (2k DB)         | ~4.4 ms  | Scales linearly with seed count                     |

### Writes

| Operation                           | Latency        | Notes                                                |
| ----------------------------------- | -------------- | ---------------------------------------------------- |
| `PutCell` single write              | ~0.57 ms/op    | Single-writer B+ tree; durable commit, no wait window |
| `PutCell` MVCC                      | ~0.53 ms/op    | Version and secondary-index rows                     |
| `BatchPutCells` batch=100           | ~0.063 ms/cell | About 8× lower per-cell latency than single MVCC writes |
| `PutEmbedding` dim=32 (HNSW insert) | ~53 ms/op      | Full HNSW graph maintenance per write                |
| `PutEmbedding` dim=384              | ~74 ms/op      | Encode + graph insert scales with dimension          |
| Deferred HNSW build 10k×32d         | ~9.28 s total  | ~1,078 vectors/s; bounded build plus atomic publish  |
| Deferred HNSW build 10k×384d        | ~36.0 s total  | ~278 vectors/s; dimension-aware recall profile       |

### Semantic and lexical search

| Operation                         | Latency   | Notes                                                  |
| --------------------------------- | --------- | ------------------------------------------------------ |
| `QueryCells` embedding (500×32d)  | ~13 ms    | Full HNSW ANN + post-filter pipeline                   |
| `QueryCells` embedding (500×128d) | ~11 ms    | Higher dim; fewer graph candidates needed              |
| HNSW search (10k×32d, recall@10=.992)  | ~5.4 ms p50  | 4 KiB pages, 64 MiB cache, `ef_search=100`          |
| HNSW search (10k×384d, recall@10=.956) | ~30.6 ms p50 | 4 KiB pages, 64 MiB cache, `ef_search=384`          |
| `SearchCells` lexical (2k cells)  | ~28–41 ms | Full-scan scorer; pre-filter with tags or source first |

### MVCC and maintenance

| Operation                                         | Latency  | Notes                                                   |
| ------------------------------------------------- | -------- | ------------------------------------------------------- |
| MVCC latest resolution (100 versions)             | ~6 µs    | Reverse B+ tree seek; does not scan older versions      |
| MVCC latest resolution (6,000 versions)           | ~12–14 µs | Growth follows tree depth/page occupancy, not chain scan |
| MVCC historical resolution (6,000 versions)       | ~10–15 µs | Seeks directly to the greatest version at `read_seq`    |
| `SnapshotDiff` (10 retained writes)               | ~159 µs  | Retained MVCC diagnostic; scales with scanned history    |
| `SnapshotDiff` (500 retained writes)              | ~6.9 ms  | Use narrow sequence windows for large histories          |
| `Compact` (512 cells)                             | ~67 ms   | Copy-compaction; run after heavy delete/prune            |
| `Compact` (2k cells)                              | ~236 ms  | One-time cost; DB is read-only during copy               |

### Performance context

HexxlaDB's "write" and "read" are not equivalent to raw KV operations. `PutCell` writes a structured primary record and every applicable source, tag, and validity index row. When the changelog is enabled, the same authoritative commit also stores recoverable outbox intent before projecting it to the sidecar. `GetCell` deserialises provenance, tags, and validity data on top of the B+ tree lookup. On the reference run above, individual cell writes measured about 0.53–0.57 ms and `BatchPutCells` amortised the commit barrier to about 0.063 ms per cell.

The context assembly operations (`LoadContext`, `LoadContextFOV`, `LoadContextVoronoi`) have no direct equivalent in general KV stores — they replace what would otherwise be multiple sequential scans and spatial assembly passes. Product ranking, prompt rendering, and model-specific token accounting remain application responsibilities.

---

## Examples

| Example                                                                    | Run                               | What it covers                                                        |
| -------------------------------------------------------------------------- | --------------------------------- | --------------------------------------------------------------------- |
| [Conversational Memory](examples/conversational_memory/)                   | `task demo`                       | Cells, seams, tags, MVCC, queries, context, FOV, pathfinding          |
| [LLM Context Engine](examples/llm_context_engine/)                         | `task demo-llm`                   | Ollama embeddings, semantic search, supersession, FOV                 |
| [Spatial Algorithms](examples/spatial_algorithms/)                         | `task demo-spatial`               | FOV, LOD, Voronoi, Dijkstra, BFS — side-by-side                       |
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
