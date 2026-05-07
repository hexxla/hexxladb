# Core Hexagonal Architecture Rules

This project uses Hexagonal Architecture. All dependencies point **inward** toward the domain. Violating these rules breaks the architecture, creates circular dependencies, and makes the codebase untestable.

Full layout and dependency table: **`docs/context/HEXAGONAL_ARCHITECTURE.md`** (source of truth).

<rules>
- `internal/domain` and `internal/app` must have zero imports from `internal/adapters/...`
- `internal/domain` and `internal/app` must not import `internal/engine` or `internal/index`
- Port interfaces belong in `internal/domain`, `internal/app`, or `internal/port` — not in adapters
- Persistence and key encoding belong in `package hexxladb` (module root) and `internal/record` / `internal/lattice`
- Outbound adapters implement ports in `internal/adapters/out/...` calling only the public `hexxladb` API
- `cmd/.../main.go` is composition root only — no business rules
</rules>

<violations>
<violation>BAD: `internal/domain/foo.go` imports `internal/adapters/out/boltdb` — domain must never know about persistence</violation>
<violation>BAD: `internal/app/service.go` imports `internal/engine` — engine is below the adapter boundary</violation>
<violation>GOOD: `internal/adapters/out/hexxlastore/repo.go` implements a port interface from `internal/app` by calling `hexxladb.Open`</violation>
</violations>
