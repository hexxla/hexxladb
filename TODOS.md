# Active Work

Immediate next steps. Update after each session.

## Current

- [ ] **Increase max cell value to 8KB** — v0.1.0 release blocker
  - [ ] Update `internal/engine/const.go`: `MaxValueBytes = 8192`
  - [ ] Bump `format_version` to v3 in `ENGINE_FORMAT.md`
  - [ ] Update `API_REFERENCE.md` storage limits section
  - [ ] Validate btree page math: 64KiB page / 8KB values = ~8 entries/page
  - [ ] Update all format_version checks in code
  - [ ] Add migration note: v1/v2 databases need explicit migration
  - [ ] Test: 8KB cell write/read roundtrip
  - [ ] Test: MVCC with 8KB values

## Pending (next sessions)

- [ ] Ready for v0.1.0 release push to remote (after 8KB work complete)
- [ ] First production use feedback collection
- [ ] Monitor for v1.0.0 graduation criteria (per VERSIONING.md)

---

## Recently Completed

---

## Usage Notes

- **Active**: Work in progress or next immediate task
- **Pending**: Backlog for future sessions
- Move items to ROADMAP.md when they become formal roadmap features
- Create GitHub issues for bugs or external collaboration needs
- This file is intentionally lightweight and disposable
