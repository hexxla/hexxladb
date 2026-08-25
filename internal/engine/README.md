# `internal/engine`

Embedded storage engine for HexxlaDB: **configurable page size** (4/8/16/64 KiB; default 4 KiB), a **primary database file** plus **`{path}-wal`**, a versioned **file header** on page 0, and a redo WAL replayed on `Open`.

Normative byte layout and versioning live in **[`ENGINE_FORMAT.md`](./ENGINE_FORMAT.md)**. The **`hexxladb`** package at the module root delegates [`Open`](../../db.go) / [`Close`](../../db.go) here; domain and app code should depend only on the public API ([`HEXAGONAL_ARCHITECTURE.md`](../../docs/architecture/HEXAGONAL_ARCHITECTURE.md)).

The engine owns file locking, page I/O, the B+ tree ([`ORDERED_STORE.md`](./ORDERED_STORE.md)), compression, overflow pages, page caching, write transactions, group-WAL flushing, and recovery. `internal/index` supplies logical key families; the module-root package owns public `View`/`Update`, MVCC semantics, record orchestration, and changefeed finalization.
