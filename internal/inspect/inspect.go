// Package inspect translates private storage details into operator diagnostics.
package inspect

import (
	"context"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/hnsw"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// Cells scans the cell/ primary key range, collecting up to limit cells.
// Works for both v1 and MVCC databases; covers all cells regardless of coordinate.
func Cells(db *hexxladb.DB, limit int) []hexxladb.CellView {
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

// WalkSeams visits decodable seams in storage order.
func WalkSeams(tx *hexxladb.Tx, visit func(hexxladb.SeamRecord) bool) error {
	return tx.AscendRange([]byte(index.SeamPrefix), index.SeamScanUpperBound(), func(_, value []byte) bool {
		seam, err := record.DecodeSeam(value)
		return err != nil || visit(seam)
	})
}

// CountSeams counts physical seam records up to the diagnostic limit.
func CountSeams(tx *hexxladb.Tx, limit int) int {
	var count int
	_ = tx.AscendRange([]byte(index.SeamPrefix), index.SeamScanUpperBound(), func(_, _ []byte) bool {
		count++
		return count < limit
	})
	return count
}

// Embeddings returns the bounded vector count and whether graph metadata is readable.
func Embeddings(tx *hexxladb.Tx, limit int) (count int, graph bool) {
	if value, ok, err := tx.Get([]byte(index.HNSWMetaKey)); err == nil && ok {
		_, err := hnsw.DecodeMeta(value)
		graph = err == nil
	}
	_ = tx.AscendRange([]byte(index.EmbedPrefix), index.EmbedKeyEnd(), func(_, _ []byte) bool {
		count++
		return count < limit
	})
	return count, graph
}
