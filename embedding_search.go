package hexxladb

import (
	"bytes"
	"container/heap"
	"fmt"
	"runtime"
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
// The database must have been opened with a non-zero [Options.EmbeddingDimension].
func (tx *Tx) SearchByEmbedding(vec []float32, cfg EmbeddingSearchConfig) ([]EmbeddingSearchResult, error) {
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if tx.db.activeEng() == nil {
		return nil, ErrDatabaseClosed
	}
	dim := tx.db.eng.EmbeddingDim()
	if dim == 0 {
		return nil, ErrEmbeddingsDisabled
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

	// Flat-scan fallback: collect all embeddings from the embed/ keyspace.
	type candidate struct {
		coord lattice.PackedCoord
		vec   []float32
	}
	var candidates []candidate
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
		candidates = append(candidates, candidate{
			coord: coord,
			vec:   decodeFloat32s(v),
		})
		return true
	})
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Phase 2: compute distances in parallel using goroutines.
	numWorkers := max(min(runtime.NumCPU(), len(candidates)), 1)

	// Each worker produces its own local top-K, then we merge.
	chunkSize := (len(candidates) + numWorkers - 1) / numWorkers
	results := make([][]searchHit, numWorkers)

	var wg sync.WaitGroup
	for w := range numWorkers {
		start := w * chunkSize
		end := min(start+chunkSize, len(candidates))
		if start >= end {
			continue
		}
		wg.Add(1)
		go func(workerIdx int, chunk []candidate) {
			defer wg.Done()
			var localHeap searchMinHeap
			for _, c := range chunk {
				s := engine.Similarity(vec, c.vec, metric)
				if cfg.MinScore != 0 && s < cfg.MinScore {
					continue
				}
				hit := searchHit{coord: c.coord, score: s}
				if localHeap.Len() < maxResults {
					heap.Push(&localHeap, hit)
				} else if s > localHeap[0].score {
					localHeap[0] = hit
					heap.Fix(&localHeap, 0)
				}
			}
			results[workerIdx] = []searchHit(localHeap)
		}(w, candidates[start:end])
	}
	wg.Wait()

	// Merge worker results into a final top-K.
	var finalHeap searchMinHeap
	for _, wr := range results {
		for _, item := range wr {
			if finalHeap.Len() < maxResults {
				heap.Push(&finalHeap, item)
			} else if item.score > finalHeap[0].score {
				finalHeap[0] = item
				heap.Fix(&finalHeap, 0)
			}
		}
	}

	// Extract results in descending score order.
	out := make([]EmbeddingSearchResult, finalHeap.Len())
	for i := len(out) - 1; i >= 0; i-- {
		item := heap.Pop(&finalHeap).(searchHit) //nolint:forcetypeassert // heap invariant guarantees searchHit
		out[i] = EmbeddingSearchResult{Coord: item.coord, Score: item.score}
	}
	return out, nil
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
