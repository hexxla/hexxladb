# Service integration — best practices

**Audience:** Engineers implementing **applications** that embed **`package hexxladb`** (`github.com/hexxla/hexxladb`) — retrieval orchestration, write paths, and conventions that live **above** the storage engine.

**Normative keys and disk behavior:** [**HEXXLA_DB.md**](./HEXXLA_DB.md). **Concept ↔ API mapping:** [**HEXXLA_LIBRARY_MAPPING.md**](./HEXXLA_LIBRARY_MAPPING.md). **Full symbol list:** [**API_REFERENCE.md**](./API_REFERENCE.md).

This document is **guidance**, not a spec: it does not change what the engine guarantees.

---

## 1. Separate concerns: engine vs your service

| Responsibility | Lives in HexxlaDB | Lives in your service |
| --- | --- | --- |
| Ring order, **`LoadContext`** / **`LoadContextPack`** eviction rules | Yes | No |
| Choosing **`center`** (seed coord), merging multi-seed packs, reranking | No | Yes |
| **`AscendCellsByTag`** / **`AscendCellsBySource`** scans | Yes | No |
| Tag namespaces, prompt policy, when the LLM writes vs summarizes | No | Yes |
| Embedding / lexical / hybrid search (**optional**) | No (no built-in ANN in v1) | Yes |

Ship a thin **composition layer**: pick seeds → call **`Tx`** primitives → optionally **`FilterCellViews`** / **`TruncateCellViewsToTokenBudget`** ([**views.go**](../../views.go)). Wiring patterns: [**HEXXLA_PRODUCT_WIRING.md**](./HEXXLA_PRODUCT_WIRING.md).

---

## 2. Retrieval: combine indexes with geometry

**Ring loading alone** only sees cells within **`maxR`** of **`center`**. To surface facts that are **far on the lattice** or **written long ago**, plan explicit steps:

1. **Discover** candidates via **`AscendCellsByTag`**, **`AscendCellsBySource`**, **`AscendCellsInTimeBucket`**, or **`AscendEdgesFrom`** (after following an edge from a known cell).
2. **Unpack** coordinates from **`record.CellRecord`** and treat them as **additional seeds** (or hop targets).
3. **Assemble** context with **`LoadContext`**, **`LoadContextPack`**, or **`AssembleCellView`** per seed, then **dedupe and merge** in your layer (there is no single “union pack” API).

Reference composition: [**examples/live_session_demo**](../../examples/live_session_demo/) (session story) and [**examples/full_api_demo**](../../examples/full_api_demo/) (exhaustive API + ELI5).

---

## 3. Tags

- **No curated topic list in the database.** Your service chooses whatever strings it puts on **`record.CellRecord.Tags`**; **`PutCell`** mirrors them into the **`tag/`** btree (see [**HEXXLA_DB.md**](./HEXXLA_DB.md)). The only hard limit is **UTF-8 length** per tag (**≤ 220 bytes** per secondary-key encoding in **`internal/index/tag_key.go`**); overly long tags return **`ErrInvalidArgument`** from **`AscendCellsByTag`** / **`PutCell`** secondary writes.
- **Enumerate topics (tags):** **`Tx.ListExistingTopics`** returns sorted distinct tag strings visible at the current **`View`** / **`ViewAt`** snapshot (cheap payload for an LLM tool — tag strings only). It walks **`tag/`** secondary keys and confirms each row against **`Tx.GetCell`** so MVCC snapshots and stale secondaries stay correct. **`Tx.AscendDistinctTags`** streams the same distinct set with a callback. Cost is still **O(number of physical tag index rows)** plus **`GetCell`** once per distinct `(tag, PackedCoord)` pair before deduplication.
- **Cells for one topic:** **`Tx.AscendCellsByTag`** scans **one known tag string** and invokes your callback with each **visible** cell that lists that tag (same MVCC semantics as other ascends).
- **Use stable, namespaced strings** (e.g. `topic/…`, `policy/…`, `memory/session`) so scans stay predictable and documentation stays short.
- **Prefer a small, reviewed taxonomy** over unbounded free-form tags from the model; if the model invents tags, map or normalize in your adapter before **`PutCell`**.
- **Tags are for “find all X”** across the lattice; they do not encode pairwise relationships — use **edges** for that.
- **`PutCell`** mirrors tags into the **`tag/`** secondary index; changing tags on update replaces index entries — plan migrations if you rename tags in production.

---

## 4. Source and time

- Set **`ProvenanceWire.SourceID`** (and related fields) consistently — **`AscendCellsBySource`** is your “by role / session / upstream system” sweep.
- Use **`AscendCellsInTimeBucket`** when your product thinks in **calendar weeks** (UTC bucket index on disk); pair with **validity** on records when “truth window” differs from write time (**`LoadContextAt`**, **`WalkRingAt`**).

---

## 5. Edges vs seams

- **`LinkCells`** / **`PutEdge`** express **directed relationships between two coordinates** regardless of hex distance — ideal for conversation threads, citations, or “this summary anchored to that raw cell.”
- **`PutSeam`** records **contradiction or tension** between cells; surface them in **`FindSeams`** / view assembly when you want the model to **see** conflict (**`LoadContextBudgetConfig.IncludeSeams`**).

---

## 6. Writing from an LLM (content hygiene)

These patterns improve **retrieval quality** and **budget behavior** without changing the engine:

- **Keep raw anchors faithful** when the fact must be auditable; put **concise paraphrases** in **facets** with correct **derivation hashes** when you use **`UpdateFacet`** discipline ([**HEXXLA_DB.md**](./HEXXLA_DB.md) facets section).
- **Avoid enormous blobs** in **`RawContent`** when you rely on ring walks — large cells still load as units; prefer splitting stable facts across cells with shared tags or edges.
- **Set confidence intentionally** — **`LoadContextWithBudgeting`** drops **lower `Provenance.Confidence`** in the **outermost ring first**; systematic mis-calibration skews what survives under token caps.

---

## 7. MVCC, retention, operations

Bad integration loses **old** versions even when APIs could query them:

- Configure **`Options.MVCCRetention`** and run [**prune**](../../mvcc_lifecycle.go) on a schedule — see [**ADOPTION.md**](./ADOPTION.md), [**MVCC_RETENTION.md**](./MVCC_RETENTION.md), [**MVCC_TEMPORAL.md**](./MVCC_TEMPORAL.md).
- Distinguish **validity time** on records from **snapshot time** (**`ViewAt`** / **`ViewAtTime`**) — both matter for “what did we believe when?” scenarios.

---

## 8. Testing and observability

- **Golden tests** on **`LoadContext`** / **`LoadContextPack`** output order and counts for fixed seeds (deterministic prompts).
- **Integration tests** for **`AscendCellsByTag`** / **`AscendCellsBySource`** mirroring how your gateway builds dashboards.
- Instrument **your** embedding or search layer; HexxlaDB exposes changefeed hooks — see [**CHANGEFEED.md**](./CHANGEFEED.md) if you replicate writes downstream.

---

## Related

- [**ADOPTION.md**](./ADOPTION.md) — Production checklist.
- [**HEXXLA.md**](./HEXXLA.md) — Product memory model (normative for Hexxla semantics).
- [**OPERATIONS.md**](./OPERATIONS.md) — Operator-facing operations.
