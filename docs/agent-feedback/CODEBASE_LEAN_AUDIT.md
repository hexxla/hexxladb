# Codebase lean audit — deduplication & dependency-aware consolidation

**Audience:** Maintainers and coding agents planning refactors that reduce line count without breaking hexagonal boundaries or the public API.

**Date:** 2026-04-23

**Scope:** Iterative analysis of duplication, mechanical boilerplate, and consolidation opportunities across the module. This document is **advisory**: it does not mandate changes; it ranks options by risk and ROI.

---

## 1. Executive summary

HexxlaDB is structured as a **Go module** with a **stable root package** (`github.com/hexxla/hexxladb`) and **private implementation** under `internal/`. The largest **safe** wins for a leaner tree were:

1. **Outbound adapter** `internal/adapters/out/hexxladb/storage.go` — **Done:** unexported `withUpdate`, `viewPair`, `viewTriple` (generics) dedupe `View`/`Update`/`ctx.Err()` patterns without touching `internal/engine`.
2. **Root package secondary index paths** — **Done (key paths + scan prefixes):** `cell*SecondaryKey` / `seam*SecondaryKey` for put/remove; `cell*ScanBounds` / `seam*ScanBounds` centralize MVCC vs plain range choice for `Ascend*` entry points.
3. **`internal/index` encoding** — **Partially done:** [`internal/index/secondary_segment.go`](../../internal/index/secondary_segment.go) provides `appendLenPrefixedUTF8`; [`SourceKey`](../../internal/index/source_key.go) and [`TagKey`](../../internal/index/tag_key.go) use it. **Not done:** deduplicating `SourceRangePrefix` / `TagRangePrefix` construction or seam key families with the same primitive (higher regression risk vs ROI).

Lower priority unchanged: documentation and **examples** — prefer **cross-links** over deleting narrative (§5.7).

---

## 2. Repository purpose (short)

- **Embedded database** with B+tree engine, optional MVCC, optional encryption, optional logical **changelog** (not WAL decode).
- **Public API** lives at the **module root** (`db.go`, `tx.go`, `primitives.go`, `cell_secondary.go`, …); see `doc.go` and `docs/hexxladb/API_REFERENCE.md`.
- **Hexagonal layout:** `internal/domain` and `internal/app` define **ports**; `internal/adapters/in|out` implement them; `cmd` is the composition root. Canonical rules: `docs/context/HEXAGONAL_ARCHITECTURE.md`, `AGENTS.md`.

---

## 3. Dependency rules (audit constraints)

Refactors must preserve:

| Rule | Implication for deduplication |
|------|--------------------------------|
| Domain/app **must not** import adapters | Do not “simplify” by moving adapter logic into `internal/domain` to delete types. |
| Port interfaces **must not** mention adapter types | Keep `domain.Storage` neutral; adapter stays in `internal/adapters/out/...`. |
| Domain/app **must not** import `internal/engine` or `internal/index` | Persistence/key encoding stays in **`package hexxladb`** (root) and internal packages; adapters call **only** `hexxladb`. |
| Root package is the stable external surface | Thin facades (e.g. changelog aliases in `db_changelog.go`) are **intentional**; do not remove them for line count. |

---

## 4. Methodology

1. Map **layers**: root API → engine/index/record/changelog → adapters → app/cmd.
2. Identify **mechanical repetition** (same control flow, different types or key builders).
3. Exclude **false duplication**: type aliases and one-line delegations that **stabilize** exported names or errors.
4. Rank by **ROI** (lines removed vs regression risk) and **test touchpoints** (`go test ./...`, focused tests in `secondary_indexes_test.go`, seam/cell tests).

---

## 5. Findings

### 5.1 High impact — `internal/adapters/out/hexxladb/storage.go`

**Observation:** Methods fall into a small number of templates:

- **Read, no extra capture:** `s.DB.View(func(tx *hxdb.Tx) error { return tx.X(ctx, ...) })` or without `ctx` in the inner call.
- **Write with early `ctx.Err()`:** check context, then `s.DB.Update(...)`.
- **Read with results:** `var out T; err := s.DB.View(...); return out, ok, err` (and variants).

**Risk:** Low if behavior is preserved (same `View`/`Update`, same context checks, same forwarding to `Tx` methods).

**Recommendation:** Add **unexported** helpers in the same package, for example:

- `func (s *Storage) view(err error, fn func(*hxdb.Tx) error) error` — or separate `view`/`update` that standardize `ctx` handling.
- Generic helpers for `view2[T any](...) (T, bool, error)` / `view1[T any](...) (T, error)` where the `Tx` method returns tuples.

