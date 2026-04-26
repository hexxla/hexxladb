---
description: Snapshot Tags/Labels — named MVCC snapshots, ViewAtTag
---

## Goal

Add human-friendly names to MVCC commit sequences. Enables:

```go
db.TagSnapshot("v1.0-release")          // pin current head
db.ViewAtTag("v1.0-release", func(tx) {}) // open snapshot by name
db.ListSnapshotTags()                   // enumerate all tags
db.DeleteSnapshotTag("v1.0-release")    // remove tag
```

No engine header format change. Tags stored as B+ tree keys under the `__meta/snap-tag/` prefix.

## Key layout

```
__meta/snap-tag/<label> → uint64 commit_seq (big-endian)
```

- `label`: arbitrary UTF-8 string, max 200 bytes (enforce in TagSnapshot)
- value: 8-byte big-endian commit_seq
- sorted by label, iterable via AscendRange

## Files

| File | Change |
|---|---|
| `internal/index/snap_tag_key.go` | `SnapTagKey(label)`, `ParseSnapTagKey(k)`, `SnapTagPrefix` constant |
| `snapshot_tags.go` | `DB.TagSnapshot`, `DB.ViewAtTag`, `DB.ListSnapshotTags`, `DB.DeleteSnapshotTag`, `SnapshotTag` type |
| `snapshot_tags_test.go` | Full test coverage |
| `errors.go` | `ErrSnapshotTagNotFound`, `ErrSnapshotTagLabelTooLong` |
| `docs/hexxladb/API_REFERENCE.md` | New "Snapshot Tags" section |
| `CHANGELOG.md` | Unreleased entry |
| `TODOS.md` | Mark complete |
| `docs/ROADMAP.md` | Move to Completed |

## Implementation notes

- `TagSnapshot` runs under `db.mu.Lock()`, reads `eng.ReadHeader().CommitSeq`, writes key via `btree.Put` inside `BeginWriteTxn`/`CommitWriteTxn`
- `ViewAtTag` reads the tag key (under `db.mu.RLock()`), extracts seq, calls existing `DB.ViewAt(seq, fn)`
- `ListSnapshotTags` returns `[]SnapshotTag{Label, CommitSeq}` sorted by label
- `DeleteSnapshotTag` removes the key via `btree.Delete` inside a write txn
- Label validation: non-empty, ≤200 bytes, no null bytes
- `ErrSnapshotTagNotFound` returned when tag key absent in `ViewAtTag` / `DeleteSnapshotTag`
- All ops hold `db.mu` appropriately (writes: Lock, reads: RLock)
- Hexagonal architecture: all logic at package root — no `internal/` changes needed beyond the key helper

## Tests

- Tag current head, view at tag, assert cell count matches
- Tag at two different commit points, assert different cell counts via `ViewAtTag`
- `ViewAtTag` unknown label → `ErrSnapshotTagNotFound`
- `ListSnapshotTags` returns all tags sorted
- `DeleteSnapshotTag` removes; subsequent `ViewAtTag` returns not-found
- Label too long → `ErrSnapshotTagLabelTooLong`
- Empty label → error
- MVCC disabled: `TagSnapshot` returns nil (no-op is fine; ViewAt still works on seq 0)

## Backward compatibility

Purely additive. No existing API changes.
