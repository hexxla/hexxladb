<div align="center">

<!-- Banner image placeholder - replace with actual image when available -->

<!-- <img src="docs/assets/banner.png" alt="HexxlaDB" width="800"> -->

# HexxlaDB

**Spatial memory for LLMs that never forgets where things are.**

</div>

HexxlaDB is an embedded database that stores memories on a **honeycomb grid**. Each memory lives at a specific coordinate. When you need context for an LLM, you pick a starting point and the database gathers nearby memories in a predictable, repeatable order—perfect for fitting within token budgets.

Most importantly: **when two memories disagree, HexxlaDB doesn't hide it.** It marks the conflict with a "seam" so your LLM can see there's a contradiction and handle it intelligently.

---

## The core concepts (explained simply)

### 🧩 Cells

A **cell** is a single memory—a fact, a message, a document chunk—stored at one hex coordinate `(q, r)`. Every cell has:

- **Raw content** — the actual text
- **Tags** — topics like "preferences" or "project-alpha"
- **Provenance** — who said it and how confident we are
- **Validity window** — when this memory is "true" in the real world

### 🔗 Edges

Connect related cells: _"this memory references that one."_ Think of them as "see also" links between tiles on the honeycomb.

### ✨ Facets

Short summaries attached to a cell—like a sticky note on a tile. Each facet is cryptographically tied to the original cell content, so you know the summary matches what was actually stored.

### 🎗️ Seams

The star of the show. When two cells contradict each other, you create a **seam**—a visible marker that says "these two beliefs disagree." Unlike other systems that silently overwrite or hide conflicts, HexxlaDB makes contradictions **first-class citizens** that can appear in context assembly.

Seams have:

- Type and reason (why they contradict)
- Confidence delta (how much this changes certainty)
- Resolution status (can be marked resolved when reconciled)

---

## Quick start

```bash
go get github.com/hexxla/hexxladb
```

### Store your first memory

```go
package main

import (
    "context"
    "log"

    "github.com/hexxla/hexxladb"
    "github.com/hexxla/hexxladb/internal/lattice"
    "github.com/hexxla/hexxladb/internal/record"
)

func main() {
    db, err := hexxladb.Open("memories.db", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Pick a coordinate on the honeycomb grid
    center := lattice.Coord{Q: 0, R: 0}
    pk, _ := lattice.Pack(center)

    // Store a memory with metadata
    rec := record.CellRecord{
        Key:        pk,
        RawContent: "User prefers concise responses.",
        Tags:       []string{"preferences"},
        Provenance: record.ProvenanceWire{
            SourceID:   "session-001",
            Confidence: 0.95,
        },
    }

    db.Update(func(tx *hexxladb.Tx) error {
        return tx.PutCell(context.Background(), rec)
    })
}
```

### Load context for an LLM prompt

```go
// Gather nearby memories in predictable order
db.View(func(tx *hexxladb.Tx) error {
    // Load up to 50 cells within 3 rings of center
    cells, err := tx.LoadContext(ctx, center, 3, 50)
    // cells ordered: center, ring 1, ring 2... same seed = same order
    return err
})
```

### Respect token budgets

```go
// Automatically evict outer rings if over budget
pack, err := tx.LoadContextPack(ctx, center, 3, 4000,
    hexxladb.ByteLenBudgeter{}, cfg)

// Or use your own tokenizer
pack, err := tx.LoadContextPack(ctx, center, 3, maxTokens,
    YourTokenizer{}, cfg)
```

### Mark a contradiction

```go
// Two cells disagree—create a visible seam
db.Update(func(tx *hexxladb.Tx) error {
    return tx.MarkConflict(cellA, cellB, "Contradicting preferences")
})

// Later, find seams near a coordinate
db.View(func(tx *hexxladb.Tx) error {
    seams, _ := tx.FindSeams(ctx, center, 2, true) // unresolved only
    for _, seam := range seams {
        log.Printf("Conflict: %s vs %s - %s",
            seam.CellA, seam.CellB, seam.Reason)
    }
    return nil
})
```

---

## Why use HexxlaDB for LLM memory?

