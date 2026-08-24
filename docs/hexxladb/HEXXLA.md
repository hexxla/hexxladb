# Hexxla memory model

Hexxla is a spatial model for long-term machine memory. It organizes memories on a deterministic hexagonal lattice so locality, neighborhood traversal, provenance, validity, and contradictions remain inspectable.

This document defines product and domain concepts. It does not define Go APIs, physical keys, page formats, or operational procedures:

- [`API_REFERENCE.md`](./API_REFERENCE.md) maps application tasks to the public Go API.
- [`HEXXLA_DB.md`](./HEXXLA_DB.md) defines the HexxlaDB storage model and key families.
- [`TX.md`](./TX.md) defines transaction and temporal snapshot behavior.

## Goals

Hexxla combines two retrieval stages:

1. Semantic, lexical, indexed, or explicit selection identifies one or more seed coordinates.
2. Deterministic lattice operations expand, filter, and pack nearby memory into a bounded context.

This separation allows seed selection to evolve without changing the spatial memory model. It also makes the expansion stage reproducible and explainable.

Hexagonal geometry is useful because every location has six symmetric neighbors, distance is exact, rings are deterministic, and aperture-based hierarchy can summarize larger regions.

## Geometry

Cells use axial coordinates `(q, r)` with implicit cube coordinate `s = -q - r`.

```text
distance(a, b) =
  (abs(a.q - b.q)
   + abs(a.r - b.r)
   + abs((a.q + a.r) - (b.q + b.r))) / 2
```

Distance is the minimum number of neighbor steps between two cells. A ring at radius `n` contains coordinates at exactly that distance; a bounded neighborhood contains rings `0..n` in deterministic order.

## Placement policy and invariants

HexxlaDB stores the coordinate an application supplies; it does not infer semantic position, relocate cells, or interpret `ClusterHint` as an allocation instruction. Meaningful proximity therefore depends on an explicit application placement policy.

A reproducible policy should maintain these invariants:

1. Assign one logical record to one unoccupied coordinate. Writing at an occupied coordinate intentionally replaces the visible record, so applications that mean “insert” must check occupancy before writing.
2. Choose collision candidates in a stable order. A fixed topic anchor followed by deterministic concentric-ring order provides reproducible first-free allocation.
3. Never shift existing coordinates during ordinary insertion. Append new records at the next free candidate so stored links, external references, and neighborhood explanations remain stable.
4. Treat `ClusterHint` as caller-owned metadata. HexxlaDB persists it but does not validate the hint, place the cell near it, or keep hinted groups together.
5. Model relocation as lifecycle history: create the successor at a new free coordinate, write its semantic and provenance metadata, and record an explicit directional supersession in the same update. Keep the predecessor addressable for audit; context assembly substitutes the successor only when the caller requests supersession filtering.

Applications should retain a stable record-ID-to-coordinate mapping or enough record metadata to reconstruct it. Collision resolution, semantic clustering, anchor selection, relocation triggers, and acceptable neighborhood-quality thresholds are product policy, not database guarantees.

