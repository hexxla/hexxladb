# Active Work

Immediate next steps. Update after each session.

## Current

- [ ] **Seam-aware context assembly** — v0.1.0 blocker; filter/supersede outdated cells via seam links during LoadContextPack
  - [ ] Detect "superseded by" seams in context assembly
  - [ ] Walk seam chains to current truth
  - [ ] Exclude superseded cells from pack (replace, don't add)
  - [ ] Preserve token budget predictability
  - [ ] Handle edge case: superseding cell outside budget

## Pending (next sessions)

- [ ] Ready for v0.1.0 release push to remote (after seam-aware assembly complete)
- [ ] Per-database MaxValueBytes — needs engine header format change + migration (reclassified to Near-term)
- [ ] Relocate secondary index files to `internal/` — import cycle; needs interface extraction (btree coupling already fixed via `deleteDirect`)
- [ ] Extract `views.go` to `internal/views` or `internal/app` — import cycle; needs interface extraction
- [ ] Complete `app.Service` use-case layer — only 4 of ~30 port methods implemented; `_ = svc` in cmd/main.go
- [ ] Monitor for v1.0.0 graduation criteria (per VERSIONING.md)

---

## Recently Completed

- 2026-04-24: Increased max cell value to 8KB (v0.1.0 blocker resolved)
- 2026-04-24: Added tag discovery API to conversational example
- 2026-04-24: Added comparison section to README (vs vector/graph/temporal DBs)
- 2026-04-24: Identified seam-aware context assembly as v0.1.0 blocker
- 2026-04-24: SoC audit validated; mvccspike rename, prune dedup, MVCC key guard added as v0.1.0 blockers
- 2026-04-24: Completed mvccspike→mvcc rename, prune profile dedup, MVCC key guard relocation
- 2026-04-24: Refactored goto assembled into collectCandidates helper
- 2026-04-24: Closed commit-time meta-key finding as false (already in correct location in DB.Update)
- 2026-04-24: Quick wins batch: Cell Template Factory, Tag Analytics, RingDensity API, Filtered Changelog, Cell Validation Hooks
- 2026-04-24: Deferred rotation.go move (import cycle requires interface extraction first)
- 2026-04-24: Quick wins batch 2: ASCII Hex Renderer, Batch PutCell, QueryStats, Explain Mode, Bulk JSON I/O, API docs update
- 2026-04-24: Fixed secondary index btree coupling: added `tx.deleteDirect`, replaced direct `tx.db.btree.Delete` calls
- 2026-04-24: Reclassified MaxValueBytes, secondary index relocation, views.go extraction to Near-term (all need design/interface extraction)

---

## Usage Notes

- **Active**: Work in progress or next immediate task
- **Pending**: Backlog for future sessions
- Move items to ROADMAP.md when they become formal roadmap features
- Create GitHub issues for bugs or external collaboration needs
- This file is intentionally lightweight and disposable
