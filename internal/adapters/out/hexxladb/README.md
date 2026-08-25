# Outbound adapter: HexxlaDB

Implement **`internal/domain`** ports by calling **only** the public **`hexxladb`** package at the module root ([doc.go](../../../../doc.go)) — not `internal/engine` or `internal/index` (see [HEXAGONAL_ARCHITECTURE.md](../../../../docs/architecture/HEXAGONAL_ARCHITECTURE.md)).

[`storage.go`](./storage.go) — package **`hexxladbout`**, [`NewStorage`](./storage.go) implements [`domain.Storage`](../../../../internal/domain/storage.go) over [`*hexxladb.DB`](../../../../db.go).
