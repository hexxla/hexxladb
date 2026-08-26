# Versioning and compatibility

**Current release:** `v0.6.0`

This document describes how **module versions**, **Go API stability**, and **on-disk format** relate for [`github.com/hexxla/hexxladb`](https://github.com/hexxla/hexxladb).

## Go module (semver)

The module follows [Semantic Versioning 2.0.0](https://semver.org/):

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

Persistence layout (page size, header fields, WAL records, B+ tree pages) is defined in [`internal/engine/ENGINE_FORMAT.md`](internal/engine/ENGINE_FORMAT.md) and the product spec [`docs/hexxladb/HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md).

- **`format_version`** in the file header is **forward-only** for a major line: older code may refuse to open newer files until upgraded.
- **`format_version` 2** (optional **[`Options.EnableMVCC`](options.go)** on **new** files only) stores **`commit_seq`** in the header and uses version-suffixed physical keys for cells and related indexes; see [`HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md). Existing v1 files are **not** auto-upgraded.
- **`format_version` 3** is selected for every newly created database that uses
  `EncryptionKey` or `Passphrase`. It retains MVCC keys and adds authenticated
  XChaCha20-Poly1305 page envelopes, an authenticated header/root generation,
  generation-linked freelist metadata, bounded tail reclaim, and WAL header
  commit markers. Existing encrypted v1/v2 files remain readable through the
  frozen AES-XTS path and are not auto-upgraded.
- The optional changelog has its own framing version: plaintext databases use format v1; encrypted databases use authenticated encrypted format v2. Modes are not mixed. Offline encryption rotation converts and re-encrypts the changelog when it remains enabled at the same path; an already encrypted database rejects a legacy plaintext changelog until operators archive/reconcile it.
- Changelog-enabled commits store private `__meta/changelog-head` and `__meta/changelog-outbox/` records in both database formats. These keys do not change the engine format version; they make the sidecar a recoverable at-least-once projection. Older libraries can preserve the unknown keys but do not perform outbox recovery, so downgrade while intents are pending is unsupported.
- A **breaking** on-disk change (e.g. new magic, incompatible page layout) should coincide with a **documented migration path** or a new major product/engine version—not a silent patch.

Unknown engine versions fail with `ErrUnsupportedFormatVersion`; they are never
opened as a known format. Format-v1 files remain v1 when opened with
`Options.EnableMVCC`. Use `MigrateV1ToV2` for the explicit, source-preserving
upgrade path; see [`OPERATIONS.md`](docs/hexxladb/OPERATIONS.md#format-v1-to-v2-migration).
Use `MigrateToAuthenticated` for a source-preserving v1/v2-to-v3 candidate; see
[`OPERATIONS.md`](docs/hexxladb/OPERATIONS.md#authenticated-format-v3-migration).

For backup and file pairing (primary + WAL), see [`docs/hexxladb/OPERATIONS.md`](docs/hexxladb/OPERATIONS.md).

## Encryption

At-rest encryption is described in [`docs/hexxladb/ENCRYPTION.md`](docs/hexxladb/ENCRYPTION.md). `MigrateToAuthenticated` provides offline source-preserving v1/v2-to-v3 migration. Offline key rotation, including an enabled changelog, is available through [`RotateEncryption`](rotation.go); online re-encryption remains out of scope.

This is a documented pre-v1 creation change: callers that supplied encryption
credentials previously created an AES-XTS v1/v2 file; they now create an
authenticated MVCC v3 file. Existing files retain their format. Older libraries
must refuse v3, and rollback requires the preserved source or a pre-upgrade
backup because no v3-to-v2 writer exists.

## Production-readiness profile

Production readiness is a bounded claim about a tested deployment profile, not
a synonym for reaching `v1.0.0`. HexxlaDB may complete this profile while still
using a `v0.y.z` version; the v1 API commitment remains a separate decision.

The initial candidate profile is:

- the Go module embedded in one Linux/amd64 process using Go 1.27.0 or newer;
- exactly one open owner for each database path and serialized public writes;
- a trusted local filesystem that honors exclusive creation, file locking, and
  `fsync`/`fdatasync` durability for the primary, WAL, and optional changelog;
- authenticated engine format v3 for encrypted new deployments, or a completed
  and verified v1/v2-to-v3 migration, with 4 KiB logical pages;
- MVCC enabled, with authenticated primary pages, encrypted changelog, and
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
| Candidate validation | `task ci`, `task integration`, fuzz smoke tests, and supported-platform builds pass on the same commit without skipped or unreconciled checks. | **Local v0.6.0 candidate gates pass on 2026-08-26; the protected tag workflow must repeat them on the exact published tag** |
| Recovery set durability | Crash-barrier, combined encrypted backup/restore, migration, changelog-consumer, compaction, and required directory-durability checks pass on the candidate commit. | **Automated set and local synthetic restore rehearsal pass on the v0.6.0 candidate; adopting-operator restore drill pending** |
| Performance and soak | Controlled, observation, vector-scale, and placement evidence passes on release hardware; a representative bounded workload meets the adopting deployment's declared targets. | **Reproducible evidence and the conservative five-minute, minimum-sample reference qualification pass; adopting deployments must declare and validate material deviations from that profile** |
| Release operations | Reproducible artifacts, checksums, signing, SBOM generation, installation, upgrade, and rollback/refusal drills pass from the candidate commit. | **Cross-build, isolated Linux install, published-fixture migration, and newer-format refusal pass on the v0.6.0 candidate; final signed protected tag and adopting-operator rollback drill pending** |
| Limited-production adoption | A named owner records the deployed profile, monitoring window, backup/restore drill, and upgrade report with no unresolved correctness or data-loss finding. | **Not met** |

These gates do not promote provisional APIs to a v1 compatibility promise. A
pre-v1 production adopter must pin the module version, isolate provisional
surfaces, and review documented migration notes before every upgrade. The
separate v1 gates additionally require resolving the complete API inventory.

## Compatibility matrix

“Read/write” means the library recognizes the format and preserves its contract;
it does not mean that every newer API is meaningful on format v1.

| Library line | Engine v1 | Engine v2 | Engine v3 | Plaintext changelog v1 | Encrypted changelog v2 | Newer engine format |
| --- | --- | --- | --- | --- | --- | --- |
| `v0.1.0`–`v0.5.1` | Read/write | Read/write | Unsupported | Release-specific; consult that tag | Unsupported before the encrypted log was introduced | Refused as corrupt/unsupported |
| `v0.6.0` | Read/write; migration source | Read/write; migration source/destination | Read/write; default for new encrypted files | Read/write | Read/write with matching database credentials | Refused with `ErrUnsupportedFormatVersion` |

Compatibility rules:

- A completed v1-to-v2 migration is an ordinary format-v2 database. Libraries
  that already support format v2 may open it, but only the current library
  understands and refuses an incomplete migration checkpoint. Never downgrade
  or open a partial destination outside `MigrateV1ToV2`.
- Copy-compaction candidates temporarily carry an incomplete feature bit in
  either format. The current library refuses an interrupted candidate with
  `ErrCompactionIncomplete`; remove that candidate and retry from the preserved
  source. A successfully finalized database never retains the bit, so this does
  not change the format version of completed files. Do not open a partial
  candidate with an older library that predates this refusal.
- A library may open only engine formats listed as supported for that release.
  Copy a recovery set before upgrading; do not test downgrade behavior on the
  only copy of production data.
- Legacy v1/v2 encryption uses the locked XTS derivation label. New encryption
  uses engine v3 and its domain-separated authenticated key hierarchy. A wrong
  or missing credential fails closed in both paths.
- Changelog framing is independent: v1 is plaintext and v2 is authenticated
  encrypted framing. A plaintext/encrypted mismatch is refused; frames are not mixed.
- Migration creates a new MVCC timeline. Existing changelog history and consumer
  cursors are not silently transplanted; explicit `ResetChangelog` authorization
  is required when such state exists.
- Deferred embedding ingestion persists an `hnsw/state` lifecycle record. Older
  libraries preserve the unknown row but do not honor its dirty flag and could
  query a stale graph. Do not downgrade while an embedding index rebuild is
  pending; complete the rebuild before opening the database with an older
  release.

## Root API stability inventory

The exhaustive declaration reference is generated from source with `go doc`.
This inventory assigns every exported root-package surface by its owning file;
symbols in a listed file inherit that row’s status. Aliases re-exported by
`export.go` are part of the root API. Unexported receiver types such as the HNSW
storage implementation are not public API even if they have exported methods.

`task api-check` compares declarations, exported fields and methods, symbol
documentation, constants, and the definitions behind root aliases with
[`api/public-v0.6.txt`](api/public-v0.6.txt). The check is intentionally exact:
additive and documentation changes also require compatibility review before
regenerating the baseline with
`go run ./scripts/api_surface.go -write`. Passing the check prevents accidental
drift; it does not by itself authorize a breaking change.

| Status | Owning root files | Policy before v1 |
| --- | --- | --- |
| v1 candidate: core storage and records | `db.go`, `tx.go`, `options.go`, `errors.go`, `export.go`, `primitives.go`, `facets_edges.go`, `delete_cell.go`, `batch.go`, `cell_secondary.go`, `seam_secondary.go`, `hooks.go` | Breaking changes require an `[Unreleased]` migration note now; compatibility is mandatory at v1. |
| v1 candidate: durability and operations | `backup.go`, `compact.go`, `reclaim.go`, `maintenance_preflight.go`, `migration.go`, `rotation.go`, `encryption.go`, `health.go`, `storage_stats.go`, `write_stats.go`, `mvcc_lifecycle.go`, `snapshot_tags.go`, `db_changelog.go`, `changelog_consumers.go` | Operational failure and recovery semantics are compatibility commitments. |
| v1 candidate: embeddings and views | `embedding.go`, `tx_embedding.go`, `embedding_index_rebuild.go`, `views.go` | Stored encodings and documented root signatures are candidates for v1 commitment. |
| v1 candidate: retrieval and query | `context_load.go`, `search.go`, `query.go`, `query_exec.go`, `embedding_search.go`, `embedding_reindex.go` | Result bounds, ordering, exact/approximate path selection, and error behavior are candidate commitments. |
| v1 candidate: spatial and derived diagnostics | `fov_context.go`, `voronoi_context.go`, `pathfind_api.go`, `snapshot_diff.go`, `superhex_summary.go`, `ring_density.go`, `tag_analytics.go`, `hex_render.go` | Determinism, bounded execution, and documented retained-history limitations are candidate commitments. |
| v1 candidate: record convenience | `templates.go` | Constructor signatures and returned record shapes are candidate commitments; applications still own product policy. |

Every pre-v1 breaking change must include all of: an explicit `CHANGELOG.md`
breaking entry, affected old and new signatures or behavior, caller migration
steps, and any on-disk/open compatibility impact. Deprecations remain exported
for at least one subsequent minor release unless a documented security or data
integrity issue requires faster removal.

## V1 release gates

These gates are evaluated on the proposed v1 release commit. “Not met” is a
recorded result, not permission to infer readiness from a version number.

| Gate | Pass condition | Current recorded evidence | Status |
| --- | --- | --- | --- |
| Production adoption | At least one named production deployment has an owner-confirmed backup/restore drill and upgrade report. | No production deployment evidence is recorded in the repository. | **Not met** |
| API commitment | Every root file in the inventory is v1-candidate or deprecated; no provisional row remains; all pre-v1 breaks have migration notes. | Every exported root file is classified above; consumer-facing signatures use root aliases and `task api-check` enforces the v0.6 candidate baseline. | **Pass on current candidate; repeat at release** |
| Format upgrade and refusal | v1 fixture migrates to logically equivalent v2 and authenticated v3 data/indexes; v2 migrates with history preserved; cancellation preserves source; older/newer formats are refused. | `migration_test.go`: `TestMigrateV1ToV2*`, `TestMigrate*Authenticated*`, `TestMigratePublishedV051Fixture`, `TestOpenRefusesNewerFormatVersion`; authenticated page/WAL faults are covered by `encryption*_test.go` and `internal/engine/authenticated_recovery_test.go`. | **Pass on current candidate; repeat at release** |
| Supported toolchain policy | `task ci` runs format, vet, boundaries, workflow lint, race, vulnerability, pinned Go lint, standalone security, complexity, and tidy without skipped or unreconciled checks on Go 1.27.0. | Pinned versions and toolchain are in `scripts/tool.sh`; release evidence must attach the successful `task ci` output. | **Pending release run** |
| Crash and recovery | `task integration` passes on the release commit, including named crash barriers; a recovery drill is recorded. | Test procedure exists in `CONTRIBUTING.md` and `OPERATIONS.md`; no v1-candidate run is recorded. | **Not met** |
| Performance envelope | Seeded performance, vector-scale, and lattice-placement evidence is regenerated on release hardware and stays within documented thresholds. | Reproducible commands and current baselines exist in `PERFORMANCE_EVIDENCE.md`; no v1-candidate run is recorded. | **Not met** |
| Release operations | Signed/tagged release rehearsal, SBOM generation, backup drill, and rollback/downgrade refusal drill complete from the candidate commit. | Workflows and drills exist; no v1-candidate rehearsal is recorded. | **Not met** |
