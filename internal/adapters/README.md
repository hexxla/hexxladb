# Adapters (placeholder)

Inbound (**`in/`**) and outbound (**`out/`**) adapters are added here when you integrate transports (HTTP, gRPC, CLI) or infrastructure (databases, queues).

**`internal/domain`** and **`internal/app`** define ports; **`cmd/...`** constructs and injects implementations. See **[`docs/context/HEXAGONAL_ARCHITECTURE.md`](../../docs/context/HEXAGONAL_ARCHITECTURE.md)**.

There are no adapter packages in this tree yet; removing empty Go packages avoids unused import noise.
