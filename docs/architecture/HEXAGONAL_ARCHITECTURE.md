# HexxlaDB architecture

This document is the canonical package-boundary contract for HexxlaDB. Read it before adding or moving code under `internal/` or `cmd/`.

HexxlaDB is primarily an embedded Go library. Its stable public API is the module-root package:

```go
import "github.com/hexxla/hexxladb"
```

The command packages are operator tools and examples of composition. They are not the public API.

## System shape

HexxlaDB has two related dependency paths:

```text
Embedding application
  -> package hexxladb
     -> internal/engine, internal/index, internal/record,
        internal/lattice, internal/hnsw, internal/mvcc, ...

Hexxla application service
  -> internal/app -> internal/domain ports
  -> internal/adapters/out/hexxladb
  -> package hexxladb
```

The first path is the normal library call path. The second demonstrates ports and adapters for application-level use cases without exposing storage internals.

## Package responsibilities

| Location                                                                | Responsibility                                                                                                                                                           |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Module-root `package hexxladb`                                          | Stable public API, transaction orchestration, and coordination of private persistence components. Exported signatures must not require callers to import `internal/...`. |
| `internal/engine`                                                       | Pages, B+ tree, WAL, file locking, durability, and engine-level transactions.                                                                                            |
| `internal/index`                                                        | Physical and logical key encoding. It does not own application policy.                                                                                                   |
| `internal/record`                                                       | Persistent record encodings and validation of wire-level structure.                                                                                                      |
| `internal/lattice`                                                      | Pure hex geometry and coordinate packing. Selected stable types and functions are re-exported at the module root.                                                        |
| `internal/hnsw`, `internal/mvcc`, `internal/pathfind`, `internal/views` | Cohesive private algorithms used by the public package.                                                                                                                  |
| `internal/domain`                                                       | Application-level rules and outbound ports. It is independent of persistence implementations.                                                                            |
| `internal/app`                                                          | Application use-case orchestration over domain-owned or app-owned ports.                                                                                                 |
| `internal/adapters/out/hexxladb`                                        | Implements application ports by calling only the public `hexxladb` API.                                                                                                  |
| `internal/adapters/in`                                                  | Documentation placeholder. Production inbound transports belong in applications embedding HexxlaDB.                                                                      |
| `cmd/hexxladb`, `cmd/tui`                                               | Operator binaries and composition roots. They may construct concrete dependencies but must not own reusable business rules.                                              |

## Required dependency rules

1. `internal/domain` and `internal/app` define the ports they consume. They must not import `internal/adapters/...`.
2. Port signatures must not mention adapter implementation types.
3. `internal/domain` and `internal/app` must not import `internal/engine` or `internal/index`.
4. Outbound adapters implement ports by using the public module-root API, not private engine or key-encoding packages.
5. Persistence orchestration and public transaction methods remain in `package hexxladb`; page mechanics stay in `internal/engine`; reusable key encodings stay in `internal/index`.
6. `internal/domain` contains business rules, not file, environment, logging, or transport concerns.
7. `cmd/...` parses command input, constructs dependencies, runs them, and maps errors to process output. Reusable behavior belongs in the library or application packages.

These rules are enforced by Go's `internal` visibility, `depguard` configuration, and [`scripts/check-hex-boundaries.sh`](../../scripts/check-hex-boundaries.sh).

## Choosing where code belongs

Use the smallest existing boundary that owns the behavior:

| Change                                               | Home                                                             |
| ---------------------------------------------------- | ---------------------------------------------------------------- |
| New stable database operation or caller-visible type | Module-root `package hexxladb`                                   |
| Page, WAL, locking, or B+ tree mechanics             | `internal/engine`                                                |
| Key encoding or index-family mechanics               | `internal/index`                                                 |
| Record serialization                                 | `internal/record`                                                |
| Pure coordinate or lattice algorithm                 | `internal/lattice` or another cohesive private algorithm package |
| Application policy or use case                       | `internal/domain` / `internal/app`                               |
| Implementation of an application I/O port            | `internal/adapters/out/...`                                      |
| Operator CLI or TUI wiring                           | `cmd/...`                                                        |

Do not add a port, adapter, package, or facade merely to wrap a pure helper. Add a boundary when it separates an existing responsibility, dependency, or substitution point.

## Public API boundary

The module root is intentionally the public package; HexxlaDB does not use a `pkg/` directory. Every exported symbol is a compatibility commitment governed by [`VERSIONING.md`](../../VERSIONING.md).

Public signatures may use standard-library types and exported root-package types. When a private type must be exposed, provide a deliberate root-level alias or public representation, as done in [`export.go`](../../export.go). Do not leak an `internal/...` import requirement to consumers.

## Common change paths

### Add database behavior

1. Define the caller-visible behavior in the root package.
2. Put private storage, encoding, or algorithm work in the package that owns it.
3. Preserve transaction, MVCC, changelog, and error semantics at the public boundary.
4. Test the narrow private behavior and the root-level observable behavior.
5. Update the owning reference document from the documentation map in [`AGENTS.md`](../../AGENTS.md).

### Add an application use case

1. Put business invariants in `internal/domain`.
2. Define the required port beside its consumer in `internal/domain` or `internal/app`.
3. Orchestrate the use case in `internal/app`.
4. Implement external I/O in an adapter. The HexxlaDB storage adapter calls only the public root package.
5. Wire concrete implementations in a `cmd/...` composition root or in the embedding application.

Use [`internal/domain/storagecontract`](../../internal/domain/storagecontract) for reusable behavioral contracts shared by storage adapters.

## Verification

Run the narrowest relevant tests while developing, then run:

```bash
bash scripts/check-hex-boundaries.sh
task ci
```

Architecture changes are complete only when dependency checks pass, public signatures remain usable without `internal/...`, and the documentation describing the affected boundary is current.

## Related documentation

- [`API_REFERENCE.md`](../hexxladb/API_REFERENCE.md) — curated public API guide.
- [`HEXXLA_DB.md`](../hexxladb/HEXXLA_DB.md) — storage families and key layout.
- [`TX.md`](../hexxladb/TX.md) — transaction and MVCC semantics.
- [`CONTRIBUTING.md`](../../CONTRIBUTING.md) — contributor workflow and checks.
