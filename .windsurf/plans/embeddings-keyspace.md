# Embeddings Keyspace — HNSW-backed vector search

## Design decisions

- **One embedding per cell** — simple `embed/<packed_coord>` key.
- **Dimension enforced, model-agnostic** — `Options.EmbeddingDimension uint16` sets the vector length; persisted in header; immutable. hexxladb has zero knowledge of model names. User is responsible for using a consistent model; if they switch models (same dimension), existing embeddings are structurally valid but semantically stale — use `ReindexEmbeddings` to recompute.
- **HNSW index** persisted in B+ tree — sub-linear search, crash-safe, MVCC-aware, encrypted for free.
- **Flat-scan fallback** — works immediately before HNSW is built; also serves as correctness oracle for testing.
- **`DeleteCell` cascades** — removes embedding + HNSW node.
- **MVCC-versioned** (format v2) — `ViewAt` sees the embedding that existed at that snapshot.
- **`ReindexEmbeddings`** — bulk recompute all embeddings via user callback; handles model switching, dimension stays the same.

## Storage layout

### Embedding vectors

```
embed/<packed_coord>  →  [float32 × dim]  (raw little-endian, dim known from header)
```

Value size: `dim × 4` bytes (e.g. 768-dim = 3072 bytes, fits inline at default page size with overflow; 384-dim = 1536 bytes, fits inline easily).

### HNSW graph

```
hnsw/entry            →  [packed_coord]           entry point node
hnsw/meta             →  [M uint16][efC uint16][maxLayer uint8][count uint64]
hnsw/node/<packed_coord>  →  [layer uint8][per-layer neighbor lists]
```

Node value layout:

```
[maxLayer uint8]
[layer 0 count uint16][neighbor_0 packed_coord]...[neighbor_N packed_coord]
[layer 1 count uint16][neighbor_0 packed_coord]...[neighbor_N packed_coord]
...
```

Typical node size: M=16, 2 layers average, 16-byte packed_coord = ~520 bytes. Well within inline threshold.

### Header extension

| Offset | Size | Field                                                                |
| ------ | ---- | -------------------------------------------------------------------- |
| 104    | 2    | **embedding_dim** `uint16` — 0 = no embeddings; >0 = fixed dimension |
| 106    | 1    | **distance_metric** `uint8` — 0=cosine, 1=dot-product, 2=L2          |
| 107    | 1    | **reserved**                                                         |

Dimension and metric are immutable after creation. Mismatch on reopen → error.

hexxladb does **not** store or enforce model names. The dimension is a structural constraint (vectors must be the same length). Users are responsible for model consistency.

## Vector search: pure math, no LLM

Vector similarity is basic arithmetic — no AI, no LLM, no external dependencies:

- **Cosine**: `dot(a,b) / (‖a‖ × ‖b‖)` = `Σ(aᵢ×bᵢ) / (√Σaᵢ² × √Σbᵢ²)`
- **Dot product**: `Σ(aᵢ×bᵢ)` (assumes normalized vectors)
- **L2**: `√Σ(aᵢ-bᵢ)²` (lower = more similar; inverted for ranking)

HNSW is graph traversal + distance comparisons. The only place an LLM is involved is **outside hexxladb** — the service layer turns text → `[]float32`, then hexxladb does the rest with pure Go `math`.

## HNSW parameters

| Param              | Default | Notes                                                              |
| ------------------ | ------- | ------------------------------------------------------------------ |
| **M**              | 16      | Max bidirectional connections per layer (layers ≥ 1)               |
| **M_max0**         | M × 2   | Max connections at layer 0 (densest layer, standard HNSW practice) |
| **efConstruction** | 200     | Build-time beam width. Higher = better graph, slower insert        |
| **efSearch**       | 100     | Query-time beam width. Configurable per query                      |
| **mL**             | 1/ln(M) | Layer probability factor (standard HNSW)                           |

Stored in `hnsw/meta`. Set at first `PutEmbedding`; immutable thereafter.

### HNSW algorithms (from Malkov & Yashunin 2016)

**Insert:**

1. Pick random layer: `l = floor(-ln(rand()) × mL)`
2. From entry point, greedily descend finding nearest node at each layer (ef=1)
3. At insertion layer and below, find efConstruction nearest neighbors
4. Connect to M best neighbors using diversity heuristic (prefer neighbors that are not too close to each other — improves recall over naive closest-M selection)
5. Bidirectional links; prune if neighbor exceeds M_max / M_max0

**Search:**

1. Start at entry point (top layer)
2. Greedily descend layers (ef=1 per layer)
3. At layer 0, beam search with priority queue of size efSearch
4. Return top-K from queue, scored by distance

**Delete:**

1. Remove node from all neighbor lists across all layers
2. Repair: connect former neighbors to each other where beneficial
3. If deleted node was entry point, promote nearest neighbor at top layer

## API surface

### Options (set at Open)

```go
type Options struct {
    // ... existing ...
    EmbeddingDimension uint16         // 0 = disabled; >0 = fixed vector dimension (immutable after creation)
    DistanceMetric     DistanceMetric // Cosine (default), DotProduct, L2 (immutable after creation)
}

type DistanceMetric uint8
const (
    DistanceCosine     DistanceMetric = 0
    DistanceDotProduct DistanceMetric = 1
    DistanceL2         DistanceMetric = 2
)
```

