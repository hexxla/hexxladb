# Operator evidence (retention, soak, changefeed)

**Audience:** Teams closing roadmap **P0** “operator-owned” items. No code in this file—use it as a **template** to record evidence outside or alongside the repo (release notes, internal wiki, change record).

## 1) MVCC retention vs product SLA

| Field | Your value |
|-------|------------|
| Product max history / compliance window (wall time) | |
| Observed commits per hour (p50 / p99) | |
| Chosen `RetainCommitsBehindHead` (see [`Options.MVCCRetention`](../../options.go)) | |
| Rationale (link to design doc) | |

**Link:** [`MVCC_RETENTION.md`](./MVCC_RETENTION.md)

## 2) Soak / release candidate run

| Field | Value |
|-------|--------|
| Date / release tag or git SHA | |
| `go version` | |
| Host (CPU, RAM, disk type) | |
| `make ci` | pass / fail |
| `make integration` | pass / fail |
| `make stress` (if run) | pass / fail / skipped — **`make stress`** defaults **`TMPDIR`** to **`./.tmp`** at repo root (see **`Makefile`**); note if you override **`TMPDIR`**. |
| DB + WAL (+ changelog if enabled) size **before** | |
| DB + WAL (+ changelog if enabled) size **after** soak | |

**Procedure:** [`OPERATIONS.md`](./OPERATIONS.md) — Pre-release soak checklist, HEXXLA rollout alignment.

## 3) Changefeed consumer alignment

| Checkpoint | Done |
|------------|------|
| Handlers idempotent (replay-safe) per [`CHANGEFEED.md`](./CHANGEFEED.md) | |
| Lag / `ErrChangelogCorrupt` / commit-finalization alerting defined | |
| Runbook for append-after-commit ([`ErrCommitFinalization`](../../errors.go)) rehearsed | |

## Related

- [`ADOPTION.md`](./ADOPTION.md) — rollout context.
- [`CHANGEFEED.md`](./CHANGEFEED.md) — delivery semantics and metrics table.
