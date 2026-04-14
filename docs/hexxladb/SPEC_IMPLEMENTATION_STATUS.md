# HEXXLA_DB.md vs this repository (v1 track)

**Normative spec:** [`HEXXLA_DB.md`](./HEXXLA_DB.md). This note summarizes **what the embedded engine ships today** versus **longer-horizon** spec items, so “done” is not confused with “every section of the vision doc.”

## Aligned and shipped (core)

- **Custom embedded engine** — no SQLite / third-party ordered-KV core; Morton `PackedCoord` keys, B+ tree, WAL, replay on open ([`internal/engine/ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md)).
- **Core objects** — Cell / Facet / Edge / Seam record families ([`internal/record`](../../internal/record)); **Facet/Edge** btree keys and [`Tx`](../../facets_edges.go) CRUD + scans ([`internal/index/facet_key.go`](../../internal/index/facet_key.go), [`edge_key.go`](../../internal/index/edge_key.go)); seam ULID + `seam-by-cells` secondary ([`internal/index`](../../internal/index)).
- **Native query primitives** on the public API — `PutCell`, `WalkRing`, `FindSeams`, `LoadContext`, `PutSeam`, `ResolveSeam`, **`MarkConflict`**, **`PutFacet` / `GetFacet` / `AscendFacetsForCell` / `UpdateFacet`**, **`PutEdge` / `GetEdge` / `AscendEdgesFrom` / `LinkCells`**, **`AscendCellsBySource` / `AscendCellsInTimeBucket`** ([`primitives.go`](../../primitives.go), [`facets_edges.go`](../../facets_edges.go), [`cell_secondary.go`](../../cell_secondary.go)); see [`TX.md`](./TX.md).
- **Optional at-rest encryption** — [`ENCRYPTION.md`](./ENCRYPTION.md).
- **Durability testing** — [`db_durability_test.go`](../../db_durability_test.go); optional heavier runs: `make integration`.

## Spec items intentionally deferred or partial

| Topic | Spec intent | Current status |
|-------|-------------|----------------|
| **MVCC / `as_of`** | Snapshot isolation, temporal queries | **Not implemented** in engine — single writer, `View` sees last committed state ([`TX.md`](./TX.md)). **Design:** [`MVCC_DESIGN.md`](./MVCC_DESIGN.md) (Phase E0). **E1:** Option A suffix-key experiment only ([`internal/mvccspike`](../../internal/mvccspike)); production MVCC deferred to E2+ per gap plan. |
| **Validity `as_of` read filters / `walk_ring` facet_mask** | Phase C in gap plan | **Shipped** — [`record.ValidAt`](../../internal/record/validity.go), [`Tx.WalkRingAt`](../../primitives.go), [`Tx.LoadContextAt`](../../primitives.go), [`Tx.WalkRingFacets`](../../primitives.go); single-version only ([`TX.md`](./TX.md)). |
| **`time/` / `source/` indexes** | Layout in Storage Layout | **Shipped** for **cells** — [`internal/index/source_key.go`](../../internal/index/source_key.go), [`time_key.go`](../../internal/index/time_key.go); maintained by [`Tx.PutCell`](../../primitives.go); scans [`Tx.AscendCellsBySource`](../../cell_secondary.go), [`Tx.AscendCellsInTimeBucket`](../../cell_secondary.go) ([`TX.md`](./TX.md)). |
| **`embed/` hybrid** | Optional vector refs | **Out of v1** per spec non-goals; key reserved for future. |
| **`Batch` as primary API** | Spec illustration | **`DB.Batch`** is exposed as an **alias** for **`DB.Update`** (Phase F); **`View`/`Update`** remain primary names ([checklist](../checklist/HEXXLA_DB_V1.md), [`TX.md`](./TX.md)). |
| **Edge volume APIs** | `link_cells` / high-volume edges | **Shipped** — [`LinkCells`](../../facets_edges.go) + low-level [`PutEdge`](../../facets_edges.go); stress/scale still product-dependent. |
| **Provenance changefeed / materialized views** | Scalability section | **Future** — not in M3–M10 roadmap scope. |
| **Locality “superiority” of Morton** | Spec asks validation via benchmarks | **Microbenchmarks** in [`BENCHMARKS.md`](./BENCHMARKS.md); full workload proof is separate. |

## Composition root

When **`HEXXLA_DB_PATH`** is set, [`cmd/hexxladb`](../../cmd/hexxladb/main.go) opens the DB, constructs the [`internal/adapters/out/hexxladb`](../../internal/adapters/out/hexxladb) storage adapter, and injects it into [`internal/app`](../../internal/app) — see hexagonal rules in [`HEXAGONAL_ARCHITECTURE.md`](../context/HEXAGONAL_ARCHITECTURE.md).

**Bottom line:** The **v1 embedded engine and public primitives** described in the roadmap and checklist are in place; **`HEXXLA_DB.md` remains the north-star** for future MVCC, temporal indexes, and operational scale-out features that are explicitly not required to call the current library “feature-complete” for its milestone scope.

**Deeper gap analysis and phased integration plan:** [`SPEC_GAP_ANALYSIS_AND_INTEGRATION_PLAN.md`](./SPEC_GAP_ANALYSIS_AND_INTEGRATION_PLAN.md).
