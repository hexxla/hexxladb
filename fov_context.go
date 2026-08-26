package hexxladb

import (
	"context"
	"fmt"

	"github.com/hexxla/hexxladb/internal/lattice"
)

const maxFOVContextRadius = 256

// FOVContextConfig configures [Tx.LoadContextFOV].
type FOVContextConfig struct {
	// MaxCells caps the total result set (default 256).
	MaxCells int
}

func (cfg *FOVContextConfig) withDefaults() {
	if cfg.MaxCells <= 0 {
		cfg.MaxCells = 256
	}
}

// LoadContextFOV loads cells around center using field-of-view filtering.
// Only cells that are visible from center (via deterministic symmetric
// shadowcasting) are included.
// The opaque function determines which coordinates block vision; cells for which
// opaque returns true are themselves included (the wall is visible) but block
// cells behind them.
//
// This is useful for sparse grids where large empty/irrelevant regions should
// not consume the context budget. Compared to plain radial loading, FOV skips
// cells that are occluded behind opaque barriers, spending budget only on
// semantically reachable cells.
func (tx *Tx) LoadContextFOV(ctx context.Context, center Coord, maxR int, opaque func(Coord) bool, cfg FOVContextConfig) ([]CellRecord, error) {
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxR < 0 || maxR > maxFOVContextRadius {
		return nil, fmt.Errorf("%w: FOV radius must be between 0 and %d", ErrInvalidArgument, maxFOVContextRadius)
	}
	if cfg.MaxCells < 0 {
		return nil, fmt.Errorf("%w: FOV MaxCells must not be negative", ErrInvalidArgument)
	}
	if opaque == nil {
		return nil, ErrInvalidArgument
	}
	if err := validatePackedRadius(center, maxR); err != nil {
		return nil, err
	}
	cfg.withDefaults()

	visible := lattice.FieldOfView(center, maxR, opaque)
	return tx.fetchVisibleCells(ctx, visible, cfg.MaxCells)
}

// fetchVisibleCells packs and fetches cells from a deterministic nearest-first
// list of visible coordinates.
func (tx *Tx) fetchVisibleCells(ctx context.Context, coords []Coord, maxCells int) ([]CellRecord, error) {
	out := make([]CellRecord, 0, min(len(coords), maxCells))
	for _, c := range coords {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(out) >= maxCells {
			break
		}
		p, err := lattice.Pack(c)
		if err != nil {
			continue
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
