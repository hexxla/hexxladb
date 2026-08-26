package hexxladb

import (
	"bytes"
	"container/heap"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"

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
			return results, EmbeddingSearchStats{Path: EmbeddingSearchPathHNSW, EfSearch: efSearch}, nil
		}
	}

	// Flat-scan fallback.
	results, err := tx.flatScanEmbeddings(vec, maxResults, cfg.MinScore, metric)
	return results, EmbeddingSearchStats{Path: EmbeddingSearchPathFlat}, err
}

// flatScanCandidate pairs a coordinate with its embedding vector.
type flatScanCandidate struct {
	coord lattice.PackedCoord
	vec   []float32
}

// flatScanEmbeddings collects all embeddings and scores them in parallel.
func (tx *Tx) flatScanEmbeddings(vec []float32, maxResults int, minScore float64, metric engine.DistanceMetric) ([]EmbeddingSearchResult, error) {
	candidates, err := tx.collectEmbeddingCandidates()
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	hits := flatScanParallelScore(candidates, vec, maxResults, minScore, metric)
	return heapToResults(hits, maxResults), nil
}

// collectEmbeddingCandidates scans the embed/ keyspace and returns all stored vectors.
func (tx *Tx) collectEmbeddingCandidates() ([]flatScanCandidate, error) {
	var candidates []flatScanCandidate
	var scanErr error
	dimension := tx.db.eng.EmbeddingDim()
	from := []byte(index.EmbedPrefix)
	to := index.EmbedKeyEnd()
	err := tx.db.btree.AscendRange(from, to, func(k, v []byte) bool {
		if !bytes.HasPrefix(k, []byte(index.EmbedPrefix)) {
			return false
		}
		coord, parseErr := index.ParseEmbedKey(k)
		if parseErr != nil {
			scanErr = fmt.Errorf("%w: invalid embedding key: %w", ErrCorruptDatabase, parseErr)
			return false
		}
		vector, decodeErr := decodeEmbedding(v, dimension)
		if decodeErr != nil {
			scanErr = decodeErr
			return false
		}
		candidates = append(candidates, flatScanCandidate{
			coord: coord,
			vec:   vector,
		})
		return true
	})
	if err != nil {
		return nil, err
	}
	if scanErr != nil {
		return nil, scanErr
	}
	return candidates, nil
}

// flatScanParallelScore computes similarity in parallel using worker goroutines,
// each maintaining a local top-K min-heap, then merges results.
func flatScanParallelScore(candidates []flatScanCandidate, vec []float32, maxResults int, minScore float64, metric engine.DistanceMetric) searchMinHeap {
	numWorkers := max(min(runtime.NumCPU(), len(candidates)), 1)
	chunkSize := (len(candidates) + numWorkers - 1) / numWorkers
	workerResults := make([][]searchHit, numWorkers)

	var wg sync.WaitGroup
	for w := range numWorkers {
		start := w * chunkSize
		end := min(start+chunkSize, len(candidates))
		if start >= end {
			continue
		}
		wg.Go(func() {
			var localHeap searchMinHeap
			for _, c := range candidates[start:end] {
				s := engine.Similarity(vec, c.vec, metric)
				if minScore != 0 && s < minScore {
					continue
				}
				pushOrReplace(&localHeap, searchHit{coord: c.coord, score: s}, maxResults)
			}
			workerResults[w] = []searchHit(localHeap)
		})
	}
	wg.Wait()

	return mergeSearchHits(workerResults, maxResults)
}

// mergeSearchHits merges per-worker hit slices into a single top-K min-heap.
func mergeSearchHits(workerResults [][]searchHit, maxResults int) searchMinHeap {
	var finalHeap searchMinHeap
	for _, wr := range workerResults {
		for _, item := range wr {
			pushOrReplace(&finalHeap, item, maxResults)
		}
	}
	return finalHeap
}

// pushOrReplace pushes hit onto the heap if under capacity, or replaces the
// minimum if hit.score is better.
func pushOrReplace(h *searchMinHeap, hit searchHit, maxSize int) {
	if h.Len() < maxSize {
		heap.Push(h, hit)
	} else if hit.score > (*h)[0].score {
		(*h)[0] = hit
		heap.Fix(h, 0)
	}
}

// heapToResults extracts results from a min-heap in descending score order.
func heapToResults(h searchMinHeap, _ int) []EmbeddingSearchResult {
	out := make([]EmbeddingSearchResult, h.Len())
	for i := range slices.Backward(out) {
		item := heap.Pop(&h).(searchHit) //nolint:forcetypeassert // heap invariant guarantees searchHit
		out[i] = EmbeddingSearchResult{Coord: item.coord, Score: item.score}
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
	g := hnsw.NewGraph(stor, metric)
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

// searchHit is a scored embedding result used internally for top-K selection.
type searchHit struct {
	coord lattice.PackedCoord
	score float64
}

// searchMinHeap is a min-heap on score for top-K selection.
type searchMinHeap []searchHit

func (h searchMinHeap) Len() int           { return len(h) }
func (h searchMinHeap) Less(i, j int) bool { return h[i].score < h[j].score }
func (h searchMinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *searchMinHeap) Push(x any) {
	*h = append(*h, x.(searchHit)) //nolint:forcetypeassert // heap contract
}

func (h *searchMinHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}
