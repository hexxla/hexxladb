# Adapters

**Outbound:** **[`internal/adapters/out/hexxladb`](./out/hexxladb)** implements **[`domain.Storage`](../domain/storage.go)** over **`package hexxladb`**. Its package and tests are the adapter example; the operator CLIs do not own this boundary.

**Inbound:** This module **does not** ship HTTP, gRPC, health checks, or other production transports. Services that use HexxlaDB add their own **`adapters/in`** (or equivalent) **in their repo** and wire **`internal/app`** / **`domain`** per **[`docs/architecture/HEXAGONAL_ARCHITECTURE.md`](../../docs/architecture/HEXAGONAL_ARCHITECTURE.md)**.

**`internal/domain`** and **`internal/app`** define ports. **`cmd/hexxladb`** and **`cmd/tui`** are operator binaries/composition roots; reusable embedding examples live under **`examples/`**.
