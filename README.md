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

**2. Point read latency — `GetCell`**

| Operation                  | DB size    | ns/op     | allocs/op |
| -------------------------- | ---------- | --------- | --------- |
| GetCell                    | 512 cells  | 52,023    | 88        |
| GetCell                    | 2000 cells | 55,443    | 111       |
| GetCell (encrypted)        | 512 cells  | 931,062   | 88        |
| GetCell (encrypted)        | 2000 cells | 964,937   | 111       |
| GetCell (MVCC + encrypted) | 512 cells  | 1,383,048 | 87        |
| GetCell (MVCC + encrypted) | 2000 cells | 1,292,176 | 122       |

_Plain reads are stable across DB sizes — confirming O(log n) B+ tree traversal. Encryption adds ~18× overhead (AES-GCM page decryption)._

**3. Context assembly — LLM hot path**

| Operation              | DB size    | radius | ns/op     | allocs/op |
| ---------------------- | ---------- | ------ | --------- | --------- |
| LoadContext            | 512 cells  | 3      | 793,257   | 2,944     |
| LoadContext            | 2000 cells | 3      | 709,624   | 3,804     |
| LoadContextPack (4 KB) | 512 cells  | 1      | 381,769   | 839       |
| LoadContextPack (4 KB) | 512 cells  | 3      | 1,510,259 | 3,756     |
| LoadContextPack (4 KB) | 512 cells  | 5      | 3,342,487 | 8,599     |
| LoadContextPack (4 KB) | 2000 cells | 1      | 576,673   | 1,084     |
| LoadContextPack (4 KB) | 2000 cells | 3      | 2,083,620 | 4,983     |
| LoadContextPack (4 KB) | 2000 cells | 5      | 4,622,585 | 11,756    |

_Cost scales with ring area (r=1: 7 cells, r=3: 37 cells, r=5: 91 cells), not DB size — spatial locality from Morton-ordered keys confirmed._

**4. Spatial ring walk — Morton-order prefix scan**

| Operation | DB size    | ring | ns/op   | allocs/op |
| --------- | ---------- | ---- | ------- | --------- |
| WalkRing  | 512 cells  | 2    | 279,450 | 927       |
| WalkRing  | 2000 cells | 2    | 308,020 | 1,203     |

_Near-constant across DB sizes — range scan on Morton-packed keys, not a graph traversal._

**5. Query engine — `QueryCells` predicate shapes**

| Predicate              | DB size    | ns/op      | allocs/op |
| ---------------------- | ---------- | ---------- | --------- |
| Tag filter (miss)      | 512 cells  | 66,450     | 82        |
| Tag filter (miss)      | 2000 cells | 105,706    | 103       |
| Source index scan      | 512 cells  | 10,331,594 | 50,851    |
| Source index scan      | 2000 cells | 53,730,561 | 210,350   |
| Spatial radius (r=3)   | 512 cells  | 759,366    | 2,948     |
| Spatial radius (r=3)   | 2000 cells | 944,028    | 3,808     |
| Combined (src+spatial) | 512 cells  | 14,244,274 | 50,845    |

_Tag miss exits early via index. Source scan cost is linear in matching rows. Spatial radius is bounded by ring area regardless of DB size._

**6. Seam resolution — `FindSeams`**

| Operation | seams in radius 3 | ns/op     | allocs/op |
| --------- | ----------------- | --------- | --------- |
| FindSeams | 0                 | 2,275,497 | 302       |
| FindSeams | 10                | 4,789,759 | 4,280     |
| FindSeams | 50                | 5,289,436 | 5,075     |
| FindSeams | 100               | 5,273,720 | 6,269     |

_Base cost (~2.3 ms) is the spatial ring scan. Seam lookup plateaus quickly — secondary index scan, not a full table walk._

**7. MVCC version resolution — `SelectVisible` scan**

| Operation      | versions | ns/op   | allocs/op |
| -------------- | -------- | ------- | --------- |
| GetCell (MVCC) | 10       | 107,550 | 94        |
| GetCell (MVCC) | 50       | 131,066 | 232       |
| GetCell (MVCC) | 100      | 179,935 | 410       |
| GetCell (MVCC) | 500      | 567,836 | 1,730     |

_Linear growth confirmed: 50× more versions → ~5× latency. Sub-millisecond up to 100 versions. Optimisation only warranted for cells rewritten hundreds of times._

**8. Concurrent read throughput**

| Operation                         | ns/op  | allocs/op |
| --------------------------------- | ------ | --------- |
| View/Update contention (19:1 r/w) | 51,956 | 112       |

_Matches plain `GetCell` latency — readers don't block each other under `sync.RWMutex`._

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
