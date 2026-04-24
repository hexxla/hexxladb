# HexxlaDB roadmap and backlog

**Purpose:** Single place for **out-of-scope** decisions, **low-risk** process/docs work, and **documented future** items so the spec and code stay alignable. This is not a release contract; see [`VERSIONING.md`](../VERSIONING.md) for compatibility rules.

---

## Explicitly out of scope (library v1)

These are **intentional** boundaries for the embedded reference implementation unless a future milestone says otherwise:

| Area                                                    | Rationale                                                                                                                                                       | References                                                                                                           |
| ------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| **Distributed replication / HA**                        | Not a library feature today; belongs in product/orchestration.                                                                                                  | —                                                                                                                    |
| **Freelist / primary file shrink**                      | Extend-only page allocator; disk does not shrink when rows are deleted or pruned. Operators use backup/copy into a fresh file if they need a smaller footprint. | [`OPERATIONS.md`](./hexxladb/OPERATIONS.md) § File growth                                                            |
| **`PageSize` / `maxValBytes` change** without migration | Requires format-version bump and migration tooling.                                                                                                             | [`HEXXLA_DB.md`](./hexxladb/HEXXLA_DB.md), [`internal/engine/ENGINE_FORMAT.md`](../internal/engine/ENGINE_FORMAT.md) |
| **Third-party ordered-KV / SQLite as storage core**     | Hex-native engine is the product direction.                                                                                                                     | [`HEXXLA_DB.md`](./hexxladb/HEXXLA_DB.md)                                                                            |
| **Online re-encryption**                                | Offline rotation exists; continuous re-encryption not in current versioning story.                                                                              | [`VERSIONING.md`](../VERSIONING.md)                                                                                  |
| **SQLite-style WAL merged into cold read path**         | Reads use engine overlay during group commit; no separate WAL merge reader like SQLite’s combined read path for v1 product docs here.                           | [`DURABILITY.md`](./hexxladb/DURABILITY.md)                                                                          |

---

## Completed (low-risk, process / docs)

Tracked here so contributors know where “done” landed:

| Item                                       | Notes                                                                                                                                                                                            |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Tiered CI                                  | PRs: [`scripts/ci.sh`](../scripts/ci.sh). Integration: [`../.github/workflows/integration.yml`](../.github/workflows/integration.yml) (weekly + manual). [`CONTRIBUTING.md`](../CONTRIBUTING.md) |
| Extend-only allocation documented          | [`OPERATIONS.md`](./hexxladb/OPERATIONS.md)                                                                                                                                                      |
| Storage limits (256 B keys / 512 B values) | [`API_REFERENCE.md`](./hexxladb/API_REFERENCE.md)                                                                                                                                                |
| `DB.GroupWALStats` forwarder               | Operators avoid importing `internal/engine`; see [`DURABILITY.md`](./hexxladb/DURABILITY.md)                                                                                                     |

---

## Near-term candidates (engineering, not promised)

Higher value but **requires design + benchmarks** before implementation:

| Idea                                       | Notes                                                                                                                                                                                               |
| ------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Batch MVCC prune** (`PruneCellVersions`) | Today each delete uses immediate `WritePage`; coalescing deletes under one engine write txn would reduce WAL/sync pressure for large prune passes. See [`DURABILITY.md`](./hexxladb/DURABILITY.md). |
| **Measured need**                          | Expose/adjust metrics before building batch APIs.                                                                                                                                                   |

---

## Documented future / product-tier (spec ahead of shipped code)

Audit of docs vs current engine/library — **these appear in normative or product docs as optional/future**:

| Topic                                                               | Spec / doc says                                                                      | Code today                                                                                                     |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------- |
| **`embed/` keyspace, ANN / hybrid retrieval**                       | Optional future hybrid index path in [`HEXXLA_DB.md`](./hexxladb/HEXXLA_DB.md).      | Not implemented as a first-class store family in v1.                                                           |
| **Materialized views / super-hex aggregation as engine algorithms** | Called out as orchestration/future in [`HEXXLA_DB.md`](./hexxladb/HEXXLA_DB.md).     | App-layer / future milestone.                                                                                  |
| **Materialized changefeed consumers, automated prune in-process**   | Product-tier orchestration.                                                          | Library provides APIs + scheduler hook; policy automation is product-owned.                                    |
| **Cross-node replication**                                          | Product-tier orchestration.                                                          | Out of scope for embedded library.                                                                             |
| **Online re-encryption**                                            | [`VERSIONING.md`](../VERSIONING.md).                                                 | Only offline [`RotateEncryption`](../rotation.go) path documented.                                             |
| **`ErrNotImplemented`**                                             | Listed in [`doc.go`](../doc.go) / [`API_REFERENCE.md`](./hexxladb/API_REFERENCE.md). | Sentinel exists in [`errors.go`](../errors.go); **no exported API currently returns it** (reserved for stubs). |

---

## How to update this file

- After a major audit or planning discussion, refresh **Explicitly out of scope** and **Documented future** tables.
- When closing a backlog item in code, move it to **Completed** or delete the row with a pointer to the PR/version.

---

## Audit log (maintenance)

| Date       | Scope                                                                                                      | Notes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ---------- | ---------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-04-24 | **v1.0.0 release**                                                                                         | Stable release. Production-ready with MVCC, encryption, WAL durability, hexagonal lattice, seams/contradictions, token-budgeted context assembly, comprehensive tests, and LLM guardrails (`.cursor/`, `.windsurf/`, `.claude/`, `.codex/`). `VERSIONING.md` updated to reflect stable semver.                                                                                                                                                                                               |
| 2026-04-23 | Repo grep (`ErrNotImplemented`, `TODO`/`FIXME` in `*.go`/`*.md`), normative docs mentioning future/post-v1 | **`ErrNotImplemented`** only in `errors.go`, `doc.go`, `API_REFERENCE.md`, this file — no exported call sites. **`HEXXLA.md`** defers heavy seam analysis to post-v1. Re-run this pass after large doc or API changes.                                                                                                                                                                                                                                                                       |
| 2026-04-23 | Docs consolidation audit                                                                                   | Deleted historical/past-decision docs (ADR, MVCC_E2_DECISIONS, MVCC_DESIGN, OPERATOR_EVIDENCE, HEXXLA_PRODUCT_WIRING, HEXXLA_LIBRARY_MAPPING, ADOPTION, SERVICE_INTEGRATION, BENCHMARKS, MODERN_GO, RESILIENCY). Consolidated MVCC_RETENTION + ADOPTION operator content into `OPERATIONS.md` and MVCC_TEMPORAL into `TX.md`. Retained set: `HEXXLA.md`, `HEXXLA_DB.md`, `API_REFERENCE.md`, `TX.md`, `OPERATIONS.md`, `DURABILITY.md`, `ENCRYPTION.md`, `CHANGEFEED.md`, plus this roadmap. |
