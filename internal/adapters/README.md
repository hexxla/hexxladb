# Adapters

**Outbound:** **[`internal/adapters/out/hexxladb`](./out/hexxladb)** implements **[`domain.Storage`](../domain/storage.go)** over **`package hexxladb`** (see **`cmd/hexxladb`**).

**Inbound:** This module **does not** ship HTTP, gRPC, health checks, or other production transports. Services that use HexxlaDB add their own **`adapters/in`** (or equivalent) **in their repo** and wire **`internal/app`** / **`domain`** per **[`docs/hexxladb/HEXXLA_PRODUCT_WIRING.md`](../../docs/hexxladb/HEXXLA_PRODUCT_WIRING.md)** and **[`docs/context/HEXAGONAL_ARCHITECTURE.md`](../../docs/context/HEXAGONAL_ARCHITECTURE.md)**.

**`internal/domain`** and **`internal/app`** define ports; **`cmd/...`** in this repo is for embedding demos and composition examples only.
