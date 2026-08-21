package hexxladb

import (
	"bytes"
	"container/heap"
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
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if tx.db.activeEng() == nil {
		return nil, ErrDatabaseClosed
	}
	if err := validateEmbeddingVector(vec); err != nil {
		return nil, err
	}
	dim := tx.db.eng.EmbeddingDim()
	if dim == 0 {
		return nil, nil // no embeddings stored yet
	}
	if uint16(len(vec)) != dim { //nolint:gosec // len(vec) bounded by uint16 max
		return nil, fmt.Errorf("%w: want %d, got %d", ErrEmbeddingDimension, dim, len(vec))
	}
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	metric := tx.db.eng.EmbeddingMetric()

	// Try HNSW graph first.
	if results, used, err := tx.searchHNSW(vec, maxResults, cfg.MinScore, metric); err != nil {
		return nil, err
	} else if used {
		return results, nil
	}

	// Flat-scan fallback.
	return tx.flatScanEmbeddings(vec, maxResults, cfg.MinScore, metric)
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
	from := []byte(index.EmbedPrefix)
	to := index.EmbedKeyEnd()
	err := tx.db.btree.AscendRange(from, to, func(k, v []byte) bool {
		if !bytes.HasPrefix(k, []byte(index.EmbedPrefix)) {
			return false
		}
		coord, parseErr := index.ParseEmbedKey(k)
		if parseErr != nil {
			return true // skip malformed
		}
		candidates = append(candidates, flatScanCandidate{
			coord: coord,
			vec:   decodeFloat32s(v),
		})
		return true
	})
	if err != nil {
		return nil, err
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
		wg.Add(1)
		go func(workerIdx int, chunk []flatScanCandidate) {
			defer wg.Done()
			var localHeap searchMinHeap
			for _, c := range chunk {
				s := engine.Similarity(vec, c.vec, metric)
				if minScore != 0 && s < minScore {
					continue
				}
				pushOrReplace(&localHeap, searchHit{coord: c.coord, score: s}, maxResults)
			}
			workerResults[workerIdx] = []searchHit(localHeap)
		}(w, candidates[start:end])
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
func (tx *Tx) searchHNSW(vec []float32, maxResults int, minScore float64, metric engine.DistanceMetric) ([]EmbeddingSearchResult, bool, error) {
	stor := &txHNSWStorage{tx: tx}
	_, hasMeta, err := stor.GetHNSWMeta()
	if err != nil {
		return nil, false, err
	}
	if !hasMeta {
		return nil, false, nil // no graph — fall back to flat scan
	}
	g := hnsw.NewGraph(stor, metric)
	efSearch := max(
		// reasonable default: 2× k
		maxResults*2, 100)
	hnswResults, err := g.Search(vec, maxResults, efSearch)
	if err != nil {
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
