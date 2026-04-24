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
- [ ] Per-database MaxValueBytes configuration — store limit in file header, expose in Options (default 8KB)
- [ ] Vector search and embeddings storage — implement `embed/` keyspace for ANN/hybrid retrieval ([`HEXXLA_DB.md`](./docs/hexxladb/HEXXLA_DB.md))
- [ ] Context Pack "Explain" Mode — per-cell inclusion reasons for debugging token budget decisions (budget_ok, low_confidence_evicted, ring_cutoff)
- [ ] Batch PutCell with Progress — efficient ingestion with progress callbacks and continue-on-error for real-time streaming
- [ ] Cell Validation Hooks — pre-write validation for content limits, required tags, business rules; production data integrity
- [ ] Temporal Range Queries — time-series analysis ("what changed this week?") vs point-in-time queries
- [ ] Snapshot Tags/Labels — human-friendly names for MVCC snapshots ("v1.0", "pre-migration") enabling ViewAtTag
- [ ] First production use feedback collection
- [ ] Monitor for v1.0.0 graduation criteria (per VERSIONING.md)

---

## Recently Completed

- 2026-04-24: Increased max cell value to 8KB (v0.1.0 blocker resolved)
- 2026-04-24: Added tag discovery API to conversational example
- 2026-04-24: Added comparison section to README (vs vector/graph/temporal DBs)
- 2026-04-24: Identified seam-aware context assembly as v0.1.0 blocker

---

## Usage Notes

- **Active**: Work in progress or next immediate task
- **Pending**: Backlog for future sessions
- Move items to ROADMAP.md when they become formal roadmap features
- Create GitHub issues for bugs or external collaboration needs
- This file is intentionally lightweight and disposable
