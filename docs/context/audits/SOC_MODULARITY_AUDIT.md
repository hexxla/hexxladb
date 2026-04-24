# Separation of Concerns & Modularity Audit

**Date:** 2026-04-24
**Scope:** Full codebase — module root (`package hexxladb`), `internal/`, `cmd/`
**Method:** Direct source reading, not doc-only analysis

---

## Executive Summary

The architecture is **fundamentally sound** with a clean hexagonal skeleton and well-enforced inner
boundaries. The dominant concern is at the **root package boundary**: `package hexxladb` is doing
the work of three distinct layers simultaneously. Several secondary issues exist around an
under-utilized app layer, a leaking `mvccspike` package, and module-root file decomposition. None
of these are catastrophic — but together they represent the highest-value area for improvement.

**Verdict:** Strong infrastructure modularity, weak public-API-layer modularity.

---

## Layer Map (as observed in code)

```
cmd/hexxladb/main.go          ← composition root (correct)
        │
        ├── internal/app        ← use-case orchestrator (thin, partially stubbed)
        │       └── domain.Storage port (interface only)
        │
        ├── internal/adapters/out/hexxladb  ← outbound adapter (implements domain.Storage)
        │       └── calls only package hexxladb public API  (correct)
        │
package hexxladb (module root) ← PUBLIC API — also plays THREE internal roles:
        │   db.go / db_open.go  → lifecycle + wiring
        │   tx.go               → txn semantics
        │   primitives.go       → business-flavored CRUD
        │   cell_secondary.go   → secondary index management
        │   seam_secondary.go   → secondary index management
        │   mvcc.go             → MVCC read resolution
        │   mvcc_lifecycle.go   → MVCC prune lifecycle
        │   views.go            → read projection / budgeting layer
        │   facets_edges.go     → facet + edge CRUD
        │   rotation.go         → offline key-rotation utility
        │   encryption.go       → key derivation
        │   db_changelog.go     → changelog facade
        │   coord_export.go     → lattice re-exports
        │
        ├── internal/engine     ← B+ tree + WAL + group commit (correct boundary)
        ├── internal/index      ← key encoding/parsing (correct boundary)
        ├── internal/record     ← wire types + encode/decode (correct boundary)
        ├── internal/lattice    ← hex grid math (correct boundary)
        ├── internal/mvccspike  ← MVCC prototype (boundary issue — see §4)
        ├── internal/changelog  ← changefeed log (correct boundary)
        └── internal/config     ← env config (correct, simple)
```

---

## 1. What Works Well

### 1.1 Hexagonal boundary between adapters and internals
`internal/adapters/out/hexxladb/storage.go` imports **only** `github.com/hexxla/hexxladb` (the
public API) plus shared `internal/domain`, `internal/lattice`, and `internal/record`. It never
touches `internal/engine` or `internal/index`. The compile-time interface assertion
(`var _ domain.Storage = (*Storage)(nil)`) enforces this.

### 1.2 Engine is genuinely isolated
`internal/engine` has zero dependencies on domain, index, record, lattice, or app packages. It
knows only pages, WAL sequences, headers, and hooks. This is correct low-level isolation.

### 1.3 Index encoding is pure
`internal/index` contains only key construction and parsing functions with no file I/O, no state,
and no engine imports. It is effectively a pure function library over byte slices.

### 1.4 Record types are decoupled
`internal/record` holds wire structs plus encode/decode. It imports nothing from engine, index, or
app. The separation of `record.CellRecord` from btree keys is maintained throughout.

### 1.5 Composition root is clean
`cmd/hexxladb/main.go` correctly acts as the composition root: it wires `DB → Storage adapter →
app.Service` without leaking those dependencies into business logic.

### 1.6 `domain.Storage` port is well-designed
The interface in `internal/domain/storage.go` covers the full observable surface without exposing
engine types. Callers of `app.Service` never need to import `internal/engine`.

---

## 2. Primary Concern: `package hexxladb` Conflates Multiple Layers

This is the core finding. The module-root package simultaneously acts as:

| Role | Files | Problem |
|------|-------|---------|
| **Public stable API surface** | `db.go`, `tx.go`, `options.go`, `errors.go`, `coord_export.go` | Correct home |
| **Business-layer CRUD** | `primitives.go`, `facets_edges.go` | Mixed concern |
| **Secondary index maintenance** | `cell_secondary.go`, `seam_secondary.go` | Infrastructure leaked into API layer |
| **MVCC visibility resolution** | `mvcc.go` | Storage-engine protocol in public package |
| **MVCC lifecycle management** | `mvcc_lifecycle.go` | Pruning policy in public package |
| **Read projection / view assembly** | `views.go` | Application-layer logic in storage layer |
| **Offline key-rotation utility** | `rotation.go` | Operational tooling mixed with runtime API |
| **Encryption key derivation** | `encryption.go` | Infrastructure concern at API layer |