**Expected effect:** Largest line-count reduction in the module with minimal architectural movement.

**Files:** `internal/adapters/out/hexxladb/storage.go`

**Status — complete:** `withUpdate`, `viewPair[T]`, `viewTriple[T]`; `AscendFacetsForCell` / `AscendEdgesFrom` keep explicit inner `ctx.Err()` loops (per original recommendation).

---

### 5.2 High impact — secondary index maintenance: `cell_secondary.go` vs `seam_secondary.go`

**Observation:**

- **`remove*SecondaryIndex` / `put*SecondaryIndex`:** Repeated `if tx.db.useMVCC { key = WithVersion(...) } else { key = ... }`, then `Delete`/`Put`, plus `ErrSourceIDTooLong` → `ErrInvalidArgument` mapping where applicable.
- Cells add **tags** and **three** families (source, time, tag); seams use **two** families (seam-source, seam-time) with different key APIs — similar **shape**, different builders.

**Risk:** Medium — easy to break edge cases (MVCC suffix, empty source, validity buckets).

**Recommendation:**

- Extract **small** helpers that take **closures** or function values for “build key” / “delete or put”, keeping **tag iteration** and **seam ULID** logic explicit at call sites.
- Optionally share **only** the MVCC branch pattern: `func mvccKey(useMVCC bool, seq uint64, plain func()([]byte, error), versioned func(uint64)([]byte, error)) ([]byte, error)` — naming and signatures should match existing style in the repo.

**Files:** `cell_secondary.go`, `seam_secondary.go`

**Status — complete:** `cellSourceSecondaryKey`, `cellTimeSecondaryKey`, `cellTagSecondaryKey`; `seamSourceSecondaryKey`, `seamTimeSecondaryKey`; shared by `put*SecondaryIndex` / `remove*SecondaryIndex`.

---

### 5.3 Medium-high — `Ascend*` scans (cells vs seams)

**Observation:** `AscendCellsBySource`, `AscendCellsInTimeBucket`, `AscendCellsByTag` share a skeleton with `AscendSeamsBySource` and `AscendSeamsInTimeBucket`:

1. `ctx.Err()`, nil `tx` / closed DB guards.
2. MVCC-aware **range** computation (`index.*RangePrefix*` vs `*AllVersions`).
3. `AscendRange` loop: context check → parse key → dedupe → load primary (`GetCell` vs seam decode path) → user callback.

**Risk:** Medium-high — generics/callbacks can obscure MVCC snapshot semantics and dedupe maps (`PackedCoord` vs ULID string).

**Recommendation:** Introduce **one helper per aggregate** (e.g. `ascendCellsSecondary`, `ascendSeamsSecondary`) parameterized by range builder, key parser, and loader — **after** adapter and put/remove dedup are stable and tests are green.

**Files:** `cell_secondary.go`, `seam_secondary.go`; tests: `secondary_indexes_test.go`

**Status — partial:** MVCC/plain **range bounds** for each ascend are centralized as `cellSourceScanBounds`, `cellTimeScanBounds`, `cellTagScanBounds`, `seamSourceScanBounds`, `seamTimeScanBounds`. The **full** parameterized helper (`ascendCellsSecondary` / `ascendSeamsSecondary` unifying parse → dedupe → load → callback) was **not** introduced — dedupe maps and loaders differ enough that risk outweighed benefit after prefix extraction.

---

### 5.4 Medium — `internal/index` key families

**Observation:** `source_key.go`, `tag_key.go`, and related files repeat:

- Prefix + `uint16` length + UTF-8 segment + `/` + packed coord (or analogous layout).
- Range `[min coord, max coord]` for prefix scans.
- `*WithVersion` appending fixed suffix length.

**Risk:** Medium — key layout bugs are subtle; shared helpers must be covered by **existing** parse tests plus golden boundaries.

**Recommendation:** Consider a **single internal** `appendLenPrefixedComponent` + shared range-bound builder **only if** accompanied by consolidated tests and clear godoc pointing to HEXXLA_DB.md sections.

**Files:** `internal/index/*.go` (especially `source_key.go`, `tag_key.go`, `time_key.go`, seam key files)

**Status — partial:** Shared **segment** encoding in `appendLenPrefixedUTF8` ([`secondary_segment.go`](../../internal/index/secondary_segment.go)); used by **`SourceKey`** and **`TagKey`** only. **`SourceRangePrefix` / `TagRangePrefix`** (and time/seam range builders) still duplicate layout logic for inclusive scan bounds — optional follow-up if benchmarks show maintainability pain.

