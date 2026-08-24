# Versioning and compatibility

**Current version:** `v0.8.0`

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
- **`v1.0.0`** — First stable release when:
  - Software is used in production
  - Stable API on which users depend
  - Backward compatibility becomes a primary concern

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

For backup and file pairing (primary + WAL), see [`docs/hexxladb/OPERATIONS.md`](docs/hexxladb/OPERATIONS.md).

## Encryption

Optional at-rest encryption is described in [`docs/hexxladb/ENCRYPTION.md`](docs/hexxladb/ENCRYPTION.md). Offline key rotation, including an enabled changelog, is available via [`RotateEncryption`](rotation.go); online re-encryption and authenticated primary-page format migration remain out of scope for the current versioning story.
