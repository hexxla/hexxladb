# Contradiction workflow

This reference application records two conflicting observations, links them
with a seam, and saves an operator's resolution note.

```bash
go run ./examples/ports_adapters
go test ./examples/ports_adapters/...
```

The demo opens and removes its own temporary MVCC database. Its entry point
constructs the [service](internal/app/app.go) and [HexxlaDB adapter](internal/adapters/out/hexxladb/storage.go).
The service validates positions, text, and resolution notes using
[application types and a two-operation port](internal/domain/storage.go).
The adapter translates those types and uses one transaction to record both
cells and their seam. The real-adapter test injects a rejected seam write and
checks that both cells are rolled back. Resolving a contradiction is a later,
separate transaction.
