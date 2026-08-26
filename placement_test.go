package hexxladb_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestTx_FindFreeCellPlacementDeterministicWithinUpdate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		opts *hexxladb.Options
	}{
		{name: "format-v1"},
		{name: "mvcc", opts: &hexxladb.Options{EnableMVCC: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db, err := hexxladb.Open(filepath.Join(t.TempDir(), "placement.db"), tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })

			anchor := hexxladb.Coord{Q: 7, R: -3}
			ring := hexxladb.Ring(anchor, 1)
			occupied := append([]hexxladb.Coord{anchor}, ring[:2]...)
			err = db.Update(func(tx *hexxladb.Tx) error {
				for _, coord := range occupied {
					key, err := hexxladb.Pack(coord)
					if err != nil {
						return err
					}
					if err := tx.PutCell(t.Context(), hexxladb.CellRecord{Key: key, RawContent: "occupied"}); err != nil {
						return err
					}
				}

				first, err := tx.FindFreeCellPlacement(t.Context(), anchor, 1)
				if err != nil {
					return err
				}
				wantFirst, err := hexxladb.Pack(ring[2])
				if err != nil {
					return err
				}
				if first.Coord != ring[2] || first.Key != wantFirst || first.Probes != 3 {
					t.Fatalf("first placement: got %+v, want coord=%+v key=%v probes=3", first, ring[2], wantFirst)
				}
				repeated, err := tx.FindFreeCellPlacement(t.Context(), anchor, 1)
				if err != nil {
					return err
				}
				if repeated != first {
					t.Fatalf("unreserved placement changed: first=%+v repeated=%+v", first, repeated)
				}
				if err := tx.PutCell(t.Context(), hexxladb.CellRecord{Key: first.Key, RawContent: "first"}); err != nil {
					return err
				}

				second, err := tx.FindFreeCellPlacement(t.Context(), anchor, 1)
				if err != nil {
					return err
				}
				wantSecond, err := hexxladb.Pack(ring[3])
				if err != nil {
					return err
				}
				if second.Coord != ring[3] || second.Key != wantSecond || second.Probes != 4 {
					t.Fatalf("second placement: got %+v, want coord=%+v key=%v probes=4", second, ring[3], wantSecond)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTx_FindFreeCellPlacementValidationAndExhaustion(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "placement.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.View(func(tx *hexxladb.Tx) error {
		_, err := tx.FindFreeCellPlacement(t.Context(), hexxladb.Coord{}, 0)
		return err
	}); !errors.Is(err, hexxladb.ErrTxReadOnly) {
		t.Fatalf("read-only placement error: got %v, want ErrTxReadOnly", err)
	}

	for _, radius := range []int{-1, hexxladb.MaxCellPlacementRadius + 1} {
		err := db.Update(func(tx *hexxladb.Tx) error {
			_, err := tx.FindFreeCellPlacement(t.Context(), hexxladb.Coord{}, radius)
			return err
		})
		if !errors.Is(err, hexxladb.ErrInvalidArgument) {
			t.Fatalf("radius %d: got %v, want ErrInvalidArgument", radius, err)
		}
	}

	err = db.Update(func(tx *hexxladb.Tx) error {
		_, err := tx.FindFreeCellPlacement(
			t.Context(),
			hexxladb.Coord{Q: hexxladb.MaxAxialAbs + 1},
			0,
		)
		return err
	})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) || !errors.Is(err, hexxladb.ErrCoordOutOfRange) {
		t.Fatalf("invalid anchor: got %v, want ErrInvalidArgument and ErrCoordOutOfRange", err)
	}
	err = db.Update(func(tx *hexxladb.Tx) error {
		_, err := tx.FindFreeCellPlacement(
			t.Context(),
			hexxladb.Coord{Q: hexxladb.MaxAxialAbs},
			1,
		)
		return err
	})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("boundary-crossing radius: got %v, want ErrInvalidArgument", err)
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	err = db.Update(func(tx *hexxladb.Tx) error {
		_, err := tx.FindFreeCellPlacement(cancelled, hexxladb.Coord{}, 0)
		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled placement: got %v, want context.Canceled", err)
	}

	anchor := hexxladb.Coord{Q: -5, R: 2}
	all := append([]hexxladb.Coord{anchor}, hexxladb.Ring(anchor, 1)...)
	err = db.Update(func(tx *hexxladb.Tx) error {
		for _, coord := range all {
			key, err := hexxladb.Pack(coord)
			if err != nil {
				return err
			}
			if err := tx.PutCell(t.Context(), hexxladb.CellRecord{Key: key, RawContent: "preserve"}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Update(func(tx *hexxladb.Tx) error {
		_, err := tx.FindFreeCellPlacement(t.Context(), anchor, 1)
		return err
	})
	if !errors.Is(err, hexxladb.ErrNoFreeCellPlacement) {
		t.Fatalf("exhausted placement: got %v, want ErrNoFreeCellPlacement", err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		for _, coord := range all {
			key, err := hexxladb.Pack(coord)
			if err != nil {
				return err
			}
			rec, ok, err := tx.GetCell(key)
			if err != nil {
				return err
			}
			if !ok || rec.RawContent != "preserve" {
				t.Fatalf("occupied cell changed at %+v: ok=%v content=%q", coord, ok, rec.RawContent)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTx_FindFreeCellPlacementMVCCReusesTombstoneAndPreservesHistory(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(
		filepath.Join(t.TempDir(), "placement.db"),
		&hexxladb.Options{EnableMVCC: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	anchor := hexxladb.Coord{Q: 3, R: 4}
	key, err := hexxladb.Pack(anchor)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), hexxladb.CellRecord{Key: key, RawContent: "historical"})
	}); err != nil {
		t.Fatal(err)
	}
	beforeDelete, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.DeleteCell(t.Context(), key)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		placement, err := tx.FindFreeCellPlacement(t.Context(), anchor, 0)
		if err != nil {
			return err
		}
		if placement.Coord != anchor || placement.Key != key || placement.Probes != 0 {
			t.Fatalf("tombstone placement: got %+v, want anchor with zero probes", placement)
		}
		return tx.PutCell(t.Context(), hexxladb.CellRecord{Key: placement.Key, RawContent: "current"})
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.ViewAt(beforeDelete.CommitSeq, func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(key)
		if err != nil {
			return err
		}
		if !ok || rec.RawContent != "historical" {
			t.Fatalf("historical placement: ok=%v content=%q", ok, rec.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(key)
		if err != nil {
			return err
		}
		if !ok || rec.RawContent != "current" {
			t.Fatalf("current placement: ok=%v content=%q", ok, rec.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