| What you need              | How HexxlaDB delivers                                                                 |
| -------------------------- | ------------------------------------------------------------------------------------- |
| **Spatial locality**       | Memories on a hex grid; `LoadContext` expands outward in predictable ring order       |
| **Token budgets**          | `LoadContextPack` respects your budget; deterministic eviction from outer rings first |
| **Contradiction handling** | Seams make conflicts visible to the LLM instead of hiding them                        |
| **Reproducibility**        | Same seed → same cell order → same LLM prompt every time                              |
| **Audit & provenance**     | Every cell tracks source and confidence; immutable raw content with facets            |
| **Time-aware queries**     | Validity windows say when a memory applies; MVCC snapshots say what the DB knew when  |
| **Production-ready**       | Durable storage, crash recovery, optional encryption, changefeeds                     |

---

## Why HexxlaDB is groundbreaking

LLM memory is broken. Current solutions fail in predictable ways:

| The problem                            | How others fail                                             | How HexxlaDB wins                                                                                            |
| -------------------------------------- | ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| **Context windows are tiny band-aids** | RAG systems dump text into prompts with no coherence        | HexxlaDB's honeycomb grid loads _spatially relevant_ context—nearby cells are semantically related by design |
| **Users contradict themselves**        | Vector DBs silently overwrite; chat history hides old turns | **Seams** track contradictions explicitly—"user said A, then said not-A" is visible to the LLM               |
| **"What did they say 100 turns ago?"** | Most systems forget or require full-scan search             | Temporal queries + MVCC snapshots—ask what the DB knew at any point in time                                  |
| **Where was that fact?**               | Pure vector search loses spatial/provenance context         | `PackedCoord` primary keys—every memory has a location; load neighbors and see relationships                 |
| **Token budget chaos**                 | Naive truncation destroys narrative coherence               | `LoadContextPack` evicts outer _rings_ first—spatial locality preserves semantic flow                        |

### Comparison with existing approaches

**Vector databases** (Pinecone, Weaviate, Chroma)

- Great at: Billion-scale similarity search
- Fail at: Contradiction tracking, provenance, spatial coherence
- Why HexxlaDB: We have _seams_—no vector DB tracks when users change their minds

**Graph databases** (Neo4j)

- Great at: Complex relationship analytics, Cypher queries
- Fail at: Hex-native addressing, seam semantics, embedded deployment
- Why HexxlaDB: Edges + hex coordinates as primary keys—no translation layer needed

**Temporal databases** (Datomic)

- Great at: Time-travel queries, immutable history
- Fail at: Spatial indexing, contradiction visibility, LLM-specific context assembly
- Why HexxlaDB: MVCC + _spatial_ rings + seams designed for token budgets

**General-purpose stores** (Postgres, SQLite)

- Great at: Ubiquity, extensions, reliability
- Fail at: First-class hex coordinates, seam primitives, provenance/validity as core
- Why HexxlaDB: The honeycomb isn't bolted-on—it's the foundational addressing scheme

### What makes this category-defining

**Nothing else combines:**

1. **Hex-native Morton-ordered keys**—ring walks as prefix scans, not graph traversals
2. **Seams as first-class citizens**—conflicts are visible, resolvable, auditable
3. **Hybrid spatial + future semantic**—`embed/` keyspace coming for ANN seed selection
4. **Embedded, no network**—in-process, deterministic, reproducible

If you ship the roadmap—especially `embed/` for vector + hex hybrid retrieval—this becomes the **de facto standard** for LLM long-term memory.

---

## API Overview

