// Package inspect translates private storage details into operator diagnostics.
package inspect

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/hnsw"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// Cells collects up to limit snapshot-visible cells in primary key order.
func Cells(ctx context.Context, db *hexxladb.DB, limit int) ([]hexxladb.CellView, error) {
	if limit <= 0 {
		return nil, nil
	}
	var out []hexxladb.CellView
	var previous hexxladb.PackedCoord
	var havePrevious bool
	v1Len := len(index.CellPrefix) + index.PackedCoordKeyLen
	mvccLen := v1Len + index.VersionSuffixLen
	opts := hexxladb.DefaultAssembleCellViewOpts()
	var scanErr error
	prefix := []byte(index.CellPrefix)
	err := db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendRange(prefix, []byte("cell0"), func(k, _ []byte) bool {
			if !bytes.HasPrefix(k, prefix) {
				return false
			}
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
			cv, err := tx.AssembleCellView(ctx, coord, nil, opts)
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
		return nil, scanErr
	}
	return out, ctx.Err()
}

// CountSeams counts logical seam IDs in the transaction's snapshot up to limit.
func CountSeams(ctx context.Context, tx *hexxladb.Tx, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	var count int
	err := tx.AscendSeams(ctx, func(hexxladb.SeamRecord) bool {
		count++
		return count < limit
	})
	return count, err
}

// Embeddings returns the bounded vector count and whether graph metadata is readable.
func Embeddings(tx *hexxladb.Tx, limit int) (count int, graph bool, err error) {
	value, found, err := tx.Get([]byte(index.HNSWMetaKey))
	if err != nil {
		return 0, false, err
	}
	if found {
		if _, err := hnsw.DecodeMeta(value); err != nil {
			return 0, false, fmt.Errorf("%w: decode embedding index metadata: %w", hexxladb.ErrCorruptDatabase, err)
		}
	}
	if limit <= 0 {
		return 0, found, nil
	}
	prefix := []byte(index.EmbedPrefix)
	err = tx.AscendRange(prefix, index.EmbedKeyEnd(), func(key, _ []byte) bool {
		if !bytes.HasPrefix(key, prefix) {
			return false
		}
		count++
		return count < limit
	})
	return count, found, err
}

// VisibleHexes projects field of view from an already captured occupancy map.
func VisibleHexes(center hexxladb.Coord, radius int, occupied map[hexxladb.Coord]bool) map[hexxladb.Coord]bool {
	visible := make(map[hexxladb.Coord]bool)
	for _, coord := range lattice.FieldOfView(center, radius, func(c lattice.Coord) bool { return !occupied[c] }) {
		visible[coord] = true
	}
	return visible
}
