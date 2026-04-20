# HEXXLA readiness roadmap (consolidated)

**Audience:** HexxlaDB maintainers and Hexxla integrators preparing LLM-memory production rollout.  
**Status:** Living roadmap; update this file when scope or implementation status changes.

## Purpose

This document is the consolidated source of truth for:

- Current readiness of HexxlaDB for the `HEXXLA.md` memory model.
- Remaining engineering and operational gaps.
- Execution order (what should ship next, and why).
- Documentation cleanup decisions (what stays, merges, or is removed).

Use this roadmap instead of maintaining separate "implementation status" and "gap analysis" docs.

## Readiness snapshot

## 1) Foundation and architecture

- **Hexagonal boundaries:** in place (`internal/domain` and `internal/app` ports, outbound adapter over public `package hexxladb`).
- **Embedded engine:** shipped (`internal/engine` page store + WAL redo + replay).
- **Ordered keyspace:** shipped (B+ tree with Morton-packed coordinate ordering).
- **Public API surface:** shipped (`Open`, `View`, `Update`, `Batch`, `Tx` primitives).

**Status:** Ready for continued product integration.

## 2) Hexxla memory-model alignment

- **Cell primitives and lattice traversal:** shipped (`PutCell`, `GetCell`, `WalkRing`, `LoadContext`).
- **Seam lifecycle:** shipped (`PutSeam`, `FindSeams`, `ResolveSeam`, `MarkConflict`).
- **Facet and edge primitives:** shipped (`PutFacet`, `UpdateFacet`, `PutEdge`, `LinkCells`, scan APIs).
- **Validity-aware reads:** shipped (`WalkRingAt`, `LoadContextAt`, `FindSeamsAt`).
- **Provenance secondaries:** shipped for cells and seams (`source/`, `time/`, `seam-source/`, `seam-time/`).

**Status:** Ready for the v1 Hexxla integration contract.

## 3) Durability, encryption, and observability

- **WAL durability + crash replay:** shipped.
- **At-rest encryption:** shipped (AES-256-XTS page encryption, deterministic key mismatch detection).
- **WAL integrity hardening:** shipped (keyed MAC on WAL records).
- **Offline key rotation:** shipped (`RotateEncryption`, `RotateEncryptionWithOptions`).
- **Logical changefeed MVP:** shipped (`ReadChangelogSince`).

**Status:** Operationally usable for single-node embedded deployments.

## 4) MVCC status

- **E2+ MVP:** shipped (`format_version` 2, `commit_seq`, `ViewAt(readSeq)`, version-suffixed keys for core families).
- **Wall-clock snapshots:** shipped (`ViewAtTime(time.Time)` resolves deterministic `as_of` snapshots to `read_seq`).
- **Snapshot-aware primitive reads:** shipped for core paths.
- **Snapshot-isolated secondaries:** shipped for `source/`, `time/`, `seam-source/`, and `seam-time/`.
- **Lifecycle tooling:** initial observability + pruning APIs shipped (`StatsMVCC`, `PruneCellVersions`, `PruneCellVersionsByProfile`).

**Status:** Feature-usable, but not fully production-hardened for long-retention/high-churn workloads.

## Remaining gaps (prioritized)

## P0 (before broad production adoption)

1. **MVCC retention and reclamation policy hardening**
   - Define SLA-based retention windows.
   - Add deterministic and incremental reclaim scheduling guidance.
   - Add stress validation for sustained churn and bounded file growth.

2. **Changefeed operational reliability guidance**
   - Publish explicit failure/reconciliation runbook (append failure after data commit, consumer replay strategy).
   - Add operational metrics recommendations (lag, tail corruption handling, retry windows).

## P1 (strongly recommended near-term)

1. **Scale and performance confidence**
   - Expand stress/soak matrix for large datasets and mixed workloads.
   - Track ring/context latency under facet/seam-heavy loads.
   - Track encrypted + MVCC contention scenarios in repeatable benches.

2. **Operator playbooks**
   - Add concrete backup/restore drills (clean shutdown and crash-recovery scenarios).
   - Add incident runbooks for key mismatch, WAL MAC errors, and changelog corruption.

3. **Documentation polish for external integrators**
   - Keep one canonical readiness page (this document).
   - Keep integration contract/checklist synchronized with shipped APIs.

## P2 (later product evolution)

1. **Materialized-view and derived-index workflows** on top of changefeed.
2. **Advanced lifecycle automation** (policy-driven pruning scheduling APIs/tooling).
3. **Future spec areas** currently out of v1 (embedding partitions, scale-out replication/tiering, super-hex sharding).

## Optional API Surface Improvements (Post-v1)

### Native View Types & Client Assembly

**Current State:** Codebase provides efficient wire formats (`record.CellRecord`, `record.SeamRecord`) with separate query APIs. HEXXLA.md spec expects aggregated view types (`CellView`, `ContextPack`, `SeamRef`).

**Optional Improvements:**

#### 1. Native View Types

```go
// CellView provides read-only aggregated view matching HEXXLA.md spec
type CellView struct {
    Coord        lattice.Coord
    RawContent   string
    Provenance   record.ProvenanceWire
    Validity     record.ValidityWire
    Tags         []string
    ClusterHint  *lattice.Coord
    Facets       []FacetView
    ActiveFacet  int
    Edges        []EdgeView
    Seams        []SeamRef
}

// ContextPack provides token-budgeted context matching HEXXLA.md spec
type ContextPack struct {
    Cells       []CellView
    TotalTokens int
    Seams       []Seam
}

// SeamRef provides lightweight seam reference for CellView
type SeamRef struct {
    ID               string
    SeamType         string
    ResolutionStatus   string
}
```

