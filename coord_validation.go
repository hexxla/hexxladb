package hexxladb

import (
	"fmt"

	"github.com/hexxla/hexxladb/internal/lattice"
)

const (
	// MaxSpatialScanRadius bounds public raw ring and radial scan helpers.
	MaxSpatialScanRadius = 128
	// MaxSeamSearchRadius bounds the coordinate probes used by seam lookup.
	MaxSeamSearchRadius = 64
	// MaxSeamIndexRows bounds secondary rows examined by one seam lookup.
	MaxSeamIndexRows = 200_000
	// MaxSeamSearchResults bounds materialized seams returned by one lookup.
	MaxSeamSearchResults = 10_000
)

func hexDiskCellCount(radius int) int {
	return 1 + 3*radius*(radius+1)
}

func validatePackedRadius(center Coord, radius int) error {
	if radius < 0 {
		return fmt.Errorf("%w: radius must be non-negative", ErrInvalidArgument)
	}
	if _, err := lattice.Pack(center); err != nil {
		return fmt.Errorf("%w: center: %w", ErrInvalidArgument, err)
	}
	cube := center.Cube()
	margin := min(
		lattice.MaxAxialAbs-absInt(cube.Q),
		lattice.MaxAxialAbs-absInt(cube.R),
		lattice.MaxAxialAbs-absInt(cube.S),
	)
	if radius > margin {
		return fmt.Errorf("%w: radius extends beyond packable coordinate range", ErrInvalidArgument)
	}
	return nil
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
