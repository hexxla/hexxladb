# `internal/engine`

Embedded **storage shell** for HexxlaDB: **configurable page size** (4/8/16/64 KiB; default 4 KiB), a **primary database file** plus **`{path}-wal`**, a versioned **file header** on page 0, and an append-only **redo WAL** replayed on `Open`.

Normative byte layout and versioning live in **[`ENGINE_FORMAT.md`](./ENGINE_FORMAT.md)**. The **`hexxladb`** package at the module root delegates [`Open`](../../db.go) / [`Close`](../../db.go) here; domain and app code should depend only on the public API ([`HEXAGONAL_ARCHITECTURE.md`](../../docs/architecture/HEXAGONAL_ARCHITECTURE.md)).

**M3:** page I/O, WAL, hooks. **M4:** **B+ tree** ([`ORDERED_STORE.md`](./ORDERED_STORE.md)), root pointer in the file header; **`internal/index`** supplies **`cell/`** keys. **M5+:** public `View`/`Update`, MVCC-shaped locking.