#### 2. Cell Aggregation Methods

```go
// AssembleCellView creates aggregated CellView from wire records + related data
func (tx *Tx) AssembleCellView(ctx context.Context, coord lattice.Coord, asOf *time.Time) (CellView, error)

// LoadContextPack returns token-budgeted ContextPack matching HEXXLA.md spec
func (tx *Tx) LoadContextPack(ctx context.Context, center lattice.Coord, maxR, maxTokens int, filters Filter) (ContextPack, error)
```

#### 3. Token Budgeting Implementation

```go
// TokenBudgeter provides token counting and truncation logic
type TokenBudgeter interface {
    CountTokens(content string) int
    TruncateToTokenBudget(items []CellView, maxTokens int) []CellView
}

// LoadContextWithBudgeting implements token-aware context loading
func (tx *Tx) LoadContextWithBudgeting(ctx context.Context, center lattice.Coord, maxR, maxTokens int, budgeter TokenBudgeter) (ContextPack, error)
```

**Benefits:**

- **Client convenience:** Eliminates manual assembly of view types
- **Spec compliance:** Direct match to HEXXLA.md expected API surface
- **Token efficiency:** Native budgeting with automatic truncation
- **Performance:** Server-side aggregation reduces round trips

**Implementation Priority:** P2 (post-production) - non-critical for core functionality

## Execution roadmap

## Phase R1 - MVCC operations hardening (1-2 weeks, P0)

**Steps:**

1. **Define retention policies**
   - Add configurable retention windows (`RetentionWindow` option)
   - Implement default profiles by workload class
   - Document SLA guidance

2. **Add deterministic scheduling**
   - Create `PruneScheduler` with configurable intervals
   - Add incremental reclaim logic
   - Implement background pruning hooks

3. **Stress validation**
   - Create long-run churn tests (`100k+` updates)
   - Validate bounded file growth under sustained load
   - Add storage growth monitoring

**Exit criteria:** Stable storage growth behavior under sustained updates, documented operator defaults.

## Phase R2 - Temporal semantics and observability (1-2 weeks, P0)

**Steps:**

1. **Lock down temporal semantics**
   - Finalize commit-seq/time mapping decisions
   - Add deterministic snapshot resolution guarantees
   - Document temporal behavior contract

2. **Add telemetry guidance**
   - Define snapshot usage metrics
   - Add changelog lag monitoring
   - Create operational dashboards spec

3. **Recovery drills**
   - Document expected failure modes
   - Create recovery procedures
   - Add automated recovery validation

**Exit criteria:** Predictable temporal semantics with actionable runtime observability.

## Phase R3 - Production runbooks and acceptance gates (2-3 weeks, P1)

**Steps:**

1. **Production acceptance checklist**
   - Define acceptance criteria matrix
   - Create validation scripts
   - Document environment requirements

2. **Backup/restore validation**
   - Implement clean shutdown drills
   - Add crash-recovery scenarios
   - Validate encryption key rotation

3. **Benchmark reproducibility**
   - Standardize benchmark environments
   - Add performance regression guards
   - Create stress test matrices

**Exit criteria:** Merge-ready production handbook with repeatable verification steps.

## Phase R4 - Scale & performance hardening (2-4 weeks, P1 - optional)

**Steps:**

1. **Expand stress testing**
   - Large dataset validation (`1M+` cells)
   - Mixed workload scenarios
   - Resource utilization profiling

2. **Performance optimization**
   - Latency tracking under load
   - Memory usage optimization
   - I/O pattern improvements

3. **Contention scenarios**
   - Encrypted + MVCC benchmarks
   - Concurrency stress tests
   - Resource exhaustion handling

**Exit criteria:** Production-ready performance characteristics under target workloads.

## Documentation cleanup decisions

| Document                                                                    | Decision   | Reason                                                                                    |
| --------------------------------------------------------------------------- | ---------- | ----------------------------------------------------------------------------------------- |
| `SPEC_IMPLEMENTATION_STATUS.md`                                             | **Delete** | Overlaps with this consolidated roadmap and drifts quickly.                               |
| `SPEC_GAP_ANALYSIS_AND_INTEGRATION_PLAN.md`                                 | **Delete** | Historical and internally conflicting snapshots; content folded into this roadmap.        |
| `DEVELOPMENT_ROADMAP.md`                                                    | **Keep**   | Milestone chronology and architecture sequence remain useful.                             |
| `HEXXLA_DB.md`                                                              | **Keep**   | Normative storage/database spec.                                                          |
| `HEXXLA.md`                                                                 | **Keep**   | Product memory-model spec.                                                                |
| `TX.md`, `ENCRYPTION.md`, `CHANGEFEED.md`, `BENCHMARKS.md`, `OPERATIONS.md` | **Keep**   | Focused operational/API references; now cross-reference this roadmap for readiness state. |

## Maintenance rule

When behavior, scope, or production-readiness assumptions change:

1. Update this roadmap in the same PR (or immediate follow-up).
2. Update any affected focused docs (`TX.md`, `ENCRYPTION.md`, `OPERATIONS.md`, etc.).
3. Remove stale "planned" wording for shipped items.
