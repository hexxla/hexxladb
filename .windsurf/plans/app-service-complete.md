---
description: Complete app.Service use-case layer — all domain.Storage delegations
---

## Goal

`internal/app/app.go` currently delegates only 4 of ~22 `domain.Storage` port methods.
Complete the remaining delegations so `app.Service` is a full facade over the outbound port.
Also wire the service properly in `cmd/hexxladb/main.go` (remove `_ = svc`).

## Methods to add to app.go

From `domain.Storage` interface, the unimplemented delegations:

| Method | Notes |
|---|---|
| `GetCell` | |
| `AscendCellsBySource` | |
| `AscendCellsInTimeBucket` | |
| `WalkRing` | |
| `WalkRingAt` | |
| `FindSeams` | |
| `FindSeamsAt` | |
| `LoadContext` | |
| `LoadContextAt` | |
| `WalkRingFacets` | |
| `ResolveSeam` | |
| `PutSeam` | |
| `MarkConflict` | |
| `AscendSeamsBySource` | |
| `AscendSeamsInTimeBucket` | |
| `PutFacet` | |
| `UpdateFacet` | |
| `GetFacet` | |
| `AscendFacetsForCell` | |
| `PutEdge` | |
| `LinkCells` | |
| `GetEdge` | |
| `AscendEdgesFrom` | |

## Files

| File | Change |
|---|---|
| `internal/app/app.go` | Add all remaining delegations |
| `internal/app/app_test.go` | Extend tests: ErrNoStorage for each new method |
| `cmd/hexxladb/main.go` | Remove `_ = svc`; use service or comment why it's unused |
| `docs/hexxladb/API_REFERENCE.md` | Note app.Service completion |
| `CHANGELOG.md` | Unreleased entry |
| `TODOS.md` | Mark complete |
| `docs/ROADMAP.md` | Move to Completed |

## Pattern

Every method is identical: nil-guard, then delegate:

```go
func (s *Service) Foo(ctx context.Context, ...) (..., error) {
    if s == nil || s.Storage == nil {
        return ..., ErrNoStorage
    }
    return s.Storage.Foo(ctx, ...)
}
```

## cmd/main.go

Check whether `_ = svc` is still there and how the service is used.
If unused: keep construction but add a clear comment; or wire it into a health check call.
