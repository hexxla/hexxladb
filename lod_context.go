package hexxladb

import (
	"context"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// LODContextConfig configures [Tx.LoadContextLOD].
type LODContextConfig struct {
	// FineRadius is the max ring loaded at full resolution (default 3).
	FineRadius int
	// CoarseFactor is the subdivision factor for outer rings (default 2).
	// Each coarse cell covers CoarseFactor² fine cells.
	CoarseFactor int
	// MaxCells caps the total result set (default 256).
	MaxCells int
}

func (cfg *LODContextConfig) withDefaults() {
	if cfg.FineRadius <= 0 {
		cfg.FineRadius = 3
	}
	if cfg.CoarseFactor < 2 {
		cfg.CoarseFactor = 2
	}
	if cfg.MaxCells <= 0 {
		cfg.MaxCells = 256
	}
}

// LoadContextLOD loads cells around center using a level-of-detail strategy.
// Inner rings [0..FineRadius] are loaded at full resolution (every cell).
// Outer rings [FineRadius+1..maxR] are loaded at coarsened resolution —
// only the coarsened parent coordinate is queried, dramatically reducing
// lookups for large radii while preserving nearby detail.
//
// This is useful for large-radius context loading where distant cells are
// less important and can be represented at lower density.
func (tx *Tx) LoadContextLOD(ctx context.Context, center Coord, maxR int, cfg LODContextConfig) ([]CellRecord, error) {
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	cfg.withDefaults()
	if maxR < 0 {
		return nil, ErrInvalidArgument
	}

	fineR := min(cfg.FineRadius, maxR)
	out, err := tx.loadFineRings(ctx, center, fineR, cfg.MaxCells)
	if err != nil {
		return nil, err
	}

	if fineR >= maxR || len(out) >= cfg.MaxCells {
		return out, nil
	}

	coarse, err := tx.loadCoarseRings(ctx, center, fineR+1, maxR, cfg.CoarseFactor, cfg.MaxCells-len(out), out)
	if err != nil {
		return nil, err
	}
	return coarse, nil
}

// loadFineRings loads rings [0..fineR] at full resolution using pre-packed coords.
func (tx *Tx) loadFineRings(ctx context.Context, center Coord, fineR, maxCells int) ([]CellRecord, error) {
	packed := lattice.WalkRingsPacked(center, fineR)
	out := make([]CellRecord, 0, min(len(packed), maxCells))
	for _, p := range packed {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(out) >= maxCells {
			break
		}
		rec, ok, err := tx.GetCell(p)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, rec)
		}
	}
	return out, nil
}

// coarseRingState tracks progress during coarsened ring loading.
type coarseRingState struct {
	seen      map[lattice.PackedCoord]struct{}
	out       []CellRecord
	added     int
	remaining int
	factor    int
}

// tryCoarseCoord coarsens a fine coordinate, deduplicates, and fetches the cell.
// Returns true if the remaining budget is exhausted.
func (s *coarseRingState) tryCoarseCoord(tx *Tx, c Coord) (done bool, err error) {
	coarse, cErr := lattice.CoarsenCoord(c, s.factor)
	if cErr != nil {
		return false, nil
	}
	p, pErr := lattice.Pack(coarse)
	if pErr != nil {
		return false, nil
	}
	if _, dup := s.seen[p]; dup {
		return false, nil
	}
	s.seen[p] = struct{}{}

	rec, ok, getErr := tx.GetCell(p)
	if getErr != nil {
		return false, getErr
	}
	if ok {
		s.out = append(s.out, rec)
		s.added++
		return s.added >= s.remaining, nil
	}
	return false, nil
}

// loadCoarseRings loads rings [startR..maxR] at coarsened resolution.
// For each ring, it generates fine coords, coarsens them, deduplicates,
// and looks up the coarsened coordinate. This reduces the number of
// lookups from O(6k) to O(6k/factor²) per ring.
func (tx *Tx) loadCoarseRings(ctx context.Context, center Coord, startR, maxR, factor, remaining int, out []CellRecord) ([]CellRecord, error) {
	s := coarseRingState{
		seen:      make(map[lattice.PackedCoord]struct{}, len(out)),
		out:       out,
		remaining: remaining,
		factor:    factor,
	}
	for _, rec := range out {
		s.seen[rec.Key] = struct{}{}
	}

	ringBuf := make([]lattice.Coord, 0, 6*maxR+1)
	for ring := startR; ring <= maxR; ring++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if s.added >= remaining {
			break
		}
		ringBuf = lattice.RingInto(ringBuf[:0], center, ring)
		for _, c := range ringBuf {
			done, err := s.tryCoarseCoord(tx, c)
			if err != nil {
				return nil, err
			}
			if done {
				return s.out, nil
			}
		}
	}
	return s.out, nil
}
