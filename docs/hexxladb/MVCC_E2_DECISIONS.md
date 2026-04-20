# Phase E2+ MVCC — locked engineering decisions (sign-off)

This addendum locks choices referenced in [`MVCC_DESIGN.md`](./MVCC_DESIGN.md) §2–§6 and §10 **before** production MVCC code ships. It is the **product + engineering sign-off** gate from §10.

## §2 Snapshot identity

- **Commit sequence** (`uint64`, monotonic per database) is the **authoritative** visibility clock. It is persisted in the engine header as **`CommitSeq`** (see [`ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md)).
- **First public snapshot API:** [`ViewAt`](./TX.md) accepts **`read_seq uint64`** (commit sequence). Mapping **wall-clock `time.Time` → `read_seq`** is **deferred** until a **`commit_seq` → time** table or policy exists (see MVCC_DESIGN §2).
- **View** pins **`read_seq = header.CommitSeq`** at the start of the callback (snapshot of last committed state at call time).

## §3 Physical version storage

- **Option A** — version suffix on logical keys: physical key = `logical_key || encode_uint64_be(commit_seq)` for **`cell/`** primary rows. Helpers live in [`internal/index/cell_version.go`](../../internal/index/cell_version.go); visibility uses [`SelectVisible`](../../internal/mvccspike/version_suffix_cell_key.go) (largest `commit_seq ≤ read_seq`).

## §4 Secondary indexes (`source/`, `time/`)

- **Index-as-of snapshot:** secondary keys include the same **`commit_seq`** suffix as the cell primary row they index, so each committed version has consistent primary + secondary entries. Scans dedupe by logical [`PackedCoord`](../../internal/lattice/packed.go) and resolve the visible cell via [`GetCell`](../../primitives.go) at the transaction’s **`read_seq`**.

## §5 WAL

- **Page-level redo WAL** unchanged ([`ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md)); no logical WAL in this phase.

## §6 Garbage collection

- **Deferred:** a **`GC`** pass to delete obsolete physical versions and reclaim btree keys is **not** required for the first merge. **`CommitSeq`** advances; old version rows remain on disk until a future GC milestone. Document operational disk growth until GC lands.

## Migration

- **v1** databases (`format_version == 1`): **unchanged** single-version behavior; **`Open`** does not auto-upgrade.
- **v2** databases (`format_version == 2`): created when [`Options.EnableMVCC`](../../options.go) is set for a **new** file. Existing v1 files open as v1 regardless of the flag.

## Out of scope (unchanged from MVCC_DESIGN §10)

- Phase **H** `embed/` keys.
- Full **logical WAL** for MVCC.
