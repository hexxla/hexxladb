# Active Work

Immediate next steps. Update after each session.

## Current

## Pending (next sessions)

- [ ] **`BatchDeleteCells`** (or equivalent) — batched/chunked **`DeleteCell`** helper + docs/tests; reference **`docs/ROADMAP.md`** Near-term bullet
- [ ] **Engine shell & MVCC exploration** — optional **`DB.Path()`**, partial reclaim research, **`PurgeCoord`** semantics, prune extensions; see **`docs/ROADMAP.md`** → *Engine shell & MVCC exploration*
- [ ] Monitor for v1.0.0 graduation criteria (per VERSIONING.md)

---

## Recently Completed

- 2026-04-29: **`docs/hexxladb/OPERATIONS.md`** — MVCC **`RetainCommitsBehindHead`** vs **`SuggestedPruneBeforeSeq`** / **`CommitSeq`** (automatic prune watermark inactive until commits exceed retention depth); operator checklist.
- 2026-04-29: **`delete_cell.go`** — package doc: **`PruneCellVersions`** drops only non-latest rows; latest tombstone per coord stays until superseded.
- 2026-04-29: Tagged **`v0.3.0`** — walk alias exports + **`NewProvenanceWire`** / **`NewFacetDerived`** (`CHANGELOG.md`); **`make ci`** before push
- 2026-04-29: Branch **feature/export-walk-aliases-helpers** merged — exported **`FacetWalkRecord`** / **`EdgeWalkRecord`** ([`walk_export_aliases.go`](walk_export_aliases.go)); **`NewProvenanceWire`** / **`NewFacetDerived`** in [`templates.go`](templates.go)
- 2026-04-27: Released v0.2.0 — embeddings/HNSW, WAL fix, B+ tree fix, llm_context_engine demo, OS-aware build system, flat .tmp/ DB paths
- 2026-04-27: WAL unbounded growth fix — `CommitWriteTxn` and `applyGroupBatch` now truncate WAL to zero after primary is durable; was accumulating all redo records until next `Open` (25 MB for 128 KB DB)
- 2026-04-27: B+ tree leaf-page-full fix — `insertIntoLeaf` update-in-place path skipped page-size check; HNSW node values grow as neighbors accumulate causing overflow; hardened `leafSplitIndex` scan boundary; regression tests added
- 2026-04-27: `examples/llm_context_engine` — realistic LLM memory retrieval demo (6 scenarios); per-turn transactions matching real production usage
- 2026-04-27: Embeddings keyspace (Phases 1-4) — flat-scan + HNSW ANN, query engine integration, benchmarks, docs

For full history see `CHANGELOG.md`.

---

## Usage Notes

- **Active**: Work in progress or next immediate task
- **Pending**: Backlog for future sessions
- Move items to ROADMAP.md when they become formal roadmap features
- Create GitHub issues for bugs or external collaboration needs
- This file is intentionally lightweight and disposable
