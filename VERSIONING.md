# Versioning and compatibility

**Current release:** `v0.7.0`

This document describes how **module versions**, **Go API stability**, and **on-disk format** relate for [`github.com/hexxla/hexxladb`](https://github.com/hexxla/hexxladb).

## Go module (semver)

The module follows [Semantic Versioning 2.0.0](https://semver.org/).
Publish versions as annotated Git tags on validated commits from `main`.

### Version format

Versions follow `MAJOR.MINOR.PATCH` (e.g., `v0.1.0`, `v1.2.3`).

- **MAJOR** — Breaking changes to the public API
- **MINOR** — New functionality, backward-compatible
- **PATCH** — Bug fixes, backward-compatible

### 0.y.z initial development phase

- **`v0.x.y`** — API may evolve. Breaking changes are allowed while the project matures.
  - Start at `v0.1.0` for first usable release
  - Increment **minor** for each subsequent release with new features
  - Increment **patch** for fixes
- **`v1.0.0`** — First stable release only when every measurable gate in
  [V1 release gates](#v1-release-gates) has current evidence on the release commit.

### Post-1.0 stable phase

- **`v1.x.y` and later** — Breaking changes require **major** version bump (`v2`, …)
- **Minor** bumps (`v1.1.0`) add features backward-compatibly
- **Patch** bumps (`v1.0.1`) are fixes only

**Internal packages** (`internal/...`) are not a compatibility promise: they may change at any release. Embed HexxlaDB only through the root module API and documented behavior.

## On-disk format

Plaintext files use engine v2 and encrypted files use authenticated engine v3.
Both use the same MVCC records and commit sequences. V3 adds authenticated
XChaCha20-Poly1305 page envelopes, authenticated WAL/header publication,
generation-linked free pages, and tail reclaim. See the
[engine layout](internal/engine/ENGINE_FORMAT.md) and
[encryption guide](docs/hexxladb/ENCRYPTION.md).

The main branch refuses single-version engine v1, AES-XTS files, and
unknown engine versions with `ErrUnsupportedFormatVersion`. The
[offline conversion procedure](docs/hexxladb/OPERATIONS.md#offline-conversion-of-retired-files)
uses the pinned v0.6.0 release for legacy files. The current library checks
for unfinished conversion candidates and refuses them.

Changelog framing is independent. Plaintext uses changelog v1 and encrypted
databases use authenticated changelog v2. Offline
[`RotateEncryption`](rotation.go) can encrypt a plaintext v2 database or change
an encrypted v3 database's credential while preserving its changelog. Retain
the complete recovery set until the replacement and its backup are verified.

## Production-readiness profile

Production readiness is a bounded claim about a tested deployment profile, not
a synonym for reaching `v1.0.0`. HexxlaDB may complete this profile while still
using a `v0.y.z` version; the v1 API commitment remains a separate decision.

The initial candidate profile is:

- the Go module embedded in one Linux/amd64 process using Go 1.27.3 or newer;
- exactly one open owner for each database path and serialized public writes;
- a trusted local filesystem that honors exclusive creation, file locking, and
  `fsync`/`fdatasync` durability for the primary, WAL, and optional changelog;
- authenticated engine format v3 for encrypted deployments, with 4 KiB logical pages;
- MVCC, with authenticated primary pages, encrypted changelog, and
  durable consumers exercised together when those optional features are used;
- at most 20,000 stored vectors at 32 dimensions or 10,000 at 384 dimensions
  with a 64 MiB page cache unless the deployment regenerates vector-scale
  evidence for its larger or different vector workload;
- low-to-moderate serialized writes or bounded batch ingestion whose measured
  latency, file growth, and maintenance windows meet the deployment's targets;
- application-owned monitoring, idempotent changefeed consumption, backup
  retention, restore drills, MVCC pruning, and explicit compaction.

The production-readiness claim excludes network service operation, concurrent
or distributed writers, replication, automatic failover, unbounded vector or
record scale, high-volume random-write workloads, and protection from an
attacker who can delete storage, replay an older valid non-root page into the
same slot, or roll back the complete recovery set. Those limits require trusted
storage and independently authenticated/versioned backups as described in
[`ENCRYPTION.md`](docs/hexxladb/ENCRYPTION.md#threat-model-and-residual-limits).

Every adopting deployment must declare its expected dataset and vector sizes,
read/write mix, enabled options, latency/throughput objectives, backup interval,
RPO, and RTO. It must then pass the bounded production-readiness gates below.
The repository records mechanisms and reproducible evidence; it does not invent
site-specific service-level objectives.

| Bounded production-readiness gate | Pass condition | Current status |
| --- | --- | --- |
| Candidate validation | `task ci`, `task integration`, fuzz smoke tests, and supported-platform builds pass on the same commit without skipped or unreconciled checks. | **Local v0.6.0 candidate gates passed on 2026-08-26. Repeat them on the commit selected for the next adoption** |
| Recovery set durability | Crash-barrier, combined encrypted backup/restore, migration, changelog-consumer, compaction, and required directory-durability checks pass on the candidate commit. | **Automated set and local synthetic restore rehearsal pass on the v0.6.0 candidate; adopting-operator restore drill pending** |
| Performance and soak | Controlled, observation, vector-scale, and placement evidence passes on release hardware; a representative bounded workload meets the adopting deployment's declared targets. | **Reproducible evidence and the conservative five-minute, minimum-sample reference qualification pass; adopting deployments must declare and validate material deviations from that profile** |
| Release operations | The operator can build from the candidate source, install, upgrade, and complete rollback and format refusal drills. | **Cross-build, isolated Linux install, published-fixture migration, and newer-format refusal passed on the v0.6.0 candidate. The adopting operator’s rollback drill is pending** |
| Limited-production adoption | A named owner records the deployed profile, monitoring window, backup/restore drill, and upgrade report with no unresolved correctness or data-loss finding. | **Not met** |

These gates do not promote provisional APIs to a v1 compatibility promise. A
pre-v1 production adopter must pin the module version, isolate provisional
surfaces, and review documented migration notes before every upgrade. The
separate v1 gates additionally require resolving the complete API inventory.

## Compatibility policy before v1

The main branch maintains the two current storage profiles. Older releases
remain available by tag for conversion and recovery. Before v1, intentional
API removals and retired formats can ship in a minor release with changelog
and upgrade notes. Keep candidate publication, unsupported-format refusal,
and data-integrity checks for the formats the current library accepts.

## Root API stability inventory

The exhaustive declaration reference is generated from source with `go doc`.
This inventory assigns every exported root-package surface by its owning file;
symbols in a listed file inherit that row’s status. Aliases re-exported by
`export.go` are part of the root API. Unexported receiver types such as the HNSW
storage implementation are not public API even if they have exported methods.

`task api-check` compares declarations, exported fields and methods, symbol
documentation, constants, and the definitions behind root aliases with
[`api/public.txt`](api/public.txt). The check is intentionally exact:
additive and documentation changes also require compatibility review before
regenerating the baseline with
`go run ./scripts/api_surface.go -write`. Passing the check prevents accidental
drift; it does not by itself authorize a breaking change.

| Status | Owning root files | Policy before v1 |
| --- | --- | --- |
| v1 candidate: core storage and records | `db.go`, `tx.go`, `options.go`, `errors.go`, `export.go`, `cells.go`, `seams.go`, `seam_scan.go`, `spatial_reads.go`, `facets_edges.go`, `delete_cell.go`, `batch.go`, `cell_secondary.go`, `seam_secondary.go`, `hooks.go` | Breaking changes require an `[Unreleased]` migration note now; compatibility is mandatory at v1. |
| v1 candidate: durability and operations | `backup.go`, `compact.go`, `reclaim.go`, `maintenance_preflight.go`, `rotation.go`, `encryption.go`, `health.go`, `storage_stats.go`, `write_stats.go`, `mvcc_lifecycle.go`, `mvcc_snapshot_floor.go`, `snapshot_tags.go`, `db_changelog.go`, `changelog_consumers.go` | Operational failure and recovery semantics are compatibility commitments. |
| v1 candidate: embeddings and views | `embedding.go`, `tx_embedding.go`, `embedding_index_rebuild.go`, `views.go` | Stored encodings and documented root signatures are candidates for v1 commitment. |
| v1 candidate: retrieval and query | `context_load.go`, `search.go`, `query.go`, `query_exec.go`, `embedding_search.go`, `embedding_reindex.go` | Result bounds, ordering, exact/approximate path selection, and error behavior are candidate commitments. |
| v1 candidate: spatial and derived diagnostics | `fov_context.go`, `voronoi_context.go`, `pathfind_api.go`, `snapshot_diff.go`, `superhex_summary.go`, `ring_density.go`, `tag_analytics.go`, `hex_render.go` | Determinism, bounded execution, and documented retained-history limitations are candidate commitments. |
| v1 candidate: record convenience | `templates.go` | Constructor signatures and returned record shapes are candidate commitments; applications still own product policy. |

Every pre-v1 breaking change must include all of: an explicit `CHANGELOG.md`
breaking entry, affected old and new signatures or behavior, caller migration
steps, and any on-disk/open compatibility impact. A deprecation period is optional before v1.

## V1 release gates

These gates are evaluated on the proposed v1 release commit. “Not met” is a
recorded result, not permission to infer readiness from a version number.

| Gate | Pass condition | Current recorded evidence | Status |
| --- | --- | --- | --- |
| Production adoption | At least one named production deployment has an owner-confirmed backup/restore drill and upgrade report. | No production deployment evidence is recorded in the repository. | **Not met** |
| API commitment | Every root file in the inventory is v1-candidate or deprecated; no provisional row remains; all pre-v1 breaks have migration notes. | Every exported root file is classified above; consumer-facing signatures use root aliases and `task api-check` enforces the current candidate baseline. | **Pass on current candidate; repeat at release** |
| Format upgrade and refusal | Retired and unknown formats are refused without modifying their recovery sets. Rotation preserves plaintext v2 and encrypted v3 history. | `TestRetiredFormatsLeaveRecoverySetIntact` and `TestRotateEncryptionPreservesMVCC` cover these paths. Rehearse pinned offline conversion for any legacy recovery sets used in the release drill. | Recheck on the exact v1 candidate. |
| Supported toolchain policy | `task ci` validates formatting, lint, import boundaries, workflows, exported API, race tests, and module metadata. `task govulncheck` checks the candidate for known reachable vulnerabilities. | Run both checks on the exact v1 candidate. |
| Crash and recovery | `task integration` passes on the release commit, including named crash barriers; a recovery drill is recorded. | Test procedure exists in `CONTRIBUTING.md` and `OPERATIONS.md`; no v1-candidate run is recorded. | **Not met** |
| Performance envelope | Seeded performance, vector-scale, and lattice-placement evidence is regenerated on release hardware and stays within documented thresholds. | Reproducible commands and current baselines exist in `PERFORMANCE_EVIDENCE.md`; no v1-candidate run is recorded. | **Not met** |
| Release operations | Build from the candidate source, then complete the backup, rollback, and downgrade refusal drills. | The operations guide documents the drills. A v1-candidate rehearsal is pending. | **Not met** |
