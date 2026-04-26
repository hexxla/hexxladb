---
description: Near-term remaining items — priority order (easiest first)
---

# Near-term Remaining Items

All four remaining Near-term items share a common characteristic: they are
**structural / architectural** rather than additive. Ordered by ascending risk
and effort below.

---

## Priority 1 — Per-database MaxValueBytes (Medium effort, self-contained)

**What:** Expose a per-database configurable value-size ceiling, stored in the
engine header so it persists across reopens. Default = 8192 (current hard-coded
`maxValBytes`). Allow 512/1024/2048/4096/8192/16384 bytes at `Open` time.

**Why first:** Engine-only change; no import cycle, no interface extraction, no
cross-package structural work. Pure data-path + header addition.

**Design:**
- Add `MaxValueBytes uint32` field to `engine.Header` at a free byte offset
  in the 512-byte header prefix (bytes 100–107 currently unused — verify in
  `internal/engine/header.go`).
- Bump to `formatVersionV3` for new databases that set a non-default limit.
  Existing v1/v2 files: treat absent field as 8192 (backward-compatible read).
- `engine.Options.MaxValueBytes` wires through to `btree_page.go` validation.
- `hexxladb.Options.MaxValueBytes uint32` → passed to engine opts at `Open`.
- `engine.BTree.Put` reads the limit from `engine.Options` (or stores it on
  `BTree` at construction); enforces `ErrValueTooLarge` when exceeded.
- No migration needed: existing rows were written under old limit; only new
  writes are validated against the new ceiling.

**Files:**
| File | Change |
|---|---|
| `internal/engine/header.go` | Add `MaxValueBytes uint32` field; encode/decode at offset ~100; default 8192 when zero |
| `internal/engine/const.go` | Add `formatVersionV3`; update `decodeHeaderPage` to accept v3 |
| `internal/engine/btree_page.go` | Replace hardcoded `maxValBytes = 8192` with instance field or func |
| `internal/engine/btree.go` | Thread limit through `Put`; read from options stored on `BTree` |
| `internal/engine/options.go` | Add `MaxValueBytes uint32` |
| `options.go` | Add `MaxValueBytes uint32` |
| `db.go` | Pass `opts.MaxValueBytes` to engine opts |
| `errors.go` | No new errors needed (`ErrValueTooLarge` already exists in engine) |
| `docs/hexxladb/API_REFERENCE.md` | Update Storage Limits section |
| Tests | Engine-level + DB-level: verify limit enforced, default preserved, v1/v2 reopen OK |

**Risk:** Low-medium. Header format change is the riskiest part — must verify
no byte collisions with existing fields. formatVersionV3 path needs careful
round-trip testing.

---

## Priority 2 — Extract `views.go` to `internal/views` (Medium-high effort)

**What:** Move `views.go` (and `views_test.go`) into `internal/views`, breaking
the import cycle that currently prevents this.

**Root cause of cycle:**
```
hexxladb (root) → internal/views (for CellView types)
internal/views  → hexxladb (root) (for *Tx methods: GetCell, AscendFacetsForCell,
                                    AscendEdgesFrom, FindSeams, resolveSupersession)
```

**Solution — interface extraction:**
Define a narrow `TxReader` interface in `internal/views` (or a shared
`internal/ports` package) that captures only the methods `views.go` calls on
`*Tx`. The root package's `*Tx` satisfies this interface implicitly (Go
structural typing). `internal/views` functions accept `TxReader`, not `*Tx`.

```go
// internal/views/tx_reader.go
type TxReader interface {
    GetCell(key lattice.PackedCoord) (record.CellRecord, bool, error)
    AscendFacetsForCell(key lattice.PackedCoord, fn func(record.FacetRecord) bool) error
    AscendEdgesFrom(key lattice.PackedCoord, fn func(record.EdgeRecord) bool) error
    FindSeams(ctx context.Context, coord lattice.Coord, radius int, unresolvedOnly bool) ([]record.SeamRecord, error)
}
```

