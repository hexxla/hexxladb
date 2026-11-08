package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/inspect"
)

// ─── message types ──────────────────────────────────────────────────────────

type cellsLoadedMsg struct {
	request uint64
	browse  []hexxladb.CellView
	hits    []searchResult
	err     error
}

type inspectCellMsg struct {
	coord hexxladb.Coord
}

type cellDetailMsg struct {
	request uint64
	cell    hexxladb.CellView
	err     error
}

type contextLoadedMsg struct {
	request uint64
	pack    hexxladb.ContextPack
	err     error
}

type seamsLoadedMsg struct {
	seams []seamRow
	err   error
}

type seamRow struct {
	coordA hexxladb.Coord
	coordB hexxladb.Coord
	stype  string
	reason string
	status string
}

type snapshotDiffMsg struct {
	diff *hexxladb.SnapshotDiff
	err  error
}

type searchResult struct {
	cell  hexxladb.CellView
	score float64
}

const (
	cellListLimit       = 200
	lexicalScanRows     = 20_000
	embeddingCandidates = 2_000
)

func searchCells(db *hexxladb.DB, query, mode string) ([]searchResult, error) {
	cfg := hexxladb.CellQuery{Query: query, MaxResults: cellListLimit, MaxScanRows: lexicalScanRows}
	if mode == "embedding" || mode == "hybrid" {
		vector, err := embed(query)
		if err != nil {
			return nil, fmt.Errorf("embed query: %w", err)
		}
		cfg.Embedding = vector
		cfg.MaxScanRows = 0
		cfg.EmbeddingCandidateLimit = embeddingCandidates
		if mode == "embedding" {
			cfg.Query = ""
		}
	}
	var hits []searchResult
	err := db.View(func(tx *hexxladb.Tx) error {
		results, stats, err := tx.QueryCellsWithStats(context.Background(), cfg)
		hits = make([]searchResult, 0, len(results))
		for _, result := range results {
			hits = append(hits, searchResult{cell: result.Cell, score: result.Score})
		}
		if err == nil && stats.EmbeddingCandidateLimitReached {
			return fmt.Errorf("embedding candidate limit reached after %d candidates", stats.EmbeddingCandidates)
		}
		return err
	})
	return hits, err
}

func loadCells(db *hexxladb.DB, request uint64) cellsLoadedMsg {
	cells, err := inspect.Cells(context.Background(), db, cellListLimit, lexicalScanRows)
	return cellsLoadedMsg{request: request, browse: cells, err: err}
}

// ── Ollama embedding helpers ────────────────────────────────────────────────

const (
	ollamaBase           = "http://localhost:11434"
	ollamaRequestTimeout = 10 * time.Second
)

var ollamaHTTPClient = &http.Client{Timeout: ollamaRequestTimeout}

func embed(text string) ([]float32, error) {
	return embedWithClient(ollamaHTTPClient, ollamaBase, text)
}

func embedWithClient(client *http.Client, baseURL, text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]string{"model": "all-minilm", "prompt": text})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama status %d", resp.StatusCode)
	}
	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embedding: %w", err)
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding in Ollama response")
	}
	return result.Embedding, nil
}