HexxlaDB provides a focused API for spatial memory management. See [`docs/hexxladb/API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md) for the complete reference.

**Core types:**

- `Cell` — memories at coordinates with tags, provenance, validity windows
- `Edge` — directed relationships between cells
- `Facet` — derived summaries cryptographically tied to cells
- `Seam` — visible contradiction markers between cells

**Key operations:**

- `PutCell` / `GetCell` — store and retrieve memories
- `LoadContext` — gather nearby cells for context assembly
- `LoadContextPack` — token-budgeted neighborhood loading
- `MarkConflict` / `FindSeams` — contradiction tracking
- `ViewAt` / `ViewAtTime` — MVCC snapshot reads

---

## Learn more

Run the interactive tour:

```bash
# Conversational memory service demo
go run ./examples/conversational_memory
```

| Documentation                                        | What's inside               |
| ---------------------------------------------------- | --------------------------- |
| [`API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md) | Complete API reference      |
| [`HEXXLA.md`](docs/hexxladb/HEXXLA.md)               | Memory model and concepts   |
| [`HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md)         | Storage layout and keys     |
| [`OPERATIONS.md`](docs/hexxladb/OPERATIONS.md)       | Production operations guide |

---

## Benchmarks

```bash
make bench-api   # DB files land in .tmp/; safe to run manually
```

HexxlaDB benchmarks are organised around the operations that matter for an embedded hex-native LLM memory store. Each category reflects a distinct performance characteristic of the architecture.

**1. Write throughput — `PutCell`** (touches primary key + 3 secondary indexes per write)

| Operation      | DB size | ns/op      | allocs/op |
| -------------- | ------- | ---------- | --------- |
| PutCell        | fresh   | 8,344,367  | 352       |
| PutCell (MVCC) | fresh   | 10,152,951 | 435       |

_Each iteration is one full `DB.Update` round-trip including fsync. MVCC adds ~22% overhead for version key writes._

**1b. Batch write throughput — `BatchPutCells`**

| Batch size | ns/op       | cells/op | ms/cell |
| ---------- | ----------- | -------- | ------- |
| 10         | 25,328,095  | 10       | 2.5     |
| 100        | 54,932,839  | 100      | 0.55    |
| 500        | 167,561,582 | 500      | 0.34    |

_Amortises fsync cost across cells. Batch=100 is ~15× cheaper per cell than single `PutCell`. Each iteration opens a fresh DB, commits one batch, and closes._

**2. Point read latency — `GetCell`**

| Operation                  | DB size    | ns/op   | allocs/op |
| -------------------------- | ---------- | ------- | --------- |
| GetCell                    | 512 cells  | 27,942  | 88        |
| GetCell                    | 2000 cells | 29,249  | 111       |
| GetCell (encrypted)        | 512 cells  | 537,138 | 88        |
| GetCell (encrypted)        | 2000 cells | 560,101 | 111       |
| GetCell (MVCC + encrypted) | 512 cells  | 764,150 | 87        |
| GetCell (MVCC + encrypted) | 2000 cells | 762,257 | 122       |

_Plain reads are stable across DB sizes — confirming O(log n) B+ tree traversal. Encryption adds ~19× overhead (AES-GCM page decryption)._

**3. Context assembly — LLM hot path**

| Operation              | DB size    | radius | ns/op     | allocs/op |
| ---------------------- | ---------- | ------ | --------- | --------- |
| LoadContext            | 512 cells  | 3      | 449,411   | 2,944     |
| LoadContext            | 2000 cells | 3      | 501,109   | 3,804     |
| LoadContextPack (4 KB) | 512 cells  | 1      | 209,470   | 836       |
| LoadContextPack (4 KB) | 512 cells  | 3      | 778,542   | 3,750     |
| LoadContextPack (4 KB) | 512 cells  | 5      | 1,648,649 | 8,588     |
| LoadContextPack (4 KB) | 2000 cells | 1      | 224,835   | 1,081     |
| LoadContextPack (4 KB) | 2000 cells | 3      | 883,607   | 4,976     |
| LoadContextPack (4 KB) | 2000 cells | 5      | 2,036,354 | 11,744    |

_Cost scales with ring area (r=1: 7 cells, r=3: 37 cells, r=5: 91 cells), not DB size — spatial locality from Morton-ordered keys confirmed. Pre-sized candidate slices and ring-buffer reuse (`RingInto`) eliminated repeated growth doublings._

**4. Spatial ring walk — Morton-order prefix scan**

| Operation | DB size    | ring | ns/op   | allocs/op |
| --------- | ---------- | ---- | ------- | --------- |
| WalkRing  | 512 cells  | 2    | 156,488 | 927       |
| WalkRing  | 2000 cells | 2    | 171,672 | 1,203     |

_Near-constant across DB sizes — range scan on Morton-packed keys, not a graph traversal._

**5. Query engine — `QueryCells` predicate shapes**

| Predicate              | DB size    | ns/op      | allocs/op |
| ---------------------- | ---------- | ---------- | --------- |
| Tag filter (miss)      | 512 cells  | 47,380     | 82        |
| Tag filter (miss)      | 2000 cells | 47,891     | 103       |
| Source index scan      | 512 cells  | 7,381,741  | 50,851    |
| Source index scan      | 2000 cells | 30,012,042 | 210,350   |
| Spatial radius (r=3)   | 512 cells  | 469,566    | 2,948     |
| Spatial radius (r=3)   | 2000 cells | 519,121    | 3,808     |
| Combined (src+spatial) | 512 cells  | 7,333,957  | 50,845    |
| Combined (src+spatial) | 2000 cells | 29,212,830 | 210,340   |

_Tag miss exits early via index. Source scan cost is linear in matching rows — use `CellQuery.MaxScanRows` to bound worst-case latency. Spatial radius is bounded by ring area regardless of DB size._

**6. Seam resolution — `FindSeams`**

| Operation | seams in radius 3 | ns/op     | allocs/op |
| --------- | ----------------- | --------- | --------- |
| FindSeams | 0                 | 25,925    | 3         |
| FindSeams | 10                | 2,140,732 | 4,326     |
| FindSeams | 50                | 2,256,158 | 5,157     |
| FindSeams | 100               | 2,343,751 | 6,383     |

_Zero-seam base cost reduced from ~2.3 ms to ~26 µs (-98.9%) via a single pre-flight `AscendRange` check — if the seam index is empty, all 74–182 per-coord B+ tree traversals are skipped. When seams exist the ring scan runs as before; cost then scales with seam count, not ring size._

**7. MVCC version resolution — `SelectVisible` scan**

| Operation      | versions | ns/op   | allocs/op |
| -------------- | -------- | ------- | --------- |
| GetCell (MVCC) | 10       | 49,039  | 94        |
| GetCell (MVCC) | 50       | 65,897  | 232       |
| GetCell (MVCC) | 100      | 85,672  | 410       |
| GetCell (MVCC) | 500      | 265,084 | 1,730     |

_Linear growth confirmed: 50× more versions → ~5× latency. Sub-millisecond up to 100 versions. Optimisation only warranted for cells rewritten hundreds of times._

**8. Concurrent read throughput**

| Operation                         | ns/op  | allocs/op |
| --------------------------------- | ------ | --------- |
| View/Update contention (19:1 r/w) | 46,333 | 112       |

_Matches plain `GetCell` latency — readers don't block each other under `sync.RWMutex`._

**9. Integrity scan — `HealthCheck` (all checks enabled)**

| DB size    | ns/op     | allocs/op |
| ---------- | --------- | --------- |
| 512 cells  | 444,772   | 2,510     |
| 2000 cells | 1,628,041 | 9,203     |

_Single forward pass over `cell/`, `seam/`, `tag/`, and `source/` primary/secondary key ranges. All `GetCell` presence checks replaced with O(1) map lookups built during the initial cell scan — complexity O(n) regardless of coordinate sparsity. `ScanRadius` is now a no-op field retained for backward compatibility._

_Hardware: Intel Core i9-14900HX, 16 GB RAM, Go 1.26, Linux (CachyOS). `benchtime=3s -count=1`. Results vary by storage speed and data shape._

---

## Projects using HexxlaDB

- **[Hexxla](https://github.com/hexxla/hexxla)** — spatial LLM memory and reasoning stack

---

## Contributing

See **[CONTRIBUTING.md](CONTRIBUTING.md)**.

```bash
git clone https://github.com/hexxla/hexxladb.git
cd hexxladb
go mod tidy
make test
```

---

## Contact

- [Issues](https://github.com/hexxla/hexxladb/issues) — bugs
- [Discussions](https://github.com/hexxla/hexxladb/discussions) — questions and ideas
