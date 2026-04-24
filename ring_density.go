package hexxladb

import (
	"context"
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
	if maxR < 0 {
		return nil, ErrInvalidArgument
	}
	out := make([]RingDensity, 0, maxR+1)
	for ring := 0; ring <= maxR; ring++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		coords := Ring(center, ring)
		total := len(coords)
		occupied := 0
		for _, c := range coords {
			p := mustPack(c)
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