Inspect placement through deterministic neighborhood walks, bounded context assembly, semantic-neighbor queries, labelled grids, and density summaries. Compare semantic-neighbor topic distribution with lattice-neighbor distribution rather than assuming that high semantic-retrieval quality implies useful spatial neighborhoods. The repeatable reference evaluation is documented in [`PERFORMANCE_EVIDENCE.md`](./PERFORMANCE_EVIDENCE.md#lattice-placement-evidence).

## Core objects

### Cell

A cell is one addressable memory unit. It contains:

- immutable-or-superseded raw content;
- provenance describing its source and confidence;
- an optional validity interval;
- tags and an optional clustering hint;
- separately stored facets, edges, and seams assembled into views when needed.

Applications may replace content at a coordinate, but product workflows that require an audit-friendly history should create a successor cell and link the old and new cells explicitly. MVCC preserves storage snapshots; it does not decide product supersession policy.

### Facet

A facet is a derived view of a cell, such as a summary, procedural interpretation, or project-specific lens. Facets do not replace raw content.

The model reserves six facet positions:

| Facet | Meaning                                                                      |
| ----- | ---------------------------------------------------------------------------- |
| 0     | Raw content anchor; represented by the cell rather than a separate facet row |
| 1     | Semantic summary                                                             |
| 2     | Conflict or contradiction notes                                              |
| 3     | Temporal interpretation                                                      |
| 4     | Procedural or action-oriented view                                           |
| 5     | Product-specific view                                                        |

Derived facets carry a hash of the source content. An update is valid only while that hash still matches the cell, preventing a stale derivation from silently attaching to changed source material.

### Edge

An edge is a lightweight directed relationship used for graph traversal. It has a relation type, weight, and provenance. Edges may be numerous and are optimized for adjacency and path operations.

### Seam

A seam is a first-class contradiction or supersession artifact between cells. It records why the relationship exists, its confidence effect, when it was detected, and how it was resolved.

Edges and seams are deliberately different:

- an edge says that two memories are related;
- a seam says that their relationship requires conflict-aware interpretation or lifecycle handling.

Seams remain visible and queryable. Resolving a seam records a status and note; it does not erase the contradiction history.

### Provenance

Provenance identifies the source of a memory and its confidence. Source identity enables indexed discovery and audit. Confidence is evidence supplied by the application; HexxlaDB does not infer truth from it.

### Validity

A validity interval describes when a cell or seam is true in the modeled domain. It is distinct from database snapshot time:

- validity answers “when does this fact apply?”;
- an MVCC snapshot answers “what had the database committed by this point?”

Applications may combine both filters when reconstructing historical context.

## Retrieval and context assembly

A typical context request follows this pipeline:

```text
seed selection
  -> spatial or graph expansion
  -> validity, provenance, tag, and seam filters
  -> ranking and facet assembly
  -> token-budgeted context
```

Seed selection may use embeddings, text search, tags, source identifiers, an explicit coordinate, or an application-defined policy. The memory model does not require embeddings.

Expansion strategies serve different structures:

- concentric rings for ordinary local context;
- visibility filtering where empty or policy-blocked cells form boundaries;
- edge traversal for explicit relationships;
- Voronoi partitioning for multiple competing seeds;
- hierarchical occupancy summaries for deciding which larger regions merit inspection.

Token budgeting happens after eligibility and assembly. Distance, confidence, and product policy may influence eviction, but the policy must remain explicit to callers.

## Contradiction workflow

HexxlaDB provides explicit seam creation and resolution. Detection policy belongs to the embedding application because it may require models, provenance rules, or product-specific thresholds.

A normal workflow is:

1. Detect a possible conflict outside the storage engine.
2. Write a seam connecting the relevant cells.
3. Keep both memories visible during review.
4. Resolve the seam as merged, superseded, archived, or another application-defined status.
5. Preserve the resolution record for later explanation and audit.

HexxlaDB does not perform automatic truth assessment or background contradiction detection.

## Hierarchy

Aperture-7 parent regions provide a natural hierarchy over the lattice. The current storage library offers rebuildable, process-local occupancy summaries that answer whether a region contains visible cells and how densely it is populated.

Persistent or content-bearing regional summaries are not part of the memory model's required baseline. They should be introduced only when representative workloads establish their aggregation, freshness, and storage semantics; candidates are tracked in [`ROADMAP.md`](../ROADMAP.md).

## Product boundary

This repository ships the embedded HexxlaDB library and operator tools. A complete Hexxla product may additionally provide model integration, seed-selection services, HTTP or tool protocols, dashboards, and application policy. Those components consume the public database API and must not depend on its private engine packages.

## Non-goals

- Automatic truth adjudication or global relevance judgment.
- A replacement for general-purpose graph or vector databases.
- Hidden background mutation of confidence, edges, or memory content.
- A requirement that every application use embeddings or hierarchical summaries.
- Experimental dynamics without explicit semantics and representative evidence.

## Evaluation

Evaluate a Hexxla-based system using observable product outcomes:

- useful context per token;
- deterministic and explainable neighborhood selection;
- contradiction visibility and resolution quality;
- retrieval latency under representative occupancy and graph shapes;
- operator ability to inspect, back up, and recover the memory store.

Reproducible database-level performance evidence is documented in [`PERFORMANCE_EVIDENCE.md`](./PERFORMANCE_EVIDENCE.md).
