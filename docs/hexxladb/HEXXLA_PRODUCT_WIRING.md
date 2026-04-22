# Hexxla product layer wiring (orchestration, transports, dashboard)

**Audience:** Engineers building **applications** that import **`package hexxladb`**—not required for embedding the library alone.

**Scope:** This repository ships the **embedded database** (`package hexxladb`, `internal/engine`, …). It does **not** ship **HTTP/JSON APIs**, **observability endpoints**, or **production operations**—implement those in **your service binary**. [HEXXLA.md](./HEXXLA.md) describes HTTP/JSON tools, automatic seam thresholds, seed selection, and dashboards at the **product** level; those are **not** inside `internal/engine` or unconditional in [`PutCell`](../../primitives.go).

## Hexagonal placement

| Concern | Package / location |
|---------|-------------------|
| Domain rules (pure validation, coordination math) | [`internal/domain`](../../internal/domain) ([`Storage`](../../internal/domain/storage.go)); glossary [HEXAGONAL_ARCHITECTURE.md](../context/HEXAGONAL_ARCHITECTURE.md) |
| Use cases (orchestrate PutCell + PutSeam + policies) | [`internal/app`](../../internal/app) |
| Outbound persistence port | [`domain.Storage`](../../internal/domain/storage.go) → implement with [`internal/adapters/out/hexxladb`](../../internal/adapters/out/hexxladb) |
| Inbound HTTP, gRPC, CLI, queue | **Consumer service repo** — not packaged in **hexxladb**; pattern only (see [HEXAGONAL_ARCHITECTURE.md](../context/HEXAGONAL_ARCHITECTURE.md)) |
| Wire **only** `package hexxladb` at the storage adapter | Per adapter rule—no `internal/engine` imports from adapters |

**Composition root:** Your **service** `main` constructs **`Storage`**, **`app`**, and **inbound** adapters. [`cmd/hexxladb`](../../cmd/hexxladb) here is a **demo / embedding reference**, not a production API server—see [HEXAGONAL_ARCHITECTURE.md](../context/HEXAGONAL_ARCHITECTURE.md).

## Spec mapping

- **Filters / ranking / token policy** beyond [`LoadContextWithBudgeting`](../../views.go): implement in **app** using [`HEXXLA_LIBRARY_MAPPING.md](./HEXXLA_LIBRARY_MAPPING.md) recipes.
- **Auto seam on write:** wrap **`Update`** + conditional [`PutSeam`](../../primitives.go) / [`MarkConflict`](../../primitives.go) in a use case.
- **HTTP/JSON API:** implement in **your** service’s inbound adapters; call **app** with **domain** types—not adapter-specific structs on **`domain.Storage`**.

## Related

- [HEXXLA_LIBRARY_MAPPING.md](./HEXXLA_LIBRARY_MAPPING.md) — layer A/B/C table.
- [MODERN_GO.md](../context/MODERN_GO.md) — Go version and stdlib evolution reference.
