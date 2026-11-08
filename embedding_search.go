package hexxladb

import (
	"bytes"
	"container/heap"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/hnsw"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// EmbeddingSearchConfig configures [Tx.SearchByEmbedding].
type EmbeddingSearchConfig struct {
	// MaxResults is the maximum number of results to return (default 10).
	MaxResults int
	// MinScore filters results below this similarity threshold (0 = no filter).
	MinScore float64
	// EfSearch controls HNSW query breadth. Zero selects a bounded
	// dimension-aware default; larger values trade latency and allocation for
	// recall. Values above 10000 are rejected.
	EfSearch int
}

// EmbeddingSearchPath identifies the execution path used for one vector query.
type EmbeddingSearchPath string

const (
	// EmbeddingSearchPathNone means no vector index was configured.
	EmbeddingSearchPathNone EmbeddingSearchPath = "none"
	// EmbeddingSearchPathHNSW means the persisted approximate graph served the query.
	EmbeddingSearchPathHNSW EmbeddingSearchPath = "hnsw"
	// EmbeddingSearchPathFlat means no graph existed and the exact flat scan served the query.
	EmbeddingSearchPathFlat EmbeddingSearchPath = "flat"
)

// EmbeddingSearchStats describes how one vector query was served.
type EmbeddingSearchStats struct {
	Path     EmbeddingSearchPath
	EfSearch int
}

// EmbeddingSearchResult is a single result from [Tx.SearchByEmbedding].
type EmbeddingSearchResult struct {
	Coord lattice.PackedCoord
	Score float64
}

// SearchByEmbedding finds the cells whose embeddings are most similar to vec.
// Uses HNSW graph when available, falling back to flat scan over the embed/ keyspace.
// Returns empty results if no embeddings have been stored yet (dimension not configured).
func (tx *Tx) SearchByEmbedding(vec []float32, cfg EmbeddingSearchConfig) ([]EmbeddingSearchResult, error) {
	results, _, err := tx.SearchByEmbeddingWithStats(vec, cfg)
	return results, err
}

// SearchByEmbeddingWithStats is [Tx.SearchByEmbedding] with execution-path
// observability for profiling and operational metrics.
func (tx *Tx) SearchByEmbeddingWithStats(vec []float32, cfg EmbeddingSearchConfig) ([]EmbeddingSearchResult, EmbeddingSearchStats, error) {
	return tx.searchByEmbedding(context.Background(), vec, cfg)
}

func (tx *Tx) searchByEmbedding(ctx context.Context, vec []float32, cfg EmbeddingSearchConfig) ([]EmbeddingSearchResult, EmbeddingSearchStats, error) {
	if err := ctx.Err(); err != nil {
		return nil, EmbeddingSearchStats{}, err
	}
	if tx == nil || tx.db == nil {
		return nil, EmbeddingSearchStats{}, ErrClosed
	}
	if tx.db.activeEng() == nil {
		return nil, EmbeddingSearchStats{}, ErrDatabaseClosed
	}
	if err := validateEmbeddingVector(vec); err != nil {
		return nil, EmbeddingSearchStats{}, err
	}
	if cfg.EfSearch < 0 || cfg.EfSearch > 10_000 {
		return nil, EmbeddingSearchStats{}, fmt.Errorf("%w: EfSearch must be between 0 and 10000", ErrInvalidArgument)
	}
	dim := tx.db.eng.EmbeddingDim()
	if dim == 0 {
		return nil, EmbeddingSearchStats{Path: EmbeddingSearchPathNone}, nil
	}
	if uint16(len(vec)) != dim { //nolint:gosec // len(vec) bounded by uint16 max
		return nil, EmbeddingSearchStats{}, fmt.Errorf("%w: want %d, got %d", ErrEmbeddingDimension, dim, len(vec))
	}
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	metric := tx.db.eng.EmbeddingMetric()
	efSearch := cfg.EfSearch
	if efSearch == 0 {
		efSearch = max(maxResults*2, min(max(len(vec), 100), 400))
	}
	efSearch = max(efSearch, maxResults)

	state, stateFound, err := tx.loadEmbeddingIndexState()
	if err != nil {
		return nil, EmbeddingSearchStats{}, err
	}
	if !stateFound || !state.dirty {
		// Try HNSW graph first when the persisted lifecycle state says it is current.
		if results, used, searchErr := tx.searchHNSW(vec, maxResults, cfg.MinScore, efSearch, metric); searchErr != nil {
			return nil, EmbeddingSearchStats{}, searchErr
		} else if used {
			return results, EmbeddingSearchStats{Path: EmbeddingSearchPathHNSW, EfSearch: efSearch}, ctx.Err()
		}
	}

	// Flat-scan fallback.
	results, err := tx.flatScanEmbeddings(ctx, vec, maxResults, cfg.MinScore, metric)
	return results, EmbeddingSearchStats{Path: EmbeddingSearchPathFlat}, err
}

// flatScanEmbeddings scores vectors as they are read and retains only the top K.
func (tx *Tx) flatScanEmbeddings(ctx context.Context, vec []float32, maxResults int, minScore float64, metric engine.DistanceMetric) ([]EmbeddingSearchResult, error) {
	var hits searchMinHeap
	var haveCandidates bool
	similarity := engine.SimilarityFunc(metric)
	err := tx.scanEmbeddings(ctx, func(coord lattice.PackedCoord, candidate []float32) error {
		haveCandidates = true
		score := similarity(vec, candidate)
		if minScore != 0 && score < minScore {
			return nil
		}
		pushOrReplace(&hits, EmbeddingSearchResult{Coord: coord, Score: score}, maxResults)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !haveCandidates {
		return nil, nil
	}

	return heapToResults(hits), nil
}

// scanEmbeddings visits the embed/ keyspace. The decoded vector is valid only
// until the visitor returns.
func (tx *Tx) scanEmbeddings(ctx context.Context, visit func(lattice.PackedCoord, []float32) error) error {
	var vector []float32
	var scanErr error
	dimension := tx.db.eng.EmbeddingDim()
	from := []byte(index.EmbedPrefix)
	to := index.EmbedKeyEnd()
	err := tx.AscendRange(from, to, func(k, v []byte) bool {
		if scanErr = ctx.Err(); scanErr != nil {
			return false
		}
		if !bytes.HasPrefix(k, from) {
			return false
		}
		coord, parseErr := index.ParseEmbedKey(k)
		if parseErr != nil {
			scanErr = fmt.Errorf("%w: invalid embedding key: %w", ErrCorruptDatabase, parseErr)
			return false
		}
		vector, scanErr = decodeEmbeddingWithBuffer(v, dimension, vector)
		if scanErr != nil {
			return false
		}
		scanErr = visit(coord, vector)
		return scanErr == nil
	})
	if err != nil {
		return err
	}
	if scanErr != nil {
		return scanErr
	}
	return ctx.Err()
}

// pushOrReplace pushes hit onto the heap if under capacity, or replaces the
// least competitive hit.
func pushOrReplace(h *searchMinHeap, hit EmbeddingSearchResult, maxSize int) {
	if h.Len() < maxSize {
		heap.Push(h, hit)
	} else if worseEmbeddingHit((*h)[0], hit) {
		(*h)[0] = hit
		heap.Fix(h, 0)
	}
}

// heapToResults extracts results in descending score order.
func heapToResults(h searchMinHeap) []EmbeddingSearchResult {
	out := make([]EmbeddingSearchResult, h.Len())
	for i := range slices.Backward(out) {
		out[i] = heap.Pop(&h).(EmbeddingSearchResult) //nolint:forcetypeassert // heap invariant guarantees EmbeddingSearchResult
	}
	return out
}

// searchHNSW attempts HNSW search. Returns (results, true, nil) if graph exists,
// or (nil, false, nil) if no graph is available (caller should fall back to flat scan).
func (tx *Tx) searchHNSW(vec []float32, maxResults int, minScore float64, efSearch int, metric engine.DistanceMetric) ([]EmbeddingSearchResult, bool, error) {
	stor := &txHNSWStorage{tx: tx}
	_, hasMeta, err := stor.GetHNSWMeta()
	if err != nil {
		return nil, false, err
	}
	if !hasMeta {
		return nil, false, nil // no graph — fall back to flat scan
	}
	g := hnsw.NewGraph(stor, engine.SimilarityFunc(metric))
	hnswResults, err := g.Search(vec, maxResults, efSearch)
	if err != nil {
		if errors.Is(err, hnsw.ErrCorruptGraph) {
			return nil, false, fmt.Errorf("%w: %w", ErrCorruptDatabase, err)
		}
		return nil, false, err
	}
	if hnswResults == nil {
		return nil, true, nil
	}
	out := make([]EmbeddingSearchResult, 0, len(hnswResults))
	for _, r := range hnswResults {
		if minScore != 0 && r.Score < minScore {
			continue
		}
		out = append(out, EmbeddingSearchResult{Coord: r.Coord, Score: r.Score})
	}
	if len(out) == 0 {
		return nil, true, nil
	}
	return out, true, nil
}

// worseEmbeddingHit breaks score ties in favor of the earlier physical key.
func worseEmbeddingHit(a, b EmbeddingSearchResult) bool {
	if a.Score != b.Score {
		return a.Score < b.Score
	}
	return b.Coord.Less(a.Coord)
}

// searchMinHeap is a min-heap on score for top-K selection.
type searchMinHeap []EmbeddingSearchResult

func (h searchMinHeap) Len() int           { return len(h) }
func (h searchMinHeap) Less(i, j int) bool { return worseEmbeddingHit(h[i], h[j]) }
func (h searchMinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *searchMinHeap) Push(x any) {
	*h = append(*h, x.(EmbeddingSearchResult)) //nolint:forcetypeassert // heap contract
}

func (h *searchMinHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}