---

### 5.5 Low — `internal/app/app.go` vs `domain.Storage`

**Observation:** `Service` exposes only a **subset** of storage operations (`PutCell`, tag ascents, `ListExistingTopics`). This is **narrow product surface**, not redundant wrapping of the full adapter.

**Recommendation:** Do **not** auto-generate app methods from `domain.Storage` unless product requirements explicitly demand full parity — that would widen API surface without user need.

**Files:** `internal/app/app.go`, `internal/domain/storage.go`

---

### 5.6 Low — Changelog surface

**Observation:** `db_changelog.go` re-exports operation constants and `ChangelogRecord` via type aliases and delegates `ReadChangelogSince` to `internal/changelog`.

**Recommendation:** Treat as **boundary design**, not duplication to remove.

**Files:** `db_changelog.go`, `internal/changelog/changelog.go`

---

### 5.7 Documentation & examples

**Observation:** `examples/full_api_demo/tour.go` and docs under `docs/hexxladb/` may overlap in narrative with README/API docs.

**Recommendation:** Prefer **cross-links** (“see example X”) over deleting prose or shrinking examples; avoids harming onboarding.

---

## 6. Non-goals (do not merge for “lean”)

- Collapsing **hex layers** (domain importing adapters or engine).
- Removing **public** type aliases / thin facades that exist for **semver-stable** names.
- Combining **WAL** concerns with **logical changelog** — different products (recovery vs semantic feed).

---

## 7. Recommended iteration order

| Step | Action | Status |
|------|--------|--------|
| 1 | Refactor `storage.go` with unexported helpers | **Done** |
| 2 | Dedup MVCC key scaffolding in `put*`/`remove*` (cells + seams) | **Done** |
| 3 | Parameterized `Ascend*` body (single generic scan) | **Deferred** — only scan-bound helpers landed (§5.3) |
| 4 | Shared `internal/index` encoding | **Partial** — `appendLenPrefixedUTF8`; range builders unchanged |

After each future step: run **`make ci`** ([`go-local-checks`](../../.cursor/skills/go-local-checks/SKILL.md) parity).

---

## 8. Key file index (for navigators)

| Area | Paths |
|------|--------|
| Public API root | `*.go` next to `go.mod`, especially `db.go`, `tx.go`, `primitives.go`, `cell_secondary.go`, `seam_secondary.go`, `facets_edges.go`, `db_changelog.go` |
| Outbound adapter | `internal/adapters/out/hexxladb/storage.go` |
| Ports | `internal/domain/storage.go`, `internal/app/app.go` |
| Changelog implementation | `internal/changelog/changelog.go` |
| Secondary index encoding | `internal/index/source_key.go`, `tag_key.go`, `secondary_segment.go`, `time_key.go`, seam-related keys |
| Architecture | `docs/context/HEXAGONAL_ARCHITECTURE.md`, `AGENTS.md` |

---

## 9. Sign-off

This audit is a **living** note: after refactors, update §5 findings (or mark them done) so future agents do not repeat the same analysis.

**Implementation batch (merged):** adapter + secondary + partial index work landed together with green **`make ci`**.

| § | Topic | Result |
|---|--------|--------|
| 5.1 | `storage.go` | **Complete** — `withUpdate`, `viewPair`, `viewTriple` in [`internal/adapters/out/hexxladb/storage.go`](../../internal/adapters/out/hexxladb/storage.go). |
| 5.2 | Put/remove secondaries | **Complete** — `cell*SecondaryKey` / `seam*SecondaryKey` in [`cell_secondary.go`](../../cell_secondary.go), [`seam_secondary.go`](../../seam_secondary.go). |
| 5.3 | `Ascend*` | **Partial** — `cell*ScanBounds` / `seam*ScanBounds`; no unified generic ascend loop. |
| 5.4 | Index encoding | **Partial** — [`appendLenPrefixedUTF8`](../../internal/index/secondary_segment.go) + [`SourceKey`](../../internal/index/source_key.go) / [`TagKey`](../../internal/index/tag_key.go); range prefix functions unchanged. |
| 5.5–5.7 | App surface, changelog, docs | **Unchanged** — recommendations stand (no refactor required). |

**Optional backlog:** (1) merge `AscendRange` loops behind small typed helpers if duplication grows; (2) reuse `appendLenPrefixedUTF8` or a sibling helper inside `SourceRangePrefix` / `TagRangePrefix`; (3) optional [`storage_test.go`](../../internal/adapters/out/hexxladb/storage_test.go) smoke test for `domain.Storage` forwarding.