### 2.1 Secondary index management leaks engine coupling

`cell_secondary.go` and `seam_secondary.go` call `tx.db.btree.Delete(...)` directly, bypassing
the `Tx.Put`/`Tx.AscendRange` abstraction defined in `tx.go`. This means the **secondary index
maintenance layer reaches into engine internals** at the same package level as the public API.

```go
// cell_secondary.go:62 — direct btree access from index-maintenance code
if err := tx.db.btree.Delete(k); err != nil {
```

This coupling also appears in `mvcc_lifecycle.go` (pruning directly calls `db.btree.AscendRange`
and `db.btree.Delete`) and `primitives.go` (which calls `tx.db.btree.Put` for the commit-time
meta key in `tx.go`).

### 2.2 `views.go` belongs in a higher layer

`views.go` implements `CellView`, `ContextPack`, `LoadContextWithBudgeting`,
`TruncateCellViewsToTokenBudget`, and the `TokenBudgeter` interface. These are **application-
layer read-model / projection concerns** — they have no storage I/O of their own, they orchestrate
calls to primitives and add business-level token budgeting logic. They live in `package hexxladb`
which forces consumers who only want raw CRUD to pull in the entire view-assembly machinery.

This code belongs in `internal/app` or an `internal/views` package.

### 2.3 `mvcc.go` conflates visibility policy with the public API

The MVCC read-path functions (`getCellVisibleRaw`, `getSeamVisibleRaw`, `getFacetVisibleRaw`,
`visibleCellAndSeq`) are unexported helpers on `*Tx` in `package hexxladb`. They contain the
version-selection loop (`mvccspike.SelectVisible`) which is a storage-engine-level policy. These
helpers could live in an `internal/txcore` or `internal/mvcc` package and be called from the thin
public `Tx` methods.

### 2.4 `rotation.go` is an offline operational tool

`RotateEncryption` performs an offline copy of the entire database. It is entirely correct
functionally, but its home in the main package couples it to every consumer. Operational tools
like this are natural candidates for an `internal/tooling` or separate subpackage.

---

## 3. Secondary Concern: `internal/app` Is Severely Under-Used

`internal/app/app.go` defines `Service` but implements only **3 of the ~30 port methods** on
`domain.Storage`: `PutCell`, `AscendCellsByTag`, `AscendDistinctTags`, and `ListExistingTopics`.

The consequence: the hexagonal architecture's intent (business logic lives in app, orchestration
in adapters) is only partially realized. Most operations bypass `app.Service` entirely — callers
use the adapter or `Tx` methods directly.

The actual composition root (`cmd/hexxladb/main.go:91`) calls `runStorageSmoke` directly against
`domain.Storage`, not `app.Service`. The service is wired then discarded (`_ = svc`).

This is not a boundary violation, but the declared architecture promises more than the
implementation delivers. If `app.Service` is meant to be the use-case layer, the view assembly
and MVCC lifecycle logic in `package hexxladb` should be elevated there.

---

## 4. Tertiary Concern: `internal/mvccspike` Is a Live Experiment in Production Path

`mvccspike` declares itself a "Phase E1 MVCC storage experiment" in its `doc.go`, yet it is
imported in production code paths:

- `mvcc.go` (module root) imports `mvccspike.SelectVisible` and `mvccspike.VersionKV`
- `mvcc.go` imports `mvccspike` alongside `internal/index`

The concern is **naming and discoverability**: a package named `spike` signals transient prototype
work, but it carries production semantics (`SelectVisible` is the MVCC visibility algorithm). This
creates cognitive confusion about what is stable vs exploratory.

**Recommendation:** Rename `internal/mvccspike` → `internal/mvcc` (the `internal/mvcc/` directory
exists but is currently empty). Promote `SelectVisible` and `VersionKV` there. The `mvccspike`
name is a maintenance hazard.

---

## 5. Specific Modularity Issues by File

### `tx.go`

- `Tx.Put` has inline knowledge of MVCC cell key format:
  ```go
  // tx.go:231
  if tx.db.useMVCC && bytes.HasPrefix(key, []byte(index.CellPrefix)) {
      if _, _, err := index.ParseCellVersionKey(key); err != nil { ...
  ```
  This is index-format validation embedded in the generic Put method. It belongs in `primitives.go`
  or the MVCC path, not the low-level byte accessor.

- The group-WAL `db.mu.Unlock()` / `db.mu.Lock()` dance in `Update` (lines 170–172) is a subtle
  locking protocol embedded in the transaction method. A named helper or explicit state machine
  would make this safer to evolve.

### `primitives.go`

- `PutCell` manually inserts the `__meta/commit-time/` timeline key (line 150–152) before
  delegating to `index.CommitTimeKey`. This MVCC bookkeeping is a storage invariant, not a
  primitive API concern. It should be encapsulated in the MVCC layer.

