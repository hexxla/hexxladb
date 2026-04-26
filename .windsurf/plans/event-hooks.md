---
description: Event Hooks/Callbacks — post-write reactions on cell writes and seam detection
---

## Goal

Allow callers to react to successful writes without polling. Hooks are called synchronously
after the write is committed inside the same `Update` transaction callback. Non-nil errors
from hooks do NOT abort the write (write is already complete); errors are surfaced as a
secondary return value pattern — but for simplicity, hooks are fire-and-forget (logged
internally, not returned) unless we expose a hook that returns error (see design below).

## Design decision

Two patterns are common:
- **Fire-and-forget**: hook called, error ignored. Simple; good for metrics/logging.
- **Error-returning**: hook can fail and the error surfaces. Useful for side-effects like
  changefeed fanout, confidence decay triggers, etc.

**Decision**: hooks return `error`. The error is returned from `PutCell`/`PutSeam` after
the write succeeds. This makes hooks composable and testable. Callers that want fire-and-
forget wrap in a func that logs and returns nil.

This is consistent with how `CellValidator` works (pre-write, error-returning) and follows
the hexagonal architecture: hook interfaces are defined at package root, implementations
live anywhere.

## API surface (hooks.go)

```go
// AfterPutCellHook is called after a cell write commits inside the current Update callback.
// The hook receives the written record. A non-nil error is returned from PutCell.
type AfterPutCellHook interface {
    AfterPutCell(ctx context.Context, rec record.CellRecord) error
}

// AfterPutCellHookFunc adapts a plain function to AfterPutCellHook.
type AfterPutCellHookFunc func(ctx context.Context, rec record.CellRecord) error

// AfterPutSeamHook is called after a seam write commits inside the current Update callback.
type AfterPutSeamHook interface {
    AfterPutSeam(ctx context.Context, rec record.SeamRecord) error
}

// AfterPutSeamHookFunc adapts a plain function to AfterPutSeamHook.
type AfterPutSeamHookFunc func(ctx context.Context, rec record.SeamRecord) error
```

## Options wiring (options.go)

```go
// AfterPutCell optional post-write hook called on every successful PutCell.
AfterPutCell AfterPutCellHook

// AfterPutSeam optional post-write hook called on every successful PutSeam.
AfterPutSeam AfterPutSeamHook
```

## DB wiring (db.go)

Store on DB struct alongside cellValidator:
```go
afterPutCell AfterPutCellHook
afterPutSeam AfterPutSeamHook
```

## Call sites

- `primitives.go` `Tx.PutCell`: after successful encode+put, call `tx.db.afterPutCell` if non-nil
- `primitives.go` `Tx.PutSeam`: after successful encode+put, call `tx.db.afterPutSeam` if non-nil

## Files

| File | Change |
|---|---|
| `hooks.go` | `AfterPutCellHook`, `AfterPutCellHookFunc`, `AfterPutSeamHook`, `AfterPutSeamHookFunc` |
| `options.go` | `AfterPutCell AfterPutCellHook`, `AfterPutSeam AfterPutSeamHook` fields |
| `db.go` | `afterPutCell`, `afterPutSeam` fields; wire from `opts` in `Open` |
| `primitives.go` | Call hooks after write in `PutCell` and `PutSeam` |
| `hooks_test.go` | Tests: hook called on put, hook error surfaced, nil hook no-ops, seam hook |
| `docs/hexxladb/API_REFERENCE.md` | New "Event Hooks" section |
| `CHANGELOG.md` | Unreleased entry |
| `TODOS.md` | Mark complete |
| `docs/ROADMAP.md` | Move to Completed |

## Hexagonal compliance

- Hook interfaces defined at package root (no `internal/` imports from callers)
- Hook implementations can live anywhere (adapters, cmd, tests)
- No changes to `internal/domain`, `internal/app`, or `internal/adapters`
- `DB` struct stores hook as interface — zero overhead when nil
