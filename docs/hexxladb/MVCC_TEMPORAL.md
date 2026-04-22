# MVCC temporal semantics (`ViewAt` vs `ViewAtTime`)

**Audience:** Integrators using snapshot reads on format-v2 databases.

## Commit sequence

The authoritative visibility clock is **`CommitSeq`** in the engine header ([`ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md)). Each successful [`Update`](../../tx.go) / [`Batch`](../../tx.go) that performs MVCC writes advances `CommitSeq` after the transaction body and header update complete.

## `ViewAt(readSeq)`

[`DB.ViewAt`](../../tx.go) pins **`read_seq`** for the callback. Primitive reads resolve the largest stored version with **`commit_seq ≤ read_seq`** per key family (cells, facets, seams—see [`mvcc.go`](../../mvcc.go)).

[`ErrReadSeqFuture`](../../errors.go) is returned if `read_seq` exceeds the header’s `CommitSeq`.

## `ViewAtTime(asOf)`

[`DB.ViewAtTime`](../../tx.go) maps **UTC wall time** to a **`read_seq`** using the commit timeline:

- During each MVCC [`Update`](../../tx.go), an **`__meta/commit-time/`** btree key records `(wall_unix_nano, writeSeq)` ([`internal/index/commit_time_key.go`](../../internal/index/commit_time_key.go)); the wall timestamp is sampled at transaction **start** (before the callback).
- [`resolveReadSeqAtOrBeforeUnixNano`](../../tx.go) scans commits at or before `asOf` and picks the **maximum** `commit_seq` in that window.

Determinism requires stable clock usage (UTC). Same `asOf` yields the same snapshot for a given database history.

## Secondary indexes (`source/`, `time/`, seam secondaries)

Version-suffixed secondary keys coexist with MVCC primaries; [`AscendCellsBySource`](../../cell_secondary.go) (and seam variants) dedupe by logical ID and evaluate visibility via [`GetCell`](../../mvcc.go) / seam readers at the transaction **`read_seq`**.

## Relation to validity filters

[`WalkRingAt`](../../primitives.go), [`LoadContextAt`](../../primitives.go), and [`record.ValidAt`](../../internal/record/validity.go) filter by **record validity windows**—orthogonal to **`read_seq`** (snapshot time vs. domain validity interval).

## Related

- [`MVCC_E2_DECISIONS.md`](./MVCC_E2_DECISIONS.md) — historical sign-off (supplement with this doc for wall-clock snapshots, which shipped after E2 text).
- [`TX.md`](./TX.md) — transaction entrypoints.
