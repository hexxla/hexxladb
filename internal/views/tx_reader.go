package views

import (
	"context"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// TxReader is the narrow read port that view assembly requires from a transaction.
// The root package *Tx satisfies this interface implicitly (Go structural typing),
// so internal/views never imports the root package — the import cycle is broken.
//
// Keep this interface minimal: only add methods that view-assembly code directly
// calls. Everything else stays on *Tx.
type TxReader interface {
	// GetCell returns the decoded cell visible at the transaction snapshot.
	// Returns (zero, false, nil) when the key has no live cell.
	GetCell(key lattice.PackedCoord) (record.CellRecord, bool, error)

	// AscendFacetsForCell iterates every facet stored for the given cell key
	// in ascending facet_id order. fn returns false to stop early.
	AscendFacetsForCell(key lattice.PackedCoord, fn func(record.FacetRecord) bool) error

	// AscendEdgesFrom iterates directed edges stored for the given cell key.
	// fn returns false to stop early.
	AscendEdgesFrom(key lattice.PackedCoord, fn func(record.EdgeRecord) bool) error

	// FindSeams returns seams where at least one endpoint lies within radius
	// rings of center. If unresolvedOnly is true only unresolved seams are returned.
	FindSeams(ctx context.Context, center lattice.Coord, radius int, unresolvedOnly bool) ([]record.SeamRecord, error)
}
