# Versioning and compatibility

**Current published version:** `v0.5.1`

**Next release candidate:** `v0.6.0`

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
- The optional changelog has its own framing version: plaintext databases use format v1; encrypted databases use authenticated encrypted format v2. Modes are not mixed. Offline encryption rotation converts and re-encrypts the changelog when it remains enabled at the same path; an already encrypted database rejects a legacy plaintext changelog until operators archive/reconcile it.
- Changelog-enabled commits store private `__meta/changelog-head` and `__meta/changelog-outbox/` records in both database formats. These keys do not change the engine format version; they make the sidecar a recoverable at-least-once projection. Older libraries can preserve the unknown keys but do not perform outbox recovery, so downgrade while intents are pending is unsupported.
- A **breaking** on-disk change (e.g. new magic, incompatible page layout) should coincide with a **documented migration path** or a new major product/engine version—not a silent patch.

Unknown engine versions fail with `ErrUnsupportedFormatVersion`; they are never
opened as a known format. Format-v1 files remain v1 when opened with
`Options.EnableMVCC`. Use `MigrateV1ToV2` for the explicit, source-preserving
upgrade path; see [`OPERATIONS.md`](docs/hexxladb/OPERATIONS.md#format-v1-to-v2-migration).

For backup and file pairing (primary + WAL), see [`docs/hexxladb/OPERATIONS.md`](docs/hexxladb/OPERATIONS.md).

## Encryption

Optional at-rest encryption is described in [`docs/hexxladb/ENCRYPTION.md`](docs/hexxladb/ENCRYPTION.md). Offline key rotation, including an enabled changelog, is available via [`RotateEncryption`](rotation.go); online re-encryption and authenticated primary-page format migration remain out of scope for the current versioning story.

## Compatibility matrix

“Read/write” means the library recognizes the format and preserves its contract;
it does not mean that every newer API is meaningful on format v1.

| Library line | Engine v1 | Engine v2 | Plaintext changelog v1 | Encrypted changelog v2 | Newer engine format |
| --- | --- | --- | --- | --- | --- |
| `v0.1.0`–`v0.5.1` | Read/write | Read/write | Release-specific; consult that tag | Unsupported before the encrypted log was introduced | Refused as corrupt/unsupported |
| `[Unreleased]` (planned `v0.6.0`) | Read/write; explicit migration source | Read/write; migration destination | Read/write | Read/write with matching database credentials | Refused with `ErrUnsupportedFormatVersion` |

Compatibility rules:

- A completed v1-to-v2 migration is an ordinary format-v2 database. Libraries
  that already support format v2 may open it, but only the current library
  understands and refuses an incomplete migration checkpoint. Never downgrade
  or open a partial destination outside `MigrateV1ToV2`.
- A library may open only engine formats listed as supported for that release.
  Copy a recovery set before upgrading; do not test downgrade behavior on the
  only copy of production data.
- Primary encryption is an engine feature using the locked XTS derivation label.
  It does not change engine version. A wrong or missing credential fails closed.
- Changelog framing is independent: v1 is plaintext and v2 is authenticated
  encrypted framing. A plaintext/encrypted mismatch is refused; frames are not mixed.
- Migration creates a new MVCC timeline. Existing changelog history and consumer
  cursors are not silently transplanted; explicit `ResetChangelog` authorization
  is required when such state exists.

## Root API stability inventory

The exhaustive declaration reference is generated from source with `go doc`.
This inventory assigns every exported root-package surface by its owning file;
symbols in a listed file inherit that row’s status. Aliases re-exported by
`export.go` are part of the root API. Unexported receiver types such as the HNSW
storage implementation are not public API even if they have exported methods.

| Status | Owning root files | Policy before v1 |
| --- | --- | --- |
| v1 candidate: core storage and records | `db.go`, `tx.go`, `options.go`, `errors.go`, `export.go`, `primitives.go`, `facets_edges.go`, `delete_cell.go`, `batch.go`, `cell_secondary.go`, `seam_secondary.go`, `hooks.go` | Breaking changes require an `[Unreleased]` migration note now; compatibility is mandatory at v1. |
| v1 candidate: durability and operations | `backup.go`, `compact.go`, `migration.go`, `rotation.go`, `encryption.go`, `health.go`, `storage_stats.go`, `write_stats.go`, `mvcc_lifecycle.go`, `snapshot_tags.go`, `db_changelog.go`, `changelog_consumers.go` | Operational failure and recovery semantics are compatibility commitments. |
| v1 candidate: embeddings and views | `embedding.go`, `tx_embedding.go`, `views.go` | Stored encodings and documented root signatures are candidates for v1 commitment. |
| Provisional: retrieval and derived algorithms | `context_load.go`, `search.go`, `query.go`, `query_exec.go`, `embedding_search.go`, `embedding_reindex.go`, `fov_context.go`, `voronoi_context.go`, `pathfind_api.go`, `snapshot_diff.go`, `superhex_summary.go`, `ring_density.go`, `tag_analytics.go`, `hex_render.go` | Must be committed, deprecated, or changed with a migration note before v1. |
| Provisional: product convenience | `templates.go` | May move or change before v1; applications should isolate use. |

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
| API commitment | Every root file in the inventory is v1-candidate or deprecated; no provisional row remains; all pre-v1 breaks have migration notes. | Provisional retrieval, derived-algorithm, and convenience rows remain above. | **Not met** |
| Format upgrade and refusal | v1 fixture migrates to logically equivalent v2 data/indexes; cancel/resume preserves source; encryption and collision cases pass; newer formats are refused. | `migration_test.go`: `TestMigrateV1ToV2*`, `TestOpenRefusesNewerFormatVersion`. | **Pass** |
| Supported toolchain policy | `task ci` runs format, vet, boundaries, race, vulnerability, pinned lint, standalone security, complexity, and tidy without skipped or unreconciled checks on Go 1.27.0. | Pinned versions and toolchain are in `scripts/tool.sh`; release evidence must attach the successful `task ci` output. | **Pending release run** |
| Crash and recovery | `task integration` passes on the release commit, including named crash barriers; a recovery drill is recorded. | Test procedure exists in `CONTRIBUTING.md` and `OPERATIONS.md`; no v1-candidate run is recorded. | **Not met** |
| Performance envelope | Seeded performance, vector-scale, and lattice-placement evidence is regenerated on release hardware and stays within documented thresholds. | Reproducible commands and current baselines exist in `PERFORMANCE_EVIDENCE.md`; no v1-candidate run is recorded. | **Not met** |
| Release operations | Signed/tagged release rehearsal, SBOM generation, backup drill, and rollback/downgrade refusal drill complete from the candidate commit. | Workflows and drills exist; no v1-candidate rehearsal is recorded. | **Not met** |
