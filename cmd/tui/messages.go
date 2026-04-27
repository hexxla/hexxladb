package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// ─── message types ──────────────────────────────────────────────────────────

type cellsLoadedMsg struct {
	cells []hexxladb.CellView
}

type inspectCellMsg struct {
	coord hexxladb.Coord
}

type contextPackLoadedMsg struct {
	pack hexxladb.ContextPack
	err  error
}

type seamsLoadedMsg struct {
	seams []seamRow
	err   error
}

type seamRow struct {
	id     string
	coordA hexxladb.Coord
	coordB hexxladb.Coord
	stype  string
	reason string
	status string
}

type healthLoadedMsg struct {
	report *hexxladb.HealthReport
	err    error
}

type snapshotDiffMsg struct {
	diff *hexxladb.SnapshotDiff
	err  error
}

type analyticsLoadedMsg struct {
	tagCounts   []hexxladb.TagCount
	tagPairs    []hexxladb.TagPair
	ringDensity []hexxladb.RingDensity
	mvccStats   hexxladb.MVCCStats
	cellCount   int
	seamCount   int
}

type embeddingsLoadedMsg struct {
	embedCount  int
	dimension   uint16
	metric      hexxladb.DistanceMetric
	hnswEnabled bool
	err         error
}

type embeddingSearchHitsLoadedMsg struct {
	hits []searchResult
	err  error
}

// ─── cell loading helper (shared by cells view and main.go tab switch) ───────

// searchResult pairs a CellView with its relevance score from SearchCells.
type searchResult struct {
	cell  hexxladb.CellView
	score float64
}

// searchCells uses SearchCells (lexical ranking) to find cells matching query.
// Results are ordered by relevance score descending.
func searchCells(db *hexxladb.DB, query string, limit int) []searchResult {
	var out []searchResult
	_ = db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.SearchCells(context.Background(), hexxladb.CellSearchConfig{
			Query:         query,
			MaxResults:    limit,
			MaxScanRadius: 64,
		})
		if err != nil {
			return err
		}
		for _, r := range results {
			out = append(out, searchResult{cell: r.Cell, score: r.Score})
		}
		return nil
	})
	return out
}

// searchByEmbedding calls Ollama to embed the query text, then SearchByEmbedding.
// Returns embeddingSearchHitsLoadedMsg which is handled by the cells view.
func searchByEmbedding(db *hexxladb.DB, query string, limit int) embeddingSearchHitsLoadedMsg {
	if !ollamaReachable() {
		return embeddingSearchHitsLoadedMsg{err: fmt.Errorf("ollama not reachable at localhost:11434")}
	}
	vec, err := embed(query)
	if err != nil {
		return embeddingSearchHitsLoadedMsg{err: fmt.Errorf("embed: %w", err)}
	}
	var out []searchResult
	err = db.View(func(tx *hexxladb.Tx) error {
		results, err := tx.SearchByEmbedding(vec, hexxladb.EmbeddingSearchConfig{
			MaxResults: limit,
		})
		if err != nil {
			return err
		}
		for _, r := range results {
			// Convert EmbeddingSearchResult to CellView
			coord, _ := lattice.Unpack(r.Coord)
			cv, err := tx.AssembleCellView(context.Background(), coord, nil, hexxladb.DefaultAssembleCellViewOpts())
			if err == nil {
				out = append(out, searchResult{cell: cv, score: r.Score})
			}
		}
		return nil
	})
	if err != nil {
		return embeddingSearchHitsLoadedMsg{err: err}
	}
	return embeddingSearchHitsLoadedMsg{hits: out}
}

// loadCells scans the cell/ primary key range, collecting up to limit cells.
// Works for both v1 and MVCC databases; covers all cells regardless of coordinate.
func loadCells(db *hexxladb.DB, limit int) []hexxladb.CellView {
	var out []hexxladb.CellView
	seen := map[lattice.PackedCoord]struct{}{}
	v1Len := len(index.CellPrefix) + index.PackedCoordKeyLen
	mvccLen := v1Len + index.VersionSuffixLen
	opts := hexxladb.DefaultAssembleCellViewOpts()
	_ = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendRange(
			[]byte(index.CellPrefix),
			[]byte("cell0"),
			func(k, _ []byte) bool {
				if len(out) >= limit {
					return false
				}
				if len(k) != v1Len && len(k) != mvccLen {
					return true
				}
				p, err := index.ParseCellKey(k[:v1Len])
				if err != nil {
					return true
				}
				if _, ok := seen[p]; ok {
					return true // already added latest version
				}
				seen[p] = struct{}{}
				coord, err := lattice.Unpack(p)
				if err != nil {
					return true
				}
				cv, err := tx.AssembleCellView(context.Background(), coord, nil, opts)
				if err == nil {
					out = append(out, cv)
				}
				return len(out) < limit
			},
		)
	})
	return out
}

// ── Ollama embedding helpers ────────────────────────────────────────────────

const ollamaBase = "http://localhost:11434"

func ollamaReachable() bool {
	resp, err := http.Get(ollamaBase) //nolint:noctx // context not required for simple health check
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func embed(text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]string{"model": "all-minilm", "prompt": text})
	resp, err := http.Post(ollamaBase+"/api/embeddings", "application/json", bytes.NewReader(body)) //nolint:noctx // context not required for simple blocking call
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama status %d", resp.StatusCode)
	}
	var result struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	out := make([]float32, len(result.Embedding))
	for i, v := range result.Embedding {
		out[i] = float32(v)
	}
	return out, nil
}
