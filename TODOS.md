# Active Work

Immediate next steps. Update after each session.

## Current

- [x] **Increase max cell value to 8KB** — v0.1.0 release blocker
  - [x] Update `internal/engine/btree_page.go`: `maxValBytes = 8192`
  - [x] Update `API_REFERENCE.md` storage limits section (512B → 8KB)
  - [x] No format_version bump needed (runtime constant only)
  - [x] All databases (new + existing) automatically get 8KB limit

## Pending (next sessions)

- [ ] Ready for v0.1.0 release push to remote
- [ ] First production use feedback collection
- [ ] Monitor for v1.0.0 graduation criteria (per VERSIONING.md)

---

## Recently Completed

- 2026-04-24: Increased max cell value to 8KB (v0.1.0 blocker resolved)

---

## Usage Notes

- **Active**: Work in progress or next immediate task
- **Pending**: Backlog for future sessions
- Move items to ROADMAP.md when they become formal roadmap features
- Create GitHub issues for bugs or external collaboration needs
- This file is intentionally lightweight and disposable
