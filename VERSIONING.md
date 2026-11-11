# Versioning and compatibility

**Maturity: alpha.** The HexxlaDB–Mosaic–MCP Ratchet stack is alpha.
Existing tags remain historical releases; their numbers do not establish
production readiness or authorize a v1 compatibility claim.

This document owns HexxlaDB release policy, Go API compatibility and storage
compatibility. The public import remains `github.com/hexxla/hexxladb`.

## Release policy

Versions identify releases; maturity is an explicit decision. The minor number
has no ceiling: `v0.9.0` can become `v0.10.0` and later `v0.100.0`. No amount of
iteration automatically promotes a project to v1.

- Published tags are immutable. Preserve every tag and its contents. Never delete, move, reuse or
  reset a tag to make the project look younger; retained tags also support
  reproducible dependency resolution and recovery.
- Keep versions independent across the three repositories. Record the exact
  compatible combination when qualifying the stack.
- Commit normal development without tagging every change. Publish deliberate
  milestones as annotated tags on validated commits.
- During v0, use a minor release for a feature group or intentional compatibility
  break, and patches for compatible fixes. Record breaks and migration steps
  under `CHANGELOG.md` → `[Unreleased]` before release.
- Label new development milestones explicitly, for example `v0.N.0-alpha.1`,
  then `-alpha.2`, `-beta.1` and `-rc.1`. Here `N` is a chosen future minor;
  use a numeric minor in an actual tag. A prerelease sorts below its corresponding
  normal version, so choose a base above existing normal releases.
- Move to beta, release candidate or v1 only through the gates below. A new
  feature or compatibility break during qualification requires a new candidate
  and requalification of affected behavior.
- Use Git tags for pre-v1 distribution. GitHub Release pages and automatic
  binary publication are reserved for v1 unless explicitly reconsidered.
- Follow [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).
  After v1, compatible additions increment minor, compatible fixes increment
  patch, and public compatibility breaks require a new major version.

## Maturity and evidence

| Stage | Meaning |
| --- | --- |
| Alpha (current) | Working software under active interface and behavior development; suitable for controlled evaluation with recoverable data. General production sign-off has not been given. |
| Beta | Intended initial scope is settled and the beta gates pass for a declared deployment profile. Changes focus on qualification and defect correction. |
| Release candidate | Intended v1 contracts and dependency versions are frozen; all v1 evidence is complete and final candidate checks pass. Only release-blocking corrections are admitted. |
| v1 | Maintainers accept the documented compatibility and support commitments for the qualified profile. Future growth remains possible under SemVer. |

