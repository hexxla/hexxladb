# HexxlaDB

**HexxlaDB** is the embedded persistence layer for **[Hexxla](https://github.com/hexxla/hexxla)** — a deterministic **hexagonal spatial memory** stack for LLMs. It turns long-term knowledge into addressable cells on a lattice: you always know **where** a memory lives, **what sits next to it**, **when** it applies, **who** asserted it — and **when two beliefs disagree**, that disagreement becomes a **first-class seam** instead of silent corruption.

HexxlaDB ships as a **production Go library** (`import "github.com/hexxla/hexxladb"`): durable storage, crash recovery, snapshot reads, and primitives aligned with the Hexxla memory model — see **`docs/hexxladb/HEXXLA_DB.md`** (normative layout) and **`docs/hexxladb/HEXXLA.md`** (memory model).

### HexxlaDB in plain language (simple explanation)

Think of memories as **stickers on a honeycomb floor**. Each **cell** is one hex tile. You write facts there with **tags** (“what topic?”) and **provenance** (“who said it?”). When two stickers **disagree**, you don’t hide the clash — you attach a bright **ribbon** called a **seam** so the disagreement stays visible. You can also add **edges** (“this tile talks to that tile”) and **facets** (short extra notes tied to a tile, with a **hash** so the note matches the original sticker).

There are **three different ways** to ask questions — easy to mix up:

1. **By author or role** — “Show every sticker where the back says `session/user`.” That’s **`AscendCellsBySource`**. It walks the whole floor by **who**, not by **where**.
2. **By topic tag** — “Show every sticker tagged `topic/preferences`.” That’s **`AscendCellsByTag`**. Again **global**, not “near the middle.”
3. **By place** — “I stand on **one** hex and pack a **backpack** with what’s nearby.” You pick a **seed coordinate** `(q, r)`. **`LoadContext`** / **`LoadContextPack`** walks **rings** around that seed: center first, then the ring around it, then the next ring, in a **fixed order**. Every tile in range that has a sticker can go into the backpack (**`CellView`** / **`ContextPack`**). If the backpack is **too small** (token or byte **budget**), the engine drops items using **deterministic rules**: prefer to drop from **farther rings** first, and within a ring drop **lower-confidence** items first — so runs are **repeatable** for LLM prompts.

**Seams** in that backpack matter: if a contradiction touches the region you’re loading, it can still show up so the model **sees** the conflict.

**Where does the seed come from?** HexxlaDB does **not** run vector search. **You** (or your app) decide the starting hex — often after **embedding / lexical search / user choice** — then pass that coordinate as **`center`** to **`LoadContextPack`**. So: **search picks the pin on the map**; **HexxlaDB expands spatially from that pin**. “Relevant” here is mainly **near on the lattice** plus **budget rules**, not a second semantic ranking inside **`LoadContextPack`**. Extra filtering or reranking belongs in your **product layer**.

**Other ideas in one line:** **Validity** = when a sticker is “true” in the real world. **`ViewAt` / `ViewAtTime`** = what the **database** knew at a past commit (**MVCC**), separate from validity. **`examples/live_session_demo`** tells a scripted session story under **`./.tmp/`**; **`examples/full_api_demo`** runs the **full exported API** with kid-friendly (**ELI5**) narration and live seeded files under **`./.tmp/full_api_demo/`**.

---

## Why HexxlaDB exists

Large language models need memory that stays **inspectable**, **budgetable**, and **honest**. Ad-hoc notebooks and opaque retrieval dumps fail in predictable ways: irrelevant context crowds the window, locality is fuzzy, conflicting facts disappear or blur together, and updates ripple unpredictably through derived text.

HexxlaDB implements the **storage substrate** for a different contract:

| Pressure                              | What HexxlaDB enables                                                                                                                                                                                                                                         |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Contradictions**                    | **Seams** link pairs of cells when facts clash — detected, typed, annotated, resolvable — so conflict is visible in context assembly instead of erased.                                                                                                       |
| **Best context under a token budget** | Memories sit on a **hex grid**. From any seed coordinate, **`load_context`** expands outward in fixed ring order while helpers can assemble **Hexxla-shaped views** (`CellView`, `ContextPack`) with deterministic eviction rules aligned to product budgets. |
| **Trust & evolution**                 | Every cell carries **validity windows** and **provenance**. **Facets** hold derived summaries tied to derivation hashes against immutable raw anchors. Knowledge can evolve without pretending the past never happened.                                       |
| **Snapshots**                         | **MVCC** pins reads to **`as_of`** commit sequences — reconstruct what the lattice knew at a moment in time while writes move forward cleanly.                                                                                                                |

This is **not** generic tabular storage bolted onto vectors. Geometry, graph-like links, time, and contradiction handling are modeled **on purpose** — so your orchestration layer can stay thin and deterministic.

---

## How it works (concept → mechanism)

### Spatial memory cells

Facts live at **coordinates** `(q, r)` on an axial hex lattice, packed into Morton order for fast range scans. **Ring walks** and **`LoadContext`** traverse neighbors in documented order — same seed, same expansion: ideal for repeatable LLM prompts and tests.

### Contradictions as seams

When two stored cells disagree meaningfully, your flow records a **seam** — an edge in the contradiction graph with type, confidence delta, timestamps, and resolution fields. Contradictions stay **addressable artifacts**, not collisions in an opaque blob store.

### Derived views without losing the anchor

**Facets** store derived text with strict derivation hashes tied to raw cell bytes. **Edges** carry directed relationships with weights and validity. You can refresh summaries without abandoning the immutable raw record that proves what the model actually stored.

### Temporal truth

**Validity windows** on cells and seams bound when a statement applies in the real world. Snapshots (**`ViewAt`**, **`ViewAtTime`**) bound what the **system** knew at a commit — separate concerns, both queryable.

### Discovery along provenance and tags

Secondary indexes support walking cells by **source**, **time bucket**, and **tag** — useful when the seed for context assembly comes from provenance or taxonomy rather than coordinates alone.

---

## What you build on top

HexxlaDB gives **primitives**; **seed selection** (how you pick the first coordinate), **ranking beyond ring order**, and **HTTP/JSON services** live in your application — see **[`docs/hexxladb/HEXXLA.md`](docs/hexxladb/HEXXLA.md)** for the memory-model view and **[`docs/context/HEXAGONAL_ARCHITECTURE.md`](docs/context/HEXAGONAL_ARCHITECTURE.md)** for boundaries.

---

## Capabilities at a glance

- **Cells, facets, edges, seams** — distinct record families with explicit roles.
- **Bolt-style transactions** — `View` / `Update` / `Batch` with `*Tx`.
- **MVCC** — format v2 optional at create time; retention and pruning hooks for long-lived deployments.
- **Logical changefeed** — optional append-only sidecar for downstream consumers.
- **At-rest encryption** — AES-256-XTS at the page boundary when configured.
- **Hexagonal lattice helpers** — exported coordinates, packing, rings — **`internal/lattice`** details in-repo.

---

## Public API inventory (`package hexxladb`)

**Full exported symbol checklist (single source for coverage audits):** **[`docs/hexxladb/API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md)** — tables by subsystem, sentinel errors, **`examples/full_api_demo`** vs **`examples/live_session_demo`** coverage.

Overview and deep links: **[`doc.go`](doc.go)** (package comment), **`docs/hexxladb/TX.md`**, **`docs/hexxladb/HEXXLA_DB.md`**.

| Area                    | Exported symbols                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Open / lifecycle**    | [`Open`](doc.go), [`(*DB).Close`](db.go), [`Options`](options.go), [`MVCCRetention`](options.go), [`RotateEncryption`](rotation.go), [`RotateEncryptionWithOptions`](rotation.go), [`RotateOptions`](rotation.go), [`DeriveKeyFromPassphrase`](encryption.go)                                                                                                                                                                                                                                                                                      |
| **Transactions**        | [`(*DB).View`](tx.go), [`(*DB).Update`](tx.go), [`(*DB).Batch`](tx.go) (= Update), [`(*DB).ViewAt`](tx.go), [`(*DB).ViewAtTime`](tx.go), [`(*Tx).Writable`](tx.go)                                                                                                                                                                                                                                                                                                                                                                                 |
| **Byte-key store**      | [`(*Tx).Get`](tx.go), [`(*Tx).Put`](tx.go), [`(*Tx).AscendRange`](tx.go)                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| **Cells & walks**       | [`(*Tx).PutCell`](primitives.go), [`(*Tx).GetCell`](primitives.go), [`(*Tx).WalkRing`](primitives.go), [`(*Tx).WalkRingAt`](primitives.go), [`(*Tx).LoadContext`](primitives.go), [`(*Tx).LoadContextAt`](primitives.go), [`(*Tx).WalkRingFacets`](primitives.go)                                                                                                                                                                                                                                                                                  |
| **Cell indexes**        | [`(*Tx).AscendCellsBySource`](cell_secondary.go), [`(*Tx).AscendCellsInTimeBucket`](cell_secondary.go), [`(*Tx).AscendCellsByTag`](cell_secondary.go), [`(*Tx).AscendDistinctTags`](cell_secondary.go), [`(*Tx).ListExistingTopics`](cell_secondary.go)                                                                                                                                                                                                                                                                                             |
| **Seams**               | [`(*Tx).PutSeam`](primitives.go), [`(*Tx).FindSeams`](primitives.go), [`(*Tx).FindSeamsAt`](primitives.go), [`(*Tx).ResolveSeam`](primitives.go), [`(*Tx).MarkConflict`](primitives.go), [`(*Tx).AscendSeamsBySource`](seam_secondary.go), [`(*Tx).AscendSeamsInTimeBucket`](seam_secondary.go)                                                                                                                                                                                                                                                    |
| **Facets & edges**      | [`(*Tx).PutFacet`](facets_edges.go), [`(*Tx).GetFacet`](facets_edges.go), [`(*Tx).UpdateFacet`](facets_edges.go), [`(*Tx).AscendFacetsForCell`](facets_edges.go), [`(*Tx).PutEdge`](facets_edges.go), [`(*Tx).GetEdge`](facets_edges.go), [`(*Tx).AscendEdgesFrom`](facets_edges.go), [`(*Tx).LinkCells`](facets_edges.go)                                                                                                                                                                                                                         |
| **HEXXLA-shaped views** | [`(*Tx).AssembleCellView`](views.go), [`(*Tx).LoadContextWithBudgeting`](views.go), [`(*Tx).LoadContextPack`](views.go), [`CellView`](views.go), [`ContextPack`](views.go), [`FacetView`](views.go), [`EdgeView`](views.go), [`SeamRef`](views.go), [`TokenBudgeter`](views.go), [`ByteLenBudgeter`](views.go), [`AssembleCellViewOpts`](views.go), [`DefaultAssembleCellViewOpts`](views.go), [`LoadContextBudgetConfig`](views.go), [`CellViewPredicate`](views.go), [`FilterCellViews`](views.go), [`TruncateCellViewsToTokenBudget`](views.go) |
| **Lattice**             | [`Coord`](coord_export.go), [`Cube`](coord_export.go), [`PackedCoord`](coord_export.go), [`Pack`](coord_export.go), [`Unpack`](coord_export.go), [`Ring`](coord_export.go), [`WalkRings`](coord_export.go), [`MaxAxialAbs`](coord_export.go), [`ErrCoordOutOfRange`](coord_export.go); methods on **`Coord`** / **`PackedCoord`** (e.g. **`Distance`**, **`Neighbors`**) — [`internal/lattice`](internal/lattice)                                                                                                                                  |
| **MVCC & prune**        | [`(*DB).StatsMVCC`](mvcc_lifecycle.go), [`(*DB).SuggestedPruneBeforeSeq`](mvcc_lifecycle.go), [`(*DB).MVCCPrunePlan`](mvcc_lifecycle.go), [`(*DB).PruneCellVersions`](mvcc_lifecycle.go), [`(*DB).PruneCellVersionsByProfile`](mvcc_lifecycle.go), [`MVCCStats`](mvcc_lifecycle.go), [`MVCCPruneProfile`](mvcc_lifecycle.go), [`PruneScheduler`](mvcc_lifecycle.go)                                                                                                                                                                                |
| **Changelog**           | [`(*DB).ReadChangelogSince`](db_changelog.go), [`ChangelogRecord`](db_changelog.go); op codes [`ChangelogOpPutCell`](db_changelog.go), [`ChangelogOpPutSeam`](db_changelog.go), [`ChangelogOpResolveSeam`](db_changelog.go), [`ChangelogOpPutFacet`](db_changelog.go), [`ChangelogOpPutEdge`](db_changelog.go)                                                                                                                                                                                                                                     |
| **Sentinel errors**     | [`db.go`](db.go) (`ErrCorruptDatabase`), [`errors.go`](errors.go) (all others in package comment), [`changelog`](internal/changelog) (`ErrChangelogCorrupt` alias)                                                                                                                                                                                                                                                                                                                                                                                 |

Validity helpers used by read paths live in **`internal/record`** ([`record.ValidAt`](internal/record/validity.go)) — same module only.

---

## Annotated API tour (full surface)

Examples import **`internal/lattice`** and **`internal/record`** because wire shapes and geometry types are defined there; **`internal/...`** is **only importable from code inside `github.com/hexxla/hexxladb`** (this repo, forks, subpackages). External binaries typically map their domain types at the adapter and still call **`hexxladb.Open`** / **`Tx`** only from the stable root package — see **`docs/context/HEXAGONAL_ARCHITECTURE.md`**.

Start from **Options, open…**; later snippets assume the same **`db`** (and reuse **`center`** / **`pk`** where shown). Merge into one `main` with a single `import` block (`context`, `time`, `hexxladb`, `lattice`, `record`, and for seams `crypto/rand`, `github.com/oklog/ulid/v2`).

### Options, open, transactions, raw KV

```go
import (
    "context"
    "time"

    "github.com/hexxla/hexxladb"
)

opts := &hexxladb.Options{
    EnableMVCC:       true,
    MVCCRetention:    hexxladb.MVCCRetention{RetainCommitsBehindHead: 500},
    ChangelogEnabled: true,
    // EncryptionKey / Passphrase: see docs/hexxladb/ENCRYPTION.md
}
db, err := hexxladb.Open("data.db", opts)
if err != nil {
    panic(err)
}
defer db.Close()

ctx := context.Background()

// Snapshots (format v2 MVCC)
_ = db.ViewAt(7, func(tx *hexxladb.Tx) error { return nil })
_ = db.ViewAtTime(time.Now(), func(tx *hexxladb.Tx) error { return nil })

// Bolt-style tx + low-level ordered store (prefer PutCell etc. for domain data)
_ = db.Update(func(tx *hexxladb.Tx) error {
    _ = tx.Writable()
    _, _, _ = tx.Get(nil)
    _ = tx.Put([]byte("meta/example"), []byte("v"))
    _ = tx.AscendRange(nil, nil, func(_, _ []byte) bool { return true })
    return nil
})
```

### Cells, rings, context, secondary scans

```go
import (
    "context"
    "time"

    "github.com/hexxla/hexxladb"
    "github.com/hexxla/hexxladb/internal/lattice"
    "github.com/hexxla/hexxladb/internal/record"
)

center := lattice.Coord{Q: 0, R: 0}
pk, _ := lattice.Pack(center)

rec := record.CellRecord{
    Key:         pk,
    RawContent:  "hello",
    Provenance:  record.ProvenanceWire{SourceID: "src", Confidence: 1, CreatedAt: 1, UpdatedAt: 1},
    Validity:    record.ValidityWire{},
    Tags:        []string{"demo"},
}

_ = db.Update(func(tx *hexxladb.Tx) error {
    _ = tx.PutCell(ctx, rec)
    return nil
})

_ = db.View(func(tx *hexxladb.Tx) error {
    _, _, _ = tx.GetCell(pk)
    _, _ = tx.LoadContext(ctx, center, 2, 50)
    _, _ = tx.LoadContextAt(ctx, center, 2, 50, time.Now())

    _ = tx.WalkRing(ctx, center, 1, func(_ lattice.Coord, _ []byte, _ bool) bool { return true })
    _ = tx.WalkRingAt(ctx, center, 1, time.Now(), func(_ lattice.Coord, _ record.CellRecord) bool { return true })

    _ = tx.WalkRingFacets(ctx, center, 1, 0x3f, nil, func(_ lattice.Coord, _ record.CellRecord, _ []record.FacetRecord) bool {
        return true
    })

    _ = tx.AscendCellsBySource(ctx, "src", func(record.CellRecord) bool { return true })
    _ = tx.AscendCellsInTimeBucket(ctx, 0, func(record.CellRecord) bool { return true })
    _ = tx.AscendCellsByTag(ctx, "demo", func(record.CellRecord) bool { return true })
    return nil
})

// Geometry helpers (no DB)
ring := hexxladb.Ring(center, 2)
buf := hexxladb.WalkRings(nil, center, 2)
_, _ = buf, ring
```

### Facets, edges, LinkCells

```go
import (
    "time"

    "github.com/hexxla/hexxladb"
    "github.com/hexxla/hexxladb/internal/lattice"
    "github.com/hexxla/hexxladb/internal/record"
)

pk, _ := lattice.Pack(lattice.Coord{Q: 0, R: 0})
raw := []byte("hello")
hash := record.HashRawContent(raw)
facet := record.FacetRecord{
    Key: pk, FacetID: 0, DerivedContent: "summary",
    LastRotated: time.Now().UnixNano(), DerivationHash: hash,
}
center := lattice.Coord{Q: 0, R: 0}

_ = db.Update(func(tx *hexxladb.Tx) error {
    _ = tx.PutFacet(facet)
    _, _, _ = tx.GetFacet(pk, 0)
    _ = tx.AscendFacetsForCell(pk, func(record.FacetRecord) bool { return true })

    pc1, _ := lattice.Pack(lattice.Coord{Q: 1, R: 0})
    _ = tx.PutEdge(record.EdgeRecord{
        From: pc1, To: pk, RelationType: "refs", Weight: 1,
        Provenance: record.ProvenanceWire{SourceID: "src", Confidence: 1, CreatedAt: 1, UpdatedAt: 1},
    })
    _, _, _ = tx.GetEdge(pc1, pk, "refs")
    _ = tx.AscendEdgesFrom(pc1, func(record.EdgeRecord) bool { return true })

    _ = tx.LinkCells(lattice.Coord{Q: 1, R: 0}, center, "refs", 1,
        record.ProvenanceWire{SourceID: "src", Confidence: 1, CreatedAt: 1, UpdatedAt: 1})

    facet.DerivedContent = "updated"
    _ = tx.UpdateFacet(facet)
    return nil
})
```

### Seams

```go
import (
    "context"
    "crypto/rand"
    "time"

    "github.com/hexxla/hexxladb"
    "github.com/hexxla/hexxladb/internal/lattice"
    "github.com/hexxla/hexxladb/internal/record"
    "github.com/oklog/ulid/v2"
)

center := lattice.Coord{Q: 0, R: 0}
ctx := context.Background()

sid := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
pa, _ := lattice.Pack(center)
pb, _ := lattice.Pack(lattice.Coord{Q: 1, R: 0})

seam := record.SeamRecord{
    ID: sid, CellA: pa, CellB: pb, SeamType: "contradiction",
    Reason: "demo", ConfidenceDelta: 0.1, DetectedAt: time.Now().UnixNano(),
}

_ = db.Update(func(tx *hexxladb.Tx) error {
    _ = tx.PutSeam(ctx, seam)
    return nil
})

_ = db.View(func(tx *hexxladb.Tx) error {
    _, _ = tx.FindSeams(ctx, center, 3, false)
    _, _ = tx.FindSeamsAt(ctx, center, 3, false, time.Now())
    _ = tx.AscendSeamsBySource(ctx, "", func(record.SeamRecord) bool { return true })
    _ = tx.AscendSeamsInTimeBucket(ctx, 0, func(record.SeamRecord) bool { return true })
    return nil
})

_ = db.Update(func(tx *hexxladb.Tx) error {
    _ = tx.ResolveSeam(sid, "merged", "done")
    _ = tx.MarkConflict(center, lattice.Coord{Q: 2, R: 0}, "manual")
    return nil
})
```

### HEXXLA-shaped views & token budgeting

```go
import (
    "context"

    "github.com/hexxla/hexxladb"
    "github.com/hexxla/hexxladb/internal/lattice"
)

center := lattice.Coord{Q: 0, R: 0}
ctx := context.Background()
hc := hexxladb.Coord{Q: center.Q, R: center.R}
opts := hexxladb.DefaultAssembleCellViewOpts()
cfg := hexxladb.LoadContextBudgetConfig{
    Assemble:          opts,
    MaxCandidateCells: 64,
    IncludeFacetText:  true,
    IncludeSeams:      false,
    SeamRadius:        2,
}

_ = db.View(func(tx *hexxladb.Tx) error {
    v, _ := tx.AssembleCellView(ctx, hc, nil, opts)
    _ = v

    pack, _ := tx.LoadContextWithBudgeting(ctx, hc, 2, 8000, hexxladb.ByteLenBudgeter{}, cfg)
    pack2, _ := tx.LoadContextPack(ctx, hc, 2, 8000, hexxladb.ByteLenBudgeter{}, cfg)
    _ = pack2

    filtered := hexxladb.FilterCellViews(pack.Cells, func(cv hexxladb.CellView) bool { return true })
    trimmed, _ := hexxladb.TruncateCellViewsToTokenBudget(filtered, hexxladb.ByteLenBudgeter{}, 4000, true)
    _ = trimmed
    return nil
})
```

### MVCC stats, prune plan, changelog

```go
import "github.com/hexxla/hexxladb"

if stats, err := db.StatsMVCC(); err == nil {
    _ = stats.CommitSeq
}

before, ok, _ := db.SuggestedPruneBeforeSeq()
if ok {
    _, _, _, _ = db.MVCCPrunePlan(hexxladb.MVCCPruneBalanced)
    _, _ = db.PruneCellVersions(before, 512)
    _, _ = db.PruneCellVersionsByProfile(before, hexxladb.MVCCPruneLowLatency)

    var sched hexxladb.PruneScheduler
    _, _ = sched.Tick(db)
}

_, _ = db.ReadChangelogSince(0, 100)
```

### Offline encryption rotation (separate maintenance step)

```go
// RotateEncryption("path.db", oldOpts, newOpts) — see docs/hexxladb/ENCRYPTION.md
```

---

## Scaling characteristics (order of growth)

| Operation                        | Behavior                                                              |
| -------------------------------- | --------------------------------------------------------------------- |
| Point read / write of a cell     | Logarithmic in index size                                             |
| Ring walk / bounded context load | Range scans over Morton-ordered keys                                  |
| MVCC snapshot read               | Pinned `read_seq`; versions visible per design                        |
| Prune stale versions             | Batched, operator-driven (`StatsMVCC`, `PruneCellVersions`, profiles) |

---

## Production readiness

- **Operations, retention & drills:** [`docs/hexxladb/OPERATIONS.md`](docs/hexxladb/OPERATIONS.md)
- **Roadmap & non-goals:** [`docs/ROADMAP.md`](docs/ROADMAP.md)
- **Verification:** `make ci` (default); `make integration` (includes MVCC churn + prune integration test)

---

## Getting started

```bash
go get github.com/hexxla/hexxladb
```

Minimal program (same import pattern as **`cmd/hexxladb`** and **`examples/storage_walkthrough`** inside this module):

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
    db, err := hexxladb.Open("hello.db", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    center := lattice.Coord{Q: 0, R: 0}
    pk, err := lattice.Pack(center)
    if err != nil {
        log.Fatal(err)
    }
    ctx := context.Background()
    rec := record.CellRecord{
        Key:        pk,
        RawContent: "An immutable anchor for downstream facets and seams.",
        Provenance: record.ProvenanceWire{
            SourceID:   "demo",
            Confidence: 1,
            CreatedAt:  1,
            UpdatedAt:  1,
        },
    }
    if err := db.Update(func(tx *hexxladb.Tx) error {
        return tx.PutCell(ctx, rec)
    }); err != nil {
        log.Fatal(err)
    }

    if err := db.View(func(tx *hexxladb.Tx) error {
        cells, err := tx.LoadContext(ctx, center, 2, 50)
    if err != nil {
            return err
        }
        log.Printf("neighborhood cells: %d", len(cells))
        return nil
    }); err != nil {
        log.Fatal(err)
    }
}
```

End-to-end adapter walkthrough: **`go run ./examples/storage_walkthrough -path .tmp/walk.db`** (from a clone of this repository).

Full **`package hexxladb`** tour (ELI5 narration, MVCC + changelog + optional encryption): **`go run ./examples/full_api_demo`** — output explains each step in simple language; see **[`docs/hexxladb/API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md)** for the symbol checklist.

---

## Documentation map

| Doc                                                                                | Purpose                                              |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------- |
| [`docs/hexxladb/HEXXLA.md`](docs/hexxladb/HEXXLA.md)                               | Memory model, geometry, retrieval story                    |
| [`docs/hexxladb/HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md)                         | Keys, indexes, native query primitives (how it works)      |
| [`docs/hexxladb/API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md)                 | Full exported symbol inventory                             |
| [`docs/hexxladb/TX.md`](docs/hexxladb/TX.md)                                       | Transactions, MVCC snapshot + temporal semantics           |
| [`docs/hexxladb/OPERATIONS.md`](docs/hexxladb/OPERATIONS.md)                       | Backups, retention, incident response, soak checklist      |
| [`docs/hexxladb/DURABILITY.md`](docs/hexxladb/DURABILITY.md)                       | WAL, group commit, durability barriers                     |
| [`docs/hexxladb/ENCRYPTION.md`](docs/hexxladb/ENCRYPTION.md)                       | Optional at-rest encryption                                |
| [`docs/hexxladb/CHANGEFEED.md`](docs/hexxladb/CHANGEFEED.md)                       | Optional logical changelog                                 |
| [`docs/context/HEXAGONAL_ARCHITECTURE.md`](docs/context/HEXAGONAL_ARCHITECTURE.md) | Ports, adapters — how services embed this library          |
| [`docs/ROADMAP.md`](docs/ROADMAP.md)                                               | Out-of-scope boundaries, backlog, near-term candidates     |

---

## Projects using HexxlaDB

- **[Hexxla](https://github.com/hexxla/hexxla)** — spatial LLM memory and reasoning stack

Add yours via PR if you ship something public.

---

## Contributing

See **[CONTRIBUTING.md](CONTRIBUTING.md)**.

```bash
git clone https://github.com/hexxla/hexxladb.git
cd hexxladb
go mod tidy
make test
```

**Quality gates:** **`make`** or **`make ci`** (same steps as CI: format, **`go vet`**, tests with **`-race`**, **`govulncheck`**, **`golangci-lint`**, **`go mod tidy`**). Optional **[pre-commit](https://pre-commit.com)** hooks: **`make pre-commit-install`**.

---

## License

[Apache License 2.0](LICENSE)

---

## Contact

- [Issues](https://github.com/hexxla/hexxladb/issues) — bugs
- [Discussions](https://github.com/hexxla/hexxladb/discussions) — questions and ideas
- [@hexxla](https://twitter.com/hexxla) — updates
