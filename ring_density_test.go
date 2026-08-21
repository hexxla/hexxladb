package hexxladb_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestRingDensityMap_BoundaryReturnsError(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "density.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	err = db.View(func(tx *hexxladb.Tx) error {
		_, err := tx.RingDensityMap(t.Context(), hexxladb.Coord{Q: hexxladb.MaxAxialAbs}, 1)
		return err
	})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("RingDensityMap boundary: want ErrInvalidArgument, got %v", err)
	}
}
