package hexxladb

import (
	"context"
	"fmt"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// RingDensity holds the cell count for a single hex ring distance.
type RingDensity struct {
	Ring     int // distance from center (0 = center cell)
	Occupied int // cells that exist at this ring distance
	Total    int // total cells at this ring distance (lattice geometry)
}

// RingDensityMap returns per-ring cell occupancy from center out to maxR.
// Ring 0 is the center cell (Total=1), ring k has Total=6*k for k>0.
func (tx *Tx) RingDensityMap(ctx context.Context, center Coord, maxR int) ([]RingDensity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if err := validatePackedRadius(center, maxR); err != nil {
		return nil, err
	}
	out := make([]RingDensity, 0, maxR+1)
	for ring := 0; ring <= maxR; ring++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		total := 1
		if ring > 0 {
			total = 6 * ring
		}
		occupied := 0
		for c := range lattice.RingSeq(center, ring) {
			p, err := Pack(c)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
			}
			_, ok, err := tx.GetCell(p)
			if err != nil {
				return nil, err
			}
			if ok {
				occupied++
			}
		}
		out = append(out, RingDensity{Ring: ring, Occupied: occupied, Total: total})
	}
	return out, nil
}

// TotalDensity returns aggregate occupied and total cell counts across all rings.
func TotalDensity(rings []RingDensity) (occupied, total int) {
	for _, r := range rings {
		occupied += r.Occupied
		total += r.Total
	}
	return occupied, total
}
