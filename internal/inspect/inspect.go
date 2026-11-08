// Package inspect translates private storage details into operator diagnostics.
package inspect

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// Cells collects up to limit snapshot-visible cells in primary key order.
// It returns partial results and ErrQueryScanLimit if the scan exceeds maxRows.
func Cells(ctx context.Context, db *hexxladb.DB, limit, maxRows int) ([]hexxladb.CellView, error) {
	if limit <= 0 {
		return nil, nil
	}
	var out []hexxladb.CellView
	var previous hexxladb.PackedCoord
	var havePrevious bool
	v1Len := len(index.CellPrefix) + index.PackedCoordKeyLen
	mvccLen := v1Len + index.VersionSuffixLen
	var scanned int
	var scanErr error
	prefix := []byte(index.CellPrefix)
	err := db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendRange(prefix, []byte("cell0"), func(k, _ []byte) bool {
			if !bytes.HasPrefix(k, prefix) {
				return false
			}
			if scanned == maxRows {
				scanErr = hexxladb.ErrQueryScanLimit
				return false
			}
			scanned++
			if scanErr = ctx.Err(); scanErr != nil {
				return false
			}
			if len(k) != v1Len && len(k) != mvccLen {
				scanErr = fmt.Errorf("%w: invalid cell primary key length", hexxladb.ErrCorruptDatabase)
				return false
			}
			p, err := index.ParseCellKey(k[:v1Len])
			if err != nil {
				scanErr = fmt.Errorf("%w: parse cell primary: %w", hexxladb.ErrCorruptDatabase, err)
				return false
			}
			if havePrevious && p == previous {
				return true
			}
			previous, havePrevious = p, true
			coord, err := hexxladb.Unpack(p)
			if err != nil {
				scanErr = fmt.Errorf("%w: unpack cell primary: %w", hexxladb.ErrCorruptDatabase, err)
				return false
			}
			cv, err := tx.AssembleCellView(ctx, coord, nil, hexxladb.AssembleCellViewOpts{})
			if errors.Is(err, hexxladb.ErrCellNotFound) {
				return true
			}
			if err != nil {
				scanErr = err
				return false
			}
			out = append(out, cv)
			return len(out) < limit
		})
	})
	if err != nil {
		return nil, err
	}
	if scanErr != nil {
		return out, scanErr
	}
	return out, ctx.Err()
}

// VisibleHexes projects field of view from an already captured occupancy map.
func VisibleHexes(center hexxladb.Coord, radius int, occupied map[hexxladb.Coord]bool) map[hexxladb.Coord]bool {
	visible := make(map[hexxladb.Coord]bool)
	for _, coord := range lattice.FieldOfView(center, radius, func(c lattice.Coord) bool { return !occupied[c] }) {
		visible[coord] = true
	}
	return visible
}