CI is necessary evidence, but does not alone establish operating maturity.
Count an unrun, failed, skipped or unrecorded gate as unmet. Historical evidence
is useful background and must not be presented as a pass on a new candidate.
The [shared qualification plan](https://github.com/hexxla/mosaic/blob/main/docs/RELEASE_QUALIFICATION.md)
owns the stack workload, observation windows and report format. Run it against
explicit source SHAs and dependency versions; an unpublished local workspace
alone does not qualify the distributable stack. Repository promotion may happen
independently, but the combined stack remains alpha until its complete selected
combination passes the stack beta gates.

Internal packages are not a public compatibility promise. Consumers use the
root API and documented behavior. API, engine format and changefeed framing
versions are separate contracts.

## On-disk format

Plaintext files use engine v2 and encrypted files use authenticated engine v3.
Both use the same MVCC records and commit sequences. V3 adds authenticated
XChaCha20-Poly1305 page envelopes, authenticated WAL/header publication,
generation-linked free pages, and tail reclaim. See the
[engine layout](internal/engine/ENGINE_FORMAT.md) and
[encryption guide](docs/hexxladb/ENCRYPTION.md).

The main branch refuses single-version engine v1, AES-XTS files, and
unknown engine versions with `ErrUnsupportedFormatVersion`. The
[offline conversion procedure](docs/hexxladb/OPERATIONS.md#offline-conversion-of-retired-files)
uses the pinned v0.6.0 release for legacy files. The current library checks
for unfinished conversion candidates and refuses them.

Changelog framing is independent. Plaintext uses changelog v1 and encrypted
databases use authenticated changelog v3; opening an accepted encrypted v2
sidecar performs the documented validated conversion to v3. Offline
[`RotateEncryption`](rotation.go) can encrypt a plaintext v2 database or change
an encrypted v3 database's credential while preserving its changelog. Retain
the complete recovery set until the replacement and its backup are verified.

## Production-readiness profile

No current production sign-off is recorded. This profile is the proposed scope
for qualification, including controlled pre-v1 pilots. Completing a bounded
qualification does not automatically create a v1 API commitment.

The initial candidate profile is:

- the Go module embedded in one Linux/amd64 process using Go 1.27.4 or newer;
- exactly one open owner for each database path and serialized public writes;
- a trusted local filesystem that honors exclusive creation, file locking, and
  `fsync`/`fdatasync` durability for the primary, WAL, and optional changelog;
- authenticated engine format v3 for encrypted deployments, with 4 KiB logical pages;
- MVCC, with authenticated primary pages, encrypted changelog, and
  durable consumers exercised together when those optional features are used;
- at most 20,000 stored vectors at 32 dimensions or 10,000 at 384 dimensions
  with a 64 MiB page cache unless the deployment regenerates vector-scale
  evidence for its larger or different vector workload;
- low-to-moderate serialized writes or bounded batch ingestion whose measured
  latency, file growth, and maintenance windows meet the deployment's targets;
- application-owned monitoring, idempotent changefeed consumption, backup
  retention, restore drills, MVCC pruning, and explicit compaction.

The proposed qualification profile excludes network service operation, concurrent
or distributed writers, replication, automatic failover, unbounded vector or
record scale, high-volume random-write workloads, and protection from an
attacker who can delete storage, replay an older valid non-root page into the
same slot, or roll back the complete recovery set. Those limits require trusted
storage and independently authenticated/versioned backups as described in
[`ENCRYPTION.md`](docs/hexxladb/ENCRYPTION.md#threat-model-and-residual-limits).

Every adopting deployment must declare its expected dataset and vector sizes,
read/write mix, enabled options, latency/throughput objectives, backup interval,
RPO, and RTO. It must then pass the bounded production-readiness gates below.
The repository records mechanisms and reproducible evidence; it does not invent
site-specific service-level objectives.

### Historical evidence

The v0.6.0 candidate recorded local validation, synthetic restore, cross-build,
install and format-refusal rehearsals on 2026-08-26. Reproducible performance
runners and a five-minute minimum-sample reference qualification also exist.
These are historical results, not qualification of current `main`. No current
owner-confirmed operating pilot, restore/rollback report or complete beta/v1
qualification report is recorded. The five-minute runner is a smoke check;
it does not satisfy the shared plan's observation window.

## Beta gates

All rows require a candidate-specific report; the current overall status is
**unmet / alpha**. Use the shared plan for sequencing and evidence capture.

| Gate | Pass condition |
| --- | --- |
| Scope and contracts | Declare the supported platform/filesystem, dataset/vector limits, options and latency, memory, disk-growth, RPO and RTO targets before running. Review the root API inventory and accepted storage formats; document every intentional break. |
| Candidate checks | `task ci`, `task govulncheck`, `task integration`, `task fuzz` and `task build-all` pass on the selected source. Cross-build success alone does not qualify another operating system. Triage vulnerability findings and block reachable unresolved security defects. |
| Data and recovery | Verify committed outcomes, uncertain/durable commit handling, crash recovery, encrypted backup/restore, changefeed continuity and consumer replay. Exercise pruning/compaction, supported conversion and unsupported-format refusal on copies of the recovery set. Check content/history against an oracle. |
| Workload qualification | Complete the shared 24-hour beta workload with at least 10,000 completed operations, including read/write concurrency within the single-owner contract, reopen and maintenance. Meet predeclared resource and performance targets. |
| Integration | The selected published/publishable HexxlaDB version passes Mosaic integration tests and the actual stack workload without relying only on local replacements. |
| Review | No open blocker involving data loss/corruption, incorrect committed outcomes, security boundary failure or uncontrolled resource use within the claimed profile. Record remaining limitations and an accountable owner. |

## Compatibility policy before v1

The main branch maintains the two current storage profiles. Older releases
remain available by tag for conversion and recovery. Before v1, intentional
API removals and retired formats can ship in a minor release with changelog
and upgrade notes. Keep candidate publication, unsupported-format refusal,
and data-integrity checks for the formats the current library accepts.

## Root API stability inventory

The exhaustive declaration reference is generated from source with `go doc`.
This inventory assigns every exported root-package surface by its owning file;
symbols in a listed file inherit that row’s status. Aliases re-exported by
`export.go` are part of the root API. Unexported receiver types such as the HNSW
storage implementation are not public API even if they have exported methods.

`task api-check` compares declarations, exported fields and methods, symbol
documentation, constants, and the definitions behind root aliases with
[`api/public.txt`](api/public.txt). The check is intentionally exact:
additive and documentation changes also require compatibility review before
regenerating the baseline with
`go run ./scripts/api_surface.go -write`. Passing the check prevents accidental
drift; it does not by itself authorize a breaking change.

| Status | Owning root files | Policy before v1 |
| --- | --- | --- |
| v1 candidate: core storage and records | `db.go`, `tx.go`, `options.go`, `errors.go`, `export.go`, `cells.go`, `seams.go`, `seam_scan.go`, `spatial_reads.go`, `facets_edges.go`, `delete_cell.go`, `batch.go`, `cell_secondary.go`, `seam_secondary.go`, `hooks.go` | Breaking changes require an `[Unreleased]` migration note now; compatibility is mandatory at v1. |
| v1 candidate: durability and operations | `backup.go`, `compact.go`, `reclaim.go`, `maintenance_preflight.go`, `rotation.go`, `encryption.go`, `health.go`, `storage_stats.go`, `write_stats.go`, `mvcc_lifecycle.go`, `mvcc_snapshot_floor.go`, `snapshot_tags.go`, `db_changelog.go`, `changelog_consumers.go`, `capability_status.go` | Operational failure and recovery semantics are compatibility commitments. |
| v1 candidate: embeddings and views | `embedding.go`, `tx_embedding.go`, `embedding_index_rebuild.go`, `views.go` | Stored encodings and documented root signatures are candidates for v1 commitment. |
| v1 candidate: retrieval and query | `context_load.go`, `search.go`, `query.go`, `query_exec.go`, `embedding_search.go`, `embedding_reindex.go` | Result bounds, ordering, exact/approximate path selection, and error behavior are candidate commitments. |
| v1 candidate: spatial and derived diagnostics | `fov_context.go`, `voronoi_context.go`, `pathfind_api.go`, `snapshot_diff.go`, `superhex_summary.go`, `ring_density.go`, `tag_analytics.go`, `hex_render.go` | Determinism, bounded execution, and documented retained-history limitations are candidate commitments. |
| v1 candidate: record convenience | `templates.go` | Constructor signatures and returned record shapes are candidate commitments; applications still own product policy. |

Every pre-v1 breaking change must include all of: an explicit `CHANGELOG.md`
breaking entry, affected old and new signatures or behavior, caller migration
steps, and any on-disk/open compatibility impact. A deprecation period is optional before v1.

## V1 release gates

Current status: **unmet**. A v1-candidate label in the inventory identifies a
proposed contract; it is not a completed stability or production qualification.

| Gate | Pass condition |
| --- | --- |
| Beta complete | Every beta gate has evidence for the selected profile; material later changes have affected gates rerun. |
| Compatibility commitment | Maintainers explicitly accept every supported root API signature, record shape, sentinel/error outcome, query ordering/limits, storage/open contract and recovery behavior. Remove or clearly deprecate provisional surfaces and publish migration notes. Recheck `task api-check`. |
| Sustained adoption | A named owner completes the shared 14-day controlled operational pilot, including a real application workflow embedding HexxlaDB, monitoring, a verified restore and at least one upgrade. A maintainer-owned deployment can qualify; external customer count is not a gate. |
| Release operations | Install from the distributable source, rehearse full recovery-set backup, upgrade, restore-based rollback and downgrade/unsupported-format refusal. Preserve pre-upgrade backups; never assume an older binary can open newer formats. |
| Performance and support | Regenerate the applicable controlled, observation, vector-scale and placement evidence on release hardware. Meet declared targets and publish the supported profile, known limits, security reporting route and maintenance/upgrade policy. |
| Final candidate | Freeze the dependency set and intended v1 contract; rerun candidate CI, vulnerability, crash/recovery, fuzz and build checks. Record exact SHAs, results and maintainer sign-off. A material runtime change restarts affected qualification. |

The stack's release candidate and v1 gates additionally require the exact
Mosaic/Ratchet combination to pass the shared qualification plan. No scheduled
date, feature count or version increment can waive these gates.
