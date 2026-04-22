# MVCC retention and pruning

**Audience:** Operators embedding format-v2 databases (`Options.EnableMVCC`). **Normative mechanics:** [`internal/index`](../../internal/index/cell_version.go); **API:** [`mvcc_lifecycle.go`](../../mvcc_lifecycle.go).

## Policy at open

[`Options.MVCCRetention.RetainCommitsBehindHead`](../../options.go) configures how much commit history to **retain** when deriving a suggested prune watermark:

- Only versions with **strictly lower** `commit_seq` than `(header.CommitSeq - RetainCommitsBehindHead)` may be reclaimed, and **never** the latest visible version per logical cell ([`PruneCellVersions`](../../mvcc_lifecycle.go)).
- Zero (**default**) disables automatic suggestions; operators supply `beforeSeq` explicitly to [`PruneCellVersions`](../../mvcc_lifecycle.go).

## Helpers

| API | Purpose |
|-----|---------|
| [`StatsMVCC`](../../mvcc_lifecycle.go) | `CommitSeq`, versioned row count, logical cell count |
| [`SuggestedPruneBeforeSeq`](../../mvcc_lifecycle.go) | `beforeSeq` from open-time retention policy |
| [`MVCCPrunePlan`](../../mvcc_lifecycle.go) | Combines suggestion with [`MVCCPruneProfile`](../../mvcc_lifecycle.go) batch sizing |
| [`PruneScheduler.Tick`](../../mvcc_lifecycle.go) | One bounded pass (`PruneScheduler` does **not** spawn goroutines—call from your scheduler) |

## SLA framing (operator-owned)

Retention is **commits**, not wall-clock SLA. Map product SLAs to `RetainCommitsBehindHead` using your observed commits-per-interval and snapshot requirements. Document org-specific defaults beside the deployment.

## Stress validation

Integration test `TestIntegration_MVCC_sustainedPutCellSameKey` ([`mvcc_churn_integration_test.go`](../../mvcc_churn_integration_test.go), build tag **`integration`**) performs **6000** MVCC commits on one logical cell, then reclaims stale versions via repeated [`PruneCellVersions`](../../mvcc_lifecycle.go) batches (run `make integration`).

## Related

- [OPERATIONS.md](./OPERATIONS.md) — incident checklist and backups.
- [ADOPTION.md](./ADOPTION.md) — operator retention expectations.