- The function `putSeamWithOp` in `primitives.go` is 72 lines and handles both non-MVCC and MVCC
  paths with interleaved branching. The two paths could be separated to reduce cognitive load.

### `cell_secondary.go` / `seam_secondary.go`

These files are well-organized internally but their placement in `package hexxladb` rather than
`internal/` is the issue. They are not part of the public API (all functions are unexported), yet
they live in the public package namespace. They read as internal implementation detail that
escaped its boundary.

### `mvcc_lifecycle.go`

- `PruneCellVersions` directly manipulates `db.btree` using `BeginWriteTxn`/`CommitWriteTxn` at
  the engine level, bypassing the `DB.Update` callback contract. This is done intentionally (the
  comment explains the reason), but it means prune logic holds a different code path from all
  other mutations. A dedicated internal write path helper would make this more consistent.

- `MVCCPruneProfile` and its `switch` branches are duplicated between `MVCCPrunePlan` and
  `PruneCellVersionsByProfile`. The profile → maxDelete mapping is literally copy-pasted.

### `views.go`

- `LoadContextWithBudgeting` uses a `goto assembled` to break out of a nested loop (line 237).
  This is valid Go, but structuring this as a helper that returns early would be cleaner and
  easier to test in isolation.

- `TokenBudgeter` interface and `ByteLenBudgeter` are defined in `package hexxladb`. These have
  no storage dependency — they operate purely on string content. They belong in an application
  or utility package.

---

## 6. Import Dependency Violations

No hard boundary violations found in the critical paths. Specifically confirmed clean:

- `internal/domain` does **not** import `internal/engine` or `internal/index` ✓
- `internal/app` does **not** import `internal/engine` or `internal/index` ✓
- `internal/adapters/out/hexxladb` does **not** import `internal/engine` ✓
- `internal/engine` imports nothing from `internal/domain`, `internal/app`, or `internal/record` ✓

The only concern is **implicit coupling through the module-root package**: because `package
hexxladb` has access to `internal/engine` (it directly holds `*engine.Engine` and `*engine.BTree`
on `DB`), and `domain.Storage` implementors are forced to go through `package hexxladb`, the
boundary is upheld structurally but not enforced by Go's type system or import graph alone.

---

## 7. Ranked Recommendations

| Priority | Issue | Recommendation |
|----------|-------|----------------|
| **High** | `mvccspike` in production path | Rename to `internal/mvcc`, promote `SelectVisible`/`VersionKV` as stable |
| **High** | Secondary index files in public package | Move `cell_secondary.go`, `seam_secondary.go` unexported logic to `internal/txcore` or `internal/storage` |
| **High** | `views.go` in public package | Move view types + budgeting to `internal/app` or `internal/views`; re-export only types from module root |
| **Medium** | `app.Service` stub | Implement remaining `domain.Storage` delegations; move view assembly into service use-cases |
| **Medium** | MVCC prune profile duplication | Extract `profileToMaxDelete(MVCCPruneProfile) (int, error)` helper, use in both sites |
| **Medium** | Commit-time meta-key insertion in `primitives.go` | Encapsulate in MVCC initialization, not in `PutCell` caller |
| **Low** | `rotation.go` in module root | Move to `internal/tooling/rotation` or a `rotatedb` subcommand under `cmd/` |
| **Low** | `goto assembled` in `LoadContextWithBudgeting` | Refactor to a named helper function returning `([]scored, error)` |
| **Low** | MVCC key validation in `Tx.Put` | Push format-specific guard into the primitives that call it |

---

## 8. What Not to Change

- **Do not** split `internal/engine` further. The B+ tree and WAL are correctly collocated; page
  management and tree logic are inherently coupled.
- **Do not** extract `internal/index` into a separate module. It is correctly shared across the
  module root and adapters without any risk.
- **Do not** move `record` types out of `internal/record`. They are correctly shared across all
  layers without creating circular dependencies.
- **Do not** add more layers to `cmd/hexxladb/main.go`. It is correctly minimal.

---

## 9. Summary Scorecard

| Dimension | Score | Notes |
|-----------|-------|-------|
| Engine isolation | ✅ Strong | No leakage out of `internal/engine` |
| Index encoding isolation | ✅ Strong | Pure functions, no I/O |
| Record type isolation | ✅ Strong | Clean wire layer |
| Hex grid isolation | ✅ Strong | `internal/lattice` has no upstream deps |
| Hexagonal adapter boundary | ✅ Strong | Adapter → public API only |
| Composition root | ✅ Clean | `cmd/` is minimal |
| Domain port design | ✅ Good | Interface covers surface correctly |
| Public API layer | ⚠️ Mixed | Conflates API, index maintenance, MVCC, views |
| App service utilization | ⚠️ Weak | Declared but mostly unused use-case layer |
| Experimental package naming | ❌ Risk | `mvccspike` is production code named as a spike |
| Prune logic consistency | ⚠️ Minor | Direct btree access + duplicated profile logic |