No model name field. hexxladb is model-agnostic — it only enforces vector length.

### Write path (on Tx)

```go
Tx.PutEmbedding(coord PackedCoord, vec []float32) error
  // Validates len(vec) == db.EmbeddingDimension
  // Writes embed/<packed_coord>
  // Inserts/updates HNSW node
  // Error if EmbeddingDimension == 0

Tx.DeleteEmbedding(coord PackedCoord) error
  // Removes embed/<packed_coord> + HNSW node + neighbor links
```

### Read path (on Tx)

```go
Tx.GetEmbedding(coord PackedCoord) ([]float32, bool, error)

Tx.SearchByEmbedding(vec []float32, opts EmbeddingSearchConfig) ([]EmbeddingSearchResult, error)
  // Uses HNSW if graph exists, else flat scan

DB.EmbeddingDimension() uint16
DB.DistanceMetric() DistanceMetric
```

### Bulk reindex (on DB)

```go
// ReindexEmbeddings recomputes all embeddings via a user-provided callback.
// Use when switching embedding models (same dimension). Rebuilds HNSW graph.
type ReindexEmbeddingsConfig struct {
    // ComputeEmbedding is called for each cell. Return nil to remove the embedding.
    ComputeEmbedding func(coord PackedCoord, cell record.CellRecord) ([]float32, error)
    BatchSize  int                      // cells per write tx (default 100)
    OnProgress func(processed, total int)
}

DB.ReindexEmbeddings(cfg ReindexEmbeddingsConfig) error
```

### Search config

```go
type EmbeddingSearchConfig struct {
    MaxResults   int     // default 10
    EfSearch     int     // HNSW beam width, default 100
    MinScore     float64 // similarity threshold (0 = no filter)
    HydrateCells bool    // return CellView per result
    // Post-ANN filters
    Center      *Coord
    Radius      int
    RequireTags []string
}

type EmbeddingSearchResult struct {
    Coord PackedCoord
    Score float64
    Cell  *CellView // nil unless HydrateCells
}
```

### Integration with existing query engine

`CellSearchConfig.Embedding []float32` triggers `SearchByEmbedding` internally, results merged with lexical scoring. `QueryCells` planner picks embedding index when `Embedding` field is set.

## Implementation phases

### Phase 1: Embed keyspace + flat scan

- Header extension: `embedding_dim`, `distance_metric`
- `Options.EmbeddingDimension`, `Options.DistanceMetric`
- `DB.EmbeddingDimension()`, `DB.DistanceMetric()`
- `embed/<packed_coord>` put/get/delete
- `DeleteCell` cascade to `embed/`
- Flat-scan `SearchByEmbedding` (cosine, dot, L2)
- `DistanceMetric` type + constants
- Tests: round-trip, dimension mismatch, delete cascade, flat-scan recall
- `DB.ReindexEmbeddings` — bulk recompute via callback, progress reporting

### Phase 2: HNSW graph

- `hnsw/` keyspace: meta, entry, node records
- HNSW insert (layer selection, neighbor search, bidirectional linking)
- HNSW delete (remove node, repair neighbor links)
- HNSW search (greedy layer descent, ef-bounded beam at layer 0)
- Node encoding/decoding
- Tests: insert/search recall, delete + re-search, graph persistence across reopen

### Phase 3: Wire HNSW into search + query engine

- `SearchByEmbedding` uses HNSW when graph exists, flat scan otherwise
- Wire `CellSearchConfig.Embedding` → `SearchByEmbedding`
- `QueryCells` planner: embedding index selection
- Tests: hybrid search (embedding + tags), query planner selection

### Phase 4: Demo + docs

- Update conversational_memory demo with embedding search phase
- Update API_REFERENCE, HEXXLA_DB, ENGINE_FORMAT, CHANGELOG, doc.go
- Benchmark: flat scan vs HNSW at 1K/10K/50K vectors

## Distance functions

All operate on `[]float32`:

- **Cosine similarity**: `dot(a,b) / (‖a‖ × ‖b‖)` — range [-1, 1], higher = more similar
- **Dot product**: `dot(a,b)` — assumes normalized vectors
- **L2 (Euclidean)**: `√Σ(a_i - b_i)²` — lower = more similar (score inverted for ranking)

SIMD optimization deferred; stdlib math is sufficient for v1 cell counts.

## Risks and mitigations

| Risk                                        | Mitigation                                                              |
| ------------------------------------------- | ----------------------------------------------------------------------- |
| HNSW node updates cause write amplification | Nodes are small (~500 bytes); B+ tree handles well; M=16 bounds fan-out |
| Graph quality degrades with deletes         | Repair neighbor links on delete; periodic rebuild via Compact if needed |
| Large dimensions blow up storage            | 1536-dim × 4 = 6 KiB per vector; overflow pages handle it transparently |
| HNSW correctness                            | Flat scan as oracle in tests; recall benchmarks                         |
| Model switching                             | `ReindexEmbeddings` recomputes all vectors + rebuilds HNSW graph        |
| Dimension change                            | Requires new database; enforced by header immutability                  |