- `AssembleCellView`, `LoadContextWithBudgeting`, etc. take `TxReader` instead of `*Tx`.
- Root package `views.go` becomes thin wrappers: `func (tx *Tx) AssembleCellView(...) { return views.AssembleCellView(tx, ...) }`.
- `resolveSupersession` uses `GetCell` + `FindSeams` — moves to `internal/views`.
- `CellView`, `ContextPack`, `AssembleCellViewOpts`, etc. types move to `internal/views`.
- Root package re-exports them as type aliases: `type CellView = views.CellView`.

**Files:** `views.go`, `views_test.go`, new `internal/views/` package,
`internal/views/tx_reader.go`, type aliases in root `views_aliases.go`.

**Risk:** Medium-high. Touches many callers of `CellView` types. Type aliases
preserve backward compatibility. `resolveSupersession` is internal so no
public API break. Needs careful review of all `internal/views` imports.

---

## Priority 3 — Relocate secondary index files to `internal/` (Medium-high effort)

**What:** Move `cell_secondary.go` and `seam_secondary.go` to
`internal/adapters/` or `internal/storage/` — these are implementation details,
not public API.

**Root cause of cycle:** Same as views — they are methods on `*Tx` and use
`tx.putDirect`/`tx.deleteDirect`, so moving requires the same `TxWriter`
interface extraction.

**Dependency on Priority 2:** If the `TxReader` interface from Priority 2 is
already extracted, adding `TxWriter` (with `putDirect`, `deleteDirect`,
`requireWritable`) is incremental. However `putDirect` is intentionally
unexported — would need to be promoted to an internal interface method or the
secondary index logic restructured as free functions accepting explicit btree
handles.

**Alternative approach — free functions:**
Instead of methods on `*Tx`, secondary index helpers become free functions
taking explicit `(bt *engine.BTree, rec record.CellRecord, ...)`. The `*Tx`
methods delegate. This avoids needing `TxWriter` and decouples from `*Tx`
entirely. The secondary index package only imports `internal/engine`,
`internal/index`, `internal/record` — no root package import → no cycle.

**Risk:** Medium-high. Requires restructuring method receivers. Secondary index
tests will need updating.

---

## Priority 4 — Move `rotation.go` to `internal/tooling/rotation` (High effort)

**What:** Move key rotation logic (`RotateEncryption`) to an internal tooling
package.

**Root cause of cycle:** `rotation.go` uses:
- `DB.Open` (root package)
- `Tx.putDirect` (unexported method on root `*Tx`)
- `Tx.AscendRange` (exported)
- `ErrValueTooLarge`, `ErrKeyTooLarge` (root package sentinels)
- Engine options (`buildEngineOptions`)

This is the hardest cycle to break because `rotation.go` needs to construct
`*DB` (calls `Open`) and access `putDirect` — it fundamentally depends on the
root package's private API.

**Options:**
1. Keep `rotation.go` in root package (no move) — current state is fine; it's
   not visible in `internal/`, just co-located with the DB API.
2. Extract rotation to `cmd/rotation/` — tool-only, not a package import.
3. Expose a `DB.copyRawTo(dst *DB)` method and implement rotation in terms of
   public API only. `putDirect` would be replaced with `Tx.Put` (with MVCC
   guard relaxed or a separate `UnsafePut` internal escape).

**Recommendation:** Defer. The risk-to-benefit ratio is poor — `rotation.go`
in the root package is not architecturally wrong (it's a DB method). The SoC
audit flagged it as a "nice to have". Reclassify to Future if no strong need.

---

## Recommended execution order

1. **MaxValueBytes** — start here; isolated, no cycle work needed
2. **views.go extraction** — biggest user-visible structural gain; unblocks many future items
3. **Secondary index relocation** — incremental after #2 (reuse TxReader/TxWriter patterns)
4. **rotation.go** — reassess; may be reclassified to Future
