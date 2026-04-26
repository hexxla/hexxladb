---
description: Architectural housekeeping — views.go extraction, secondary index relocation, rotation.go decision
---

# Architectural Housekeeping

Branch: `feat/arch-housekeeping`

**MaxValueBytes is already shipped.** This plan covers the three remaining
near-term items in dependency order.

---

## Status

| # | Item | Status |
|---|---|---|
| 1 | Extract `views.go` → `internal/views` | pending |
| 2 | Relocate `cell_secondary.go` / `seam_secondary.go` → `internal/` | pending |
| 3 | `rotation.go` — decision + action | pending |

---

## Item 1 — Extract `views.go` to `internal/views`

**Why:** `views.go` (566 lines) lives in `package hexxladb` but contains
pure view-assembly logic that belongs below the public API boundary. Moving
it to `internal/views` gives it a test-friendly, import-controlled home.

**Import cycle root cause:**
```
hexxladb (root)  →  internal/views  (CellView types)
internal/views   →  hexxladb (root) (*Tx methods: GetCell, AscendFacetsForCell,
                                      AscendEdgesFrom, FindSeams)
```

**Solution — narrow `TxReader` interface:**

Define in `internal/views/tx_reader.go`:
```go
type TxReader interface {
    GetCell(key lattice.PackedCoord) (record.CellRecord, bool, error)
    AscendFacetsForCell(key lattice.PackedCoord, fn func(record.FacetRecord) bool) error
    AscendEdgesFrom(key lattice.PackedCoord, fn func(record.EdgeRecord) bool) error
    FindSeams(ctx context.Context, coord lattice.Coord, radius int, unresolvedOnly bool) ([]record.SeamRecord, error)
}
```

`*Tx` satisfies this implicitly (structural typing — no root→internal/views import).

**Changes:**
| File | Action |
|---|---|
| `internal/views/` | New package — all types + logic from `views.go` |
| `internal/views/tx_reader.go` | `TxReader` interface |
| `internal/views/views.go` | All types (`CellView`, `ContextPack`, etc.) + free functions accepting `TxReader` |
| `internal/views/budget.go` | `LoadContextWithBudgeting`, `collectCandidates`, `resolveSupersession` |
| `views.go` (root) | Replaced with thin `*Tx` method wrappers + type aliases (`type CellView = views.CellView`) |
| `views_test.go` (root) | Move to `internal/views/views_test.go`; update package |
| All callers (`query_exec.go`, `search.go`, `multi_context.go`, etc.) | Update imports if referencing moved types |

**API compatibility:** Type aliases preserve all existing call sites. No
semver break.

**Risk:** Medium. Many files reference `CellView`, `ContextPack`, etc.
Type aliases guarantee zero call-site changes in consuming code.

---

## Item 2 — Relocate secondary index files to `internal/`

**Depends on:** Item 1 (TxReader pattern established).

**What:** `cell_secondary.go` and `seam_secondary.go` are `*Tx` methods that
manage secondary index writes/reads. They are not public API — they should
not live in `package hexxladb`.

**Approach — free functions with explicit BTree handle:**

Secondary index helpers become free functions in `internal/secondary/`:
```go
// internal/secondary/cell.go
func PutCellIndex(bt BTreeWriter, rec record.CellRecord, commitSeq uint64, useMVCC bool) error
func RemoveCellIndex(bt BTreeWriter, rec record.CellRecord, commitSeq uint64, useMVCC bool) error
func AscendCellsBySource(bt BTreeReader, ...) error
// etc.
```

Where `BTreeWriter`/`BTreeReader` are narrow interfaces over `engine.BTree`
(or `*Tx`'s `putDirect`/`deleteDirect`/`AscendRange`). The root `*Tx`
methods become one-line delegates.

**Alternative (simpler):** Keep as `*Tx` methods but move the files into
`internal/` using a `//go:build` trick — not valid Go. The only real option
is interface extraction or acceptance that these stay at root.

**Pragmatic reassessment:** `cell_secondary.go` and `seam_secondary.go`
contain **no exported symbols** — only unexported helpers called by `*Tx`.
Moving them to a sub-package requires either promoting internal state
(`tx.db.useMVCC`, `tx.db.btree`) behind an interface or restructuring
significantly. The btree coupling is already fixed (`deleteDirect`). These
files are effectively **private implementation files** of `package hexxladb`.
The SoC concern is cosmetic — they don't violate any import rule.

**Decision point:** Evaluate during Item 1 work whether the `TxReader`
pattern naturally extends to a `TxIndexWriter` that makes this move clean.
If not, reclassify to Future.

---

## Item 3 — `rotation.go` decision

**Current state:** `rotation.go` is in `package hexxladb` (root). It uses:
- `DB.Open` (root)
- `Tx.putDirect` (unexported)
- `Tx.AscendRange` (exported)
- Root error sentinels

**Recommendation from prior audit:** Defer. Moving to `internal/` creates a
circular dependency that requires significant restructuring for minimal gain.
`rotation.go` in the root package is not architecturally wrong — it is a DB
utility that belongs with the DB API.

**Action:** Formally reclassify from Near-term to Future in ROADMAP.md.
Document the rationale in a brief comment in `rotation.go` if not already
present.

---

## Execution order

1. **Item 1** — `views.go` extraction (most structural value; unblocks Item 2 patterns)
2. **Item 2** — Secondary index reassessment after Item 1
3. **Item 3** — Reclassify `rotation.go` to Future in ROADMAP (no code change needed)

## Definition of done

- [ ] `internal/views` package created; all view types + logic moved
- [ ] Root `views.go` is thin wrappers + type aliases only
- [ ] `go test ./... -race` passes
- [ ] `golangci-lint run` passes (no new import cycle violations)
- [ ] Secondary index files: clean decision documented (move or reclassify)
- [ ] `rotation.go` reclassified in ROADMAP.md
- [ ] TODOS.md + CHANGELOG.md + ROADMAP.md updated
