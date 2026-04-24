# Active Work

Immediate next steps. Update after each session.

## Current

- [ ] **Seam-aware context assembly** — v0.1.0 blocker; filter/supersede outdated cells via seam links during LoadContextPack
  - [ ] Detect "superseded by" seams in context assembly
  - [ ] Walk seam chains to current truth
  - [ ] Exclude superseded cells from pack (replace, don't add)
  - [ ] Preserve token budget predictability
  - [ ] Handle edge case: superseding cell outside budget
- [ ] **Rename `internal/mvccspike` → `internal/mvcc`** — v0.1.0 blocker; production `SelectVisible`/`VersionKV` in a package named "spike"; `internal/mvcc/` exists but is empty
  - [ ] Move `version_suffix_cell_key.go` and test to `internal/mvcc/`
  - [ ] Update all imports (`mvcc.go`, `mvcc_lifecycle.go`, etc.)
  - [ ] Remove empty `internal/mvccspike/` directory
- [ ] **Extract prune profile helper** — v0.1.0 blocker; identical profile→maxDelete switch in `MVCCPrunePlan` (line 68) and `PruneCellVersionsByProfile` (line 220)
  - [ ] Create `profileToMaxDelete(MVCCPruneProfile) (int, error)` in `mvcc_lifecycle.go`
  - [ ] Replace both switch blocks with single helper call
- [ ] **Move MVCC key validation out of `Tx.Put`** — v0.1.0 blocker; `tx.go:231` has index-format-specific guard in generic Put
  - [ ] Push cell key format check into `PutCell` MVCC path in `primitives.go`
  - [ ] Remove `bytes.HasPrefix(key, CellPrefix)` guard from `Tx.Put`

## Pending (next sessions)

- [ ] Ready for v0.1.0 release push to remote (after seam-aware assembly complete)
- [ ] Per-database MaxValueBytes configuration — store limit in file header, expose in Options (default 8KB)
- [ ] Vector search and embeddings storage — implement `embed/` keyspace for ANN/hybrid retrieval ([`HEXXLA_DB.md`](./docs/hexxladb/HEXXLA_DB.md))
- [ ] Context Pack "Explain" Mode — per-cell inclusion reasons for debugging token budget decisions (budget_ok, low_confidence_evicted, ring_cutoff)
- [ ] Batch PutCell with Progress — efficient ingestion with progress callbacks and continue-on-error for real-time streaming
- [ ] Cell Validation Hooks — pre-write validation for content limits, required tags, business rules; production data integrity
- [ ] Temporal Range Queries — time-series analysis ("what changed this week?") vs point-in-time queries
- [ ] Snapshot Tags/Labels — human-friendly names for MVCC snapshots ("v1.0", "pre-migration") enabling ViewAtTag
- [ ] Relocate secondary index logic to `internal/` — `cell_secondary.go` and `seam_secondary.go` bypass Tx abstraction with direct btree.Delete calls
- [ ] Extract `views.go` to `internal/views` or `internal/app` — `TokenBudgeter`, `ByteLenBudgeter`, budgeting logic are app-layer with no storage I/O
- [ ] Move `rotation.go` to `internal/tooling/rotation` — offline re-encryption utility mixed with runtime API
- [ ] Refactor `goto assembled` in `LoadContextWithBudgeting` — replace with named helper for cleaner control flow
- [ ] Encapsulate commit-time meta-key in MVCC layer — `PutCell` inlines `__meta/commit-time/` bookkeeping that belongs in MVCC init
- [ ] Complete `app.Service` use-case layer — only 4 of ~30 port methods implemented; `_ = svc` in cmd/main.go
- [ ] First production use feedback collection
- [ ] Monitor for v1.0.0 graduation criteria (per VERSIONING.md)

---

## Recently Completed

- 2026-04-24: Increased max cell value to 8KB (v0.1.0 blocker resolved)
- 2026-04-24: Added tag discovery API to conversational example
- 2026-04-24: Added comparison section to README (vs vector/graph/temporal DBs)
- 2026-04-24: Identified seam-aware context assembly as v0.1.0 blocker
- 2026-04-24: SoC audit validated; mvccspike rename, prune dedup, MVCC key guard added as v0.1.0 blockers

---

## Usage Notes

- **Active**: Work in progress or next immediate task
- **Pending**: Backlog for future sessions
- Move items to ROADMAP.md when they become formal roadmap features
- Create GitHub issues for bugs or external collaboration needs
- This file is intentionally lightweight and disposable
