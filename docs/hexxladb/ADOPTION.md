# Production adoption (HexxlaDB)

**Normative product and storage:** [`HEXXLA.md`](./HEXXLA.md), [`HEXXLA_DB.md`](./HEXXLA_DB.md).
**Concept → API mapping:** [`HEXXLA_LIBRARY_MAPPING.md`](./HEXXLA_LIBRARY_MAPPING.md).
**Service wiring (ports / adapters):** [`HEXXLA_PRODUCT_WIRING.md`](./HEXXLA_PRODUCT_WIRING.md), [`../context/HEXAGONAL_ARCHITECTURE.md`](../context/HEXAGONAL_ARCHITECTURE.md).

## What the library provides (v1)

Embedded durable storage: hex lattice addressing, cells / facets / edges / seams, optional **MVCC** at database create time, optional **logical changefeed** and **at-rest encryption**, secondary indexes for **source**, **time**, **tag**, and seam provenance/temporal walks, view-assembly helpers, and operator-driven **MVCC prune** APIs. Default quality gate: **`make ci`**; extended coverage: **`make integration`** (includes MVCC churn + prune on a single cell).

## Before you claim “production” in *your* org

These are **process and evidence** items, not missing engine features:

- **Retention and disk growth** — set [`Options.MVCCRetention`](../../options.go), run [`PruneScheduler.Tick`](../../mvcc_lifecycle.go) or manual [`PruneCellVersions`](../../mvcc_lifecycle.go) on a schedule. See [`MVCC_RETENTION.md`](./MVCC_RETENTION.md), [`MVCC_TEMPORAL.md`](./MVCC_TEMPORAL.md).
- **Soak and operations** — capture real-world runs, backups, recovery drills. See [`OPERATIONS.md`](./OPERATIONS.md), [`OPERATOR_EVIDENCE.md`](./OPERATOR_EVIDENCE.md).
- **Changefeed consumers** — idempotency, lag, tail recovery. See [`CHANGEFEED.md`](./CHANGEFEED.md).

## Further confidence (optional)

Larger stress runs, mixed workloads, and benchmark baselines: [`BENCHMARKS.md`](./BENCHMARKS.md), **`make stress`**, **`make bench`**.

## Post–v1 (backlog, product-scoped)

Features such as materialized changefeed consumers, automated prune policy in-process, cross-node replication, or hybrid **`embed/`** routing are **out of the current spec** until a product milestone funds them. Track in your product plan, not in this repository’s delivery bar.

## Spec maintenance

When on-disk behavior or public API changes, update **`HEXXLA_DB.md`** and this module’s [`doc.go`](../../doc.go) in the same change (or an immediate follow-up) so integrators see one consistent story.
