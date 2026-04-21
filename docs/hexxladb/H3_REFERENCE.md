# H3 (Uber) vs HexxlaDB — concepts and adoption notes

**Purpose:** Record what the [H3](https://github.com/uber/h3) codebase is, how it differs from HexxlaDB’s problem, and what is safe to **reuse as ideas** versus what **not** to port.
**H3 source reviewed:** local tree (e.g. `/home/anon/Documents/code/h3/`), layout typical of upstream `uber/h3` — C library under `src/h3lib/`.

## What H3 is

H3 is a **hierarchical geospatial** indexing system:

- Maps **latitude/longitude** ↔ a compact **`H3Index`** (typically **64 bits** in the C API).
- Partitions the globe using an **icosahedron** → **base cells** → recursive **aperture-7** subdivision (each cell has 7 child directions per finer resolution).
- The index is a **bit field**: mode, resolution, base cell id, then **3-bit “digits”** per resolution level (see `h3Index.h` — `H3_BC_OFFSET`, `H3_RES_OFFSET`, `H3_PER_DIGIT_OFFSET`, etc.).
- Local geometry uses **cube-style IJK** coordinates (`CoordIJK` in `coordijk.h`) on **face-centered** systems (`FaceIJK` in `faceijk.h`), with **gnomonic / hex2d** projections — not an infinite abstract axial grid.

So H3 optimizes for **Earth-scale geo**, **hierarchy (parent/child)**, and **stable global cell ids** — not for an arbitrary **flat axial lattice** with **Morton key ordering** for a custom embedded store.

## What HexxlaDB is (contrast)

Per **[HEXXLA_DB.md](./HEXXLA_DB.md)** and **[HEXXLA.md](./HEXXLA.md)**:

- **Abstract hex lattice** in **axial (q, r)** with cube constraint; **no** requirement for lat/lng or an icosahedral partition.
- **Primary key** is **`PackedCoord`**: **Morton (Z-order)** over zigzag-encoded cube coordinates — a **different encoding** from H3’s hierarchical digits.
- **Storage engine** concerns (pages, WAL, seams, MVCC path) are **out of scope** for H3 entirely.

The design spaces overlap only at the level of **“hex topology”** (neighbors, rings, distances), not at **index bit layout** or **persistence**.

## Concepts that are *similar* (useful mental models)

| Idea | In H3 | In HexxlaDB |
| --- | --- | --- |
| Cube-style coords | `CoordIJK` (i, j, k with 120° axes) on faces | Cube \((q,r,s)\), \(s=-q-r\) — same **cube distance** idea as standard flat hex |
| Neighbors / steps | `UNIT_VECS`, digit directions, `h3NeighborRotations` | Six neighbor deltas; distance \((\|dq\|+\|dr\|+\|ds\|)/2\) |
| K-ring / annulus | `gridDisk`, `gridRing`, `_kRingInternal` in `algos.c` | `WalkRing`, ring iteration — **same combinatorial shell sizes** (ring \(k\ge1\): \(6k\) cells) for a **flat** grid; **ordering** is fixed by **HEXXLA.md** (`load_context`), not H3’s CCW direction tables |
| Testing | Large test suite, fuzzers under `src/apps/fuzzers/` | Adopt **exhaustive / property tests** and **fuzzing** for lattice + decoders (see **[DEVELOPMENT_ROADMAP.md](./DEVELOPMENT_ROADMAP.md)**) |

## What is *not* portable to HexxlaDB core

- **`H3Index` encoding** — hierarchical 64-bit layout is **incompatible** with Morton **`PackedCoord`** and would fight the locked engine design.
- **Face / base-cell / digit machinery** — specific to the **globe + icosahedron**; Hexxla has no faces in this sense.
- **Lat/lng, projection, polygon fill** — product may use geo **outside** the DB; not the DB’s coordinate core.
- **Embedding H3 as the storage or key layer** — conflicts with **no third-party ordered-KV / foreign index as core** and with the **Morton-first** spec (**[HEXXLA_DB.md](./HEXXLA_DB.md)** Architecture Position).

## Optional future boundaries (orthogonal layers)

If a deployment needs **map coordinates**:

- A **separate** component could convert **lat/lng → external index** (e.g. H3) for **seeding** or UI, while **HexxlaDB** still keys **cells** by **`PackedCoord`** internally. That matches the spec’s **seed selection outside the engine** story.

## Files in a typical H3 tree worth skimming (for algorithms only)

| Area | Typical paths | Why skim |
| --- | --- | --- |
| Index layout | `src/h3lib/include/h3Index.h` | See **documented bit layout** discipline (masks, offsets) — analogous discipline needed for **PackedCoord** in code + docs |
| IJK / faces | `coordijk.h`, `faceijk.h` | Cube neighbor and face-transition **patterns** (not the encoding) |
| Grid algorithms | `algos.c`, `algos.h` | K-ring / ring traversal **structure**; compare to your **canonical ring order** in **HEXXLA.md** |
| Iterators | `iterators.c` | Systematic enumeration patterns |
| Tests / fuzz | `src/apps/testapps/`, `src/apps/fuzzers/` | **QA** patterns |

## Recommendation

- **Do not** depend on H3 for HexxlaDB’s **core lattice encoding** or **on-disk keys**.
- **Do** treat H3 as a **reference for** (1) **rigorous bit-level documentation** of a cell id, (2) **heavy testing and fuzzing** of grid operations, (3) **cube/IJK intuition** shared with standard hex math.
- **Align ring walk semantics** with **[HEXXLA.md](./HEXXLA.md)** (positive-q spiral, etc.), not with H3’s specific CCW direction tables.

---

*Upstream H3 license: Apache-2.0; this note is informative comparison only, not a derivative work requirement.*
