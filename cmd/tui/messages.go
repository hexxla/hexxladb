package main

import (
	"context"

	"github.com/hexxla/hexxladb"
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
	coordA hexxladb.Coord
	coordB hexxladb.Coord
	stype  string
	reason string
	status string
}

type analyticsLoadedMsg struct {
	tagCounts   []hexxladb.TagCount
	tagPairs    []hexxladb.TagPair
	ringDensity []hexxladb.RingDensity
	mvccStats   hexxladb.MVCCStats
	cellCount   int
	seamCount   int
}

// ─── cell loading helper (shared by cells view and main.go tab switch) ───────

// loadCells walks rings from origin outward, collecting up to limit cells.
// Returns cells in ring order (outer rings last).
func loadCells(db *hexxladb.DB, limit int) []hexxladb.CellView {
	var out []hexxladb.CellView
	_ = db.View(func(tx *hexxladb.Tx) error {
		for r := 0; len(out) < limit && r < 10; r++ {
			err := tx.WalkRing(context.Background(), hexxladb.Coord{}, r, func(coord hexxladb.Coord, _ []byte, ok bool) bool {
				if !ok || len(out) >= limit {
					return false
				}
				pk, err := lattice.Pack(coord)
				if err != nil {
					return true
				}
				rec, ok, _ := tx.GetCell(pk)
				if ok {
					out = append(out, hexxladb.CellView{
						Coord:      coord,
						RawContent: rec.RawContent,
						Tags:       rec.Tags,
						Provenance: rec.Provenance,
						Validity:   rec.Validity,
					})
				}
				return len(out) < limit
			})
			if err != nil {
				break
			}
		}
		return nil
	})
	return out
}
