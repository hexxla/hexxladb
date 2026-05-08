# Breaking the Sorting Barrier for Directed Single-Source Shortest Paths

**Authors:** Ran Duan, Jiayi Mao, Xiao Mao, Xinkai Shu, Longhui Yin
**Date:** July 31, 2025
**arXiv:** [2504.17033v2](https://arxiv.org/abs/2504.17033)

---

## Abstract

A deterministic `O(m log^(2/3) n)` algorithm for single-source shortest paths (SSSP) on directed
graphs with real non-negative edge weights in the comparison-addition model. This is the first result
to break the `O(m + n log n)` time bound of Dijkstra's algorithm on sparse graphs, showing that
Dijkstra is not optimal for SSSP.

---

## 1. Introduction

In an `m`-edge `n`-vertex directed graph `G = (V, E)` with non-negative weight function
`w : E → R≥0`, SSSP finds the shortest path lengths from a source `s` to all `v ∈ V`.

- **Dijkstra + Fibonacci heap:** `O(m + n log n)` — comparison-addition model
- **Pettie–Ramachandran (undirected):** `O(m·α(m,n) + min{n log n, n log log r})` — hierarchy-based
- **Haeupler et al. [HHR+24]:** Dijkstra is _optimal_ if the vertex ordering by distance is required
- **Duan et al. [DMSY23]:** `O(m√(log n) log log n)` randomized for undirected — better than `O(n log n)` on sparse graphs

**This paper:** First directed-graph SSSP algorithm breaking the sorting barrier.

### Theorem 1.1

> There exists a deterministic algorithm that takes `O(m log^(2/3) n)` time to solve SSSP on
> directed graphs with real non-negative edge weights.

This is also the first _deterministic_ algorithm to break `O(m + n log n)` even for undirected graphs.

### Technical Overview

Two classical approaches:

- **Dijkstra:** Priority queue extracts min-distance vertex, relaxes outgoing edges. Sorts vertices
  by distance → `Θ(n log n)` lower bound.
- **Bellman-Ford:** Dynamic programming, relaxes all edges for `k` steps. Finds paths with ≤ k edges
  in `O(mk)` — no sorting required.

**Key idea:** Merge both via recursive partitioning. Reduce the "frontier" `S` size from `Θ(n)` to
`|Ũ| / log^Ω(1)(n)` where `Ũ` is the set of vertices of interest, avoiding the sorting bottleneck.

Given `k = log^Ω(1)(n)`, either:

1. `|Ũ| > k|S|` — frontier already has size `|Ũ|/k`; or
2. `|Ũ| ≤ k|S|` — run Bellman-Ford `k` steps from `S`; most vertices become complete, remaining
   "pivots" number at most `|Ũ|/k`

The algorithm uses divide-and-conquer with `log n / t` levels. Frontier reduction limits `Θ(t)` work
to pivots (≈ `1/log^Ω(1)(n)` of frontier), reducing cost per vertex to `log n / log^Ω(1)(n)`.

---

## 2. Preliminaries

**Setting:** Directed graph `G = (V, E)`, weight `w : E → R≥0`, source `s`. Goal: find `d(v)` for all
`v ∈ V`. Assume all vertices reachable from `s`, so `m ≥ n - 1`.

**Constant-degree reduction:** Transform `G` to `G'` with in/out-degree ≤ 2, `O(m)` vertices and
edges, preserving shortest paths:

- Replace each vertex `v` with a zero-weight strongly connected cycle
- For each edge `(u, v)`, add directed edge from `x_uv` to `x_vu` with weight `w_uv`

**Comparison-addition model:** Only comparison and addition on edge weights; each takes unit time.

**Labels:**

- `d̂[v]` — sound estimate: `d̂[v] ≥ d(v)` at all times; initialised `d̂[s] = 0`, `d̂[v] = ∞`
- **Complete:** `d̂[x] = d(x)`; **incomplete** otherwise
- `Pred[v]` — predecessor in current shortest path tree; updated on relaxation

**Total order on paths:** Treat path of length `l` through `α` vertices `v₁=s,...,vα` as tuple
`⟨l, α, vα, ..., v₁⟩`; sort lexicographically. Ties resolved in `O(1)` using `Pred[]` and depth.
Assumption 2.1: all paths have distinct lengths (no generality lost).

---

## 3. Main Result

### Parameters

```text
k := ⌊log^(1/3)(n)⌋
t := ⌊log^(2/3)(n)⌋
```

### 3.1 The Algorithm

**Core subproblem — Bounded Multi-Source Shortest Path (BMSSP):**

Given level `l`, bound `B`, and frontier set `S` (size ≤ `2^(lt)`), find all vertices `v` with
`d(v) < B` whose shortest path visits `S`, and return a boundary `B' ≤ B`.

**Lemma 3.1 (BMSSP):** `BMSSP(l, B, S)` runs in `O((kl + tl/k + t)|U|)` time and either:

- **Successful:** returns `B' = B`, `U` = all vertices with `d(v) < B` reachable via `S`
- **Partial:** returns `B' < B`, `|U| = Θ(k · 2^(lt))`

Top-level call: `BMSSP(l = ⌈log(n)/t⌉, S = {s}, B = ∞)` → finds all distances in `O(m log^(2/3) n)`.

---

#### Algorithm 1: FindPivots(B, S)

Runs `k` Bellman-Ford steps from `S` bounded by `B`. Returns:

- `W` — completed vertices (size `O(min{k|S|, |Ũ|}`)
- `P ⊆ S` — pivots, `|P| ≤ |W|/k`

```text
W ← S; W₀ ← S
for i = 1 to k:
    Wᵢ ← ∅
    for all edges (u,v) with u ∈ Wᵢ₋₁:
        if d̂[u] + w_uv ≤ d̂[v]:
            d̂[v] ← d̂[u] + w_uv
            if d̂[u] + w_uv < B: Wᵢ ← Wᵢ ∪ {v}
    W ← W ∪ Wᵢ
    if |W| > k|S|: P ← S; return P, W

F ← {(u,v) ∈ E : u,v ∈ W, d̂[v] = d̂[u] + w_uv}  // directed forest
P ← {u ∈ S : u is root of tree with ≥ k vertices in F}
return P, W
```

**Lemma 3.2:** Runs in `O(min{k²|S|, k|Ũ|})` time.

---

#### Algorithm 2: BaseCase(B, S) (l = 0)

`S = {x}` is a singleton. Run mini-Dijkstra from `x` to find the `k+1` closest vertices within
bound `B`.

```text
U₀ ← S
H ← binary heap with ⟨x, d̂[x]⟩
while H non-empty and |U₀| < k+1:
    ⟨u, d̂[u]⟩ ← H.ExtractMin()
    U₀ ← U₀ ∪ {u}
    for edge (u,v):
        if d̂[u] + w_uv ≤ d̂[v] and d̂[u] + w_uv < B:
            d̂[v] ← d̂[u] + w_uv
            H.Insert or H.DecreaseKey ⟨v, d̂[v]⟩

if |U₀| ≤ k: return B' ← B, U ← U₀
else:         return B' ← max_{v∈U₀} d̂[v], U ← {v ∈ U₀ : d̂[v] < B'}
```

---

#### Algorithm 3: BMSSP(l, B, S)

```text
if l = 0: return BaseCase(B, S)

P, W ← FindPivots(B, S)
D.Initialize(M = 2^((l-1)t), B)
D.Insert(⟨x, d̂[x]⟩) for x ∈ P
i ← 0; B'₀ ← min_{x∈P} d̂[x]; U ← ∅

while |U| < k·2^(lt) and D non-empty:
    i ← i + 1
    Bᵢ, Sᵢ ← D.Pull()
    B'ᵢ, Uᵢ ← BMSSP(l-1, Bᵢ, Sᵢ)
    U ← U ∪ Uᵢ
    K ← ∅
    for edge (u,v) with u ∈ Uᵢ:
        if d̂[u] + w_uv ≤ d̂[v]:
            d̂[v] ← d̂[u] + w_uv
            if d̂[u] + w_uv ∈ [Bᵢ, B):    D.Insert(⟨v, d̂[u]+w_uv⟩)
            elif d̂[u] + w_uv ∈ [B'ᵢ, Bᵢ): K ← K ∪ {⟨v, d̂[u]+w_uv⟩}
    D.BatchPrepend(K ∪ {⟨x, d̂[x]⟩ : x ∈ Sᵢ, d̂[x] ∈ [B'ᵢ, Bᵢ)})

return B' ← min{B'ᵢ, B}; U ← U ∪ {x ∈ W : d̂[x] < B'}
```

---

### Data Structure (Lemma 3.3)

Block-based linked list supporting up to `N` key/value pairs with parameter `M`:

| Operation         | Amortised cost                    | Notes                                                    |
| ----------------- | --------------------------------- | -------------------------------------------------------- |
| `Insert(k, v)`    | `O(max{1, log(N/M)})`             | update if key exists and new value is smaller            |
| `BatchPrepend(L)` | `O(\|L\| · max{1, log(\|L\|/M)})` | all values smaller than current min                      |
| `Pull()`          | `O(\|S'\|)`                       | returns ≤ M items with smallest values + upper bound `x` |

Implementation: two block sequences `D₀` (batch prepends) and `D₁` (inserts). Each block ≤ `M`
pairs. `D₁` block upper bounds maintained in a Red-Black tree for `O(log(N/M))` search.

---

### 3.2 Correctness

**Notation:** For vertex set `S` and bound `B`:

- `T(S)` — union of shortest-path subtrees rooted at vertices in `S`
- `T*(S)` — same restricted to complete vertices in `S`
- `T<B(S)` — `{v ∈ T(S) : d(v) < B}`

**Lemma 3.7:** After `BMSSP(l, B, S)`, the returned `U = T<B'(S)` and all vertices in `U` are
complete. (Proved by induction on `l`; base case is correctness of mini-Dijkstra.)

**Remark 3.8:** The `Uᵢ`'s from successive recursive calls are disjoint:
`Uᵢ = T[B'ᵢ₋₁, B'ᵢ)(P)`.

---

### 3.4 Time Complexity

**Lemma 3.12:** `BMSSP(l, B, S)` runs in:

```text
C(k + 2t/k)(l+1)|U| + C(t + l·log k)|N⁺[min_{x∈S} d(x), B)(U)|
```

With `k = ⌊log^(1/3)(n)⌋`, `t = ⌊log^(2/3)(n)⌋`, top-level call takes **`O(m log^(2/3) n)`**.

Cost breakdown:

- **FindPivots** across all nodes at one recursion depth: `O(nk)` → summed over `O(log n / t)` depths: `O(n log^(2/3) n)`
- **Data structure inserts** (pivots into D): `O(n log^(2/3) n)`
- **BatchPrepend** (pulled vertices back): `O(n log^(1/3) n · log log n)`
- **Direct inserts** (edge relaxations): `O(m log^(2/3) n)`
- **K-inserts via BatchPrepend**: `O(m log^(1/3) n · log log n)`

Dominant term: `O(m log^(2/3) n)`.

---

## References

- [Bel58] Bellman. _On a routing problem._ Quarterly of Applied Mathematics, 1958.
- [Dij59] Dijkstra. _A note on two problems in connexion with graphs._ Numerische Mathematik, 1959.
- [DMSY23] Duan, Mao, Shu, Yin. _Randomized SSSP on undirected real-weighted graphs._ FOCS 2023.
- [FT87] Fredman, Tarjan. _Fibonacci heaps._ JACM 34(3), 1987.
- [GT88] Gabow, Tarjan. _Algorithms for two bottleneck optimization problems._ J. Algorithms, 1988.
- [HHR+24] Haeupler, Hladík, Rozhoň, Tarjan, Tětek. _Universal optimality of Dijkstra._ FOCS 2024.
- [PR05] Pettie, Ramachandran. _Shortest path algorithm for real-weighted undirected graphs._ SIAM J. Comput., 2005.
- [Tho99] Thorup. _Undirected SSSP with positive integer weights in linear time._ J. ACM, 1999.
- [Wil64] Williams. _Algorithm 232: Heapsort._ CACM, 1964.

Full reference list: [arXiv:2504.17033](https://arxiv.org/abs/2504.17033)
