# Versioning and compatibility

This document describes how **module versions**, **Go API stability**, and **on-disk format** relate for [`github.com/hexxla/hexxladb`](https://github.com/hexxla/hexxladb).

## Go module (semver)

The module follows [Go module version numbering](https://go.dev/ref/mod#semantic-versioning):

- **`v0.x.y`** — The public [`package hexxladb`](doc.go) API may still evolve. Breaking changes are allowed while the project is pre-1.0; bump the **minor** version when adding features and the **patch** version for fixes, following normal Go ecosystem practice.
- **`v1.0.0` and later** — When the project commits to a stable public API, releases will use **major version** bumps (`v2`, …) for breaking changes to **exported** identifiers in `package hexxladb` and other **non-internal** packages (if any are added later).

**Internal packages** (`internal/...`) are not a compatibility promise for external callers: they may change at any release. Embed HexxlaDB only through the root module API and documented behavior.

## On-disk format

Persistence layout (page size, header fields, WAL records, B+ tree pages) is defined in [`internal/engine/ENGINE_FORMAT.md`](internal/engine/ENGINE_FORMAT.md) and the product spec [`docs/hexxladb/HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md).

- **`format_version`** in the file header is **forward-only** for a major line: older code may refuse to open newer files until upgraded.
- **`format_version` 2** (optional **[`Options.EnableMVCC`](options.go)** on **new** files only) stores **`commit_seq`** in the header and uses version-suffixed physical keys for cells and related indexes; see [`HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md). Existing v1 files are **not** auto-upgraded.
- A **breaking** on-disk change (e.g. new magic, incompatible page layout) should coincide with a **documented migration path** or a new major product/engine version—not a silent patch.

For backup and file pairing (primary + WAL), see [`docs/hexxladb/OPERATIONS.md`](docs/hexxladb/OPERATIONS.md).

## Encryption

Optional at-rest encryption is described in [`docs/hexxladb/ENCRYPTION.md`](docs/hexxladb/ENCRYPTION.md). Offline key rotation is available via [`RotateEncryption`](rotation.go); online re-encryption is still out of scope for the current versioning story.
