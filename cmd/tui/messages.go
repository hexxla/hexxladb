package main

import (
	"context"

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
