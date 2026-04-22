# ADR: Raw `Tx.Put` and MVCC cell keys (format v2)

## Status

Accepted — guard in [`tx.go`](../../tx.go) (`Tx.Put`).

## Context

Under MVCC, physical **`cell/`** keys carry an 8-byte **`commit_seq`** suffix ([`internal/index/cell_version.go`](../../internal/index/cell_version.go)). Raw **`Tx.Put`** calls bypass **`Tx.PutCell`**, so callers can insert **`cell/`** rows without matching secondaries (**`source/`**, **`time/`**, **`tag/`**) or consistent ordering relative to metadata writes. That breaks snapshot invariants and can stress btree delete/rebalance paths when **`cell/`** rows interleave with **`__meta/`** in dangerous orders ([`MVCC_DESIGN.md`](./MVCC_DESIGN.md)).

## Decision

**Option A — Guard:** When the database uses MVCC (**`DB.useMVCC`**), **`Tx.Put`** rejects keys with prefix **`cell/`** that do not parse as a full **`ParseCellVersionKey`** (length + layout). The error is **`ErrInvalidArgument`** with a message directing callers to **`Tx.PutCell`**.

Low-level tooling that truly needs raw puts must encode version-suffixed keys itself and accept responsibility for secondary indexes (not a supported embedded use case).

**Option B — Engine hardening** for arbitrary raw **`cell/`** layouts remains a possible future milestone if a concrete internal need appears.

## Consequences

- **Breaking change** for any code that used raw **`Put`** with **`index.CellKey`** on MVCC databases; the supported path is **`PutCell`**.
- **`make ci`** includes regressions asserting the guard ([`secondary_indexes_test.go`](../../secondary_indexes_test.go) pattern).
