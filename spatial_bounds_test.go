package hexxladb_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
)

func TestLoadContextRejectsUnboundedWork(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "context-bounds.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	seed := hexxladb.Coord{}
	tooManySeeds := make([]hexxladb.Coord, hexxladb.MaxContextSeeds+1)
	tooMuchCombinedWork := make([]hexxladb.Coord, 5)
	tests := map[string]hexxladb.LoadContextConfig{
		"negative radius": {Seeds: []hexxladb.Coord{seed}, MaxRing: -1},
		"radius":          {Seeds: []hexxladb.Coord{seed}, MaxRing: hexxladb.MaxContextRadius + 1},
		"seeds":           {Seeds: tooManySeeds},
		"results":         {Seeds: []hexxladb.Coord{seed}, MaxCells: hexxladb.MaxContextResults + 1},
		"hops":            {Seeds: []hexxladb.Coord{seed}, EdgeFilter: "r", MaxHops: hexxladb.MaxContextHops + 1},
		"combined work":   {Seeds: tooMuchCombinedWork, MaxRing: hexxladb.MaxContextRadius},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			err := db.View(func(tx *hexxladb.Tx) error {
				_, err := tx.LoadContext(t.Context(), cfg)
				return err
			})
			if !errors.Is(err, hexxladb.ErrInvalidArgument) {
				t.Fatalf("LoadContext error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func TestSpatialWalksRejectExcessiveRadius(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "spatial-bounds.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	err = db.View(func(tx *hexxladb.Tx) error {
		checks := []func() error{
			func() error {
				return tx.WalkRing(t.Context(), hexxladb.Coord{}, hexxladb.MaxSpatialScanRadius+1, func(hexxladb.Coord, []byte, bool) bool { return true })
			},
			func() error {
				return tx.WalkRingAt(t.Context(), hexxladb.Coord{}, hexxladb.MaxSpatialScanRadius+1, time.Now(), func(hexxladb.Coord, hexxladb.CellRecord) bool { return true })
			},
			func() error {
				return tx.WalkRingFacets(t.Context(), hexxladb.Coord{}, hexxladb.MaxSpatialScanRadius+1, 1, nil, func(hexxladb.Coord, hexxladb.CellRecord, []hexxladb.FacetWalkRecord) bool { return true })
			},
			func() error {
				_, err := tx.ScanContextRaw(t.Context(), hexxladb.Coord{}, hexxladb.MaxSpatialScanRadius+1, 1)
				return err
			},
			func() error {
				_, err := tx.ScanContextAtRaw(t.Context(), hexxladb.Coord{}, hexxladb.MaxSpatialScanRadius+1, 1, time.Now())
				return err
			},
			func() error {
				_, err := tx.FindSeams(t.Context(), hexxladb.Coord{}, hexxladb.MaxSeamSearchRadius+1, false)
				return err
			},
		}
		for i, check := range checks {
			if err := check(); !errors.Is(err, hexxladb.ErrInvalidArgument) {
				t.Errorf("check %d error = %v, want ErrInvalidArgument", i, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWalkRingPreflightsPackableBoundaryBeforeCallback(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "boundary.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	called := false
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.WalkRing(t.Context(), hexxladb.Coord{Q: hexxladb.MaxAxialAbs}, 1, func(hexxladb.Coord, []byte, bool) bool {
			called = true
			return true
		})
	})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("WalkRing error = %v, want ErrInvalidArgument", err)
	}
	if called {
		t.Fatal("WalkRing called callback before rejecting out-of-range disk")
	}
}
