package hexxladb_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestSuperHexSummaryIndex_RebuildAndSync(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "superhex.db"), &hexxladb.Options{ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	parent := lattice.Coord{Q: 2, R: -1}
	children := lattice.SuperHexChildren(parent)
	put := func(coords ...lattice.Coord) {
		t.Helper()
		if err := db.Update(func(tx *hexxladb.Tx) error {
			for _, coord := range coords {
				packed, err := lattice.Pack(coord)
				if err != nil {
					return err
				}
				if err := tx.PutCell(context.Background(), record.CellRecord{Key: packed, RawContent: "cell"}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	put(children[:3]...)

	idx, err := hexxladb.NewSuperHexSummaryIndex(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Rebuild(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	summary, ok := idx.Summary(parent)
	if !ok {
		t.Fatal("summary missing after rebuild")
	}
	if summary.OccupiedCells != 3 || summary.Capacity != 7 {
		t.Fatalf("rebuilt summary = %+v, want occupied=3 capacity=7", summary)
	}
	if summary.Center != lattice.SuperHexCenter(parent) {
		t.Fatalf("summary center = %v, want %v", summary.Center, lattice.SuperHexCenter(parent))
	}
	byCoord, ok, err := idx.SummaryForCoord(children[2])
	if err != nil || !ok || byCoord.Parent != parent {
		t.Fatalf("SummaryForCoord = (%+v, %v, %v), want parent %v", byCoord, ok, err, parent)
	}
	rebuildSeq := idx.LastSeq()

	// Updating one existing cell must not double-count it; adding a child must.
	put(children[0], children[3])
	processed, err := idx.Sync(t.Context(), db, 100)
	if err != nil {
		t.Fatal(err)
	}
	if processed != 2 {
		t.Fatalf("processed %d records, want 2", processed)
	}
	summary, _ = idx.Summary(parent)
	if summary.OccupiedCells != 4 {
		t.Fatalf("synced occupied=%d, want 4", summary.OccupiedCells)
	}
	if summary.LastUpdatedSeq <= rebuildSeq || idx.LastSeq() != summary.LastUpdatedSeq {
		t.Fatalf("sequence did not advance: rebuild=%d summary=%d index=%d", rebuildSeq, summary.LastUpdatedSeq, idx.LastSeq())
	}

	packed, err := lattice.Pack(children[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.DeleteCell(t.Context(), packed) }); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.Sync(t.Context(), db, 0); err != nil {
		t.Fatal(err)
	}
	summary, _ = idx.Summary(parent)
	if summary.OccupiedCells != 3 {
		t.Fatalf("occupied after delete=%d, want 3", summary.OccupiedCells)
	}
}

func TestSuperHexSummaryIndex_SummariesSorted(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "sorted.db"), &hexxladb.Options{ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	parents := []lattice.Coord{{Q: 3, R: 0}, {Q: -2, R: 4}, {Q: 0, R: -1}}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		for _, parent := range parents {
			packed, err := lattice.Pack(lattice.SuperHexCenter(parent))
			if err != nil {
				return err
			}
			if err := tx.PutCell(t.Context(), record.CellRecord{Key: packed, RawContent: "cell"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	idx, _ := hexxladb.NewSuperHexSummaryIndex(1)
	if err := idx.Rebuild(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	got := idx.Summaries()
	gotParents := make([]lattice.Coord, len(got))
	for i := range got {
		gotParents[i] = got[i].Parent
	}
	want := slices.Clone(parents)
	slices.SortFunc(want, func(a, b lattice.Coord) int {
		switch {
		case a.Q < b.Q:
			return -1
		case a.Q > b.Q:
			return 1
		case a.R < b.R:
			return -1
		case a.R > b.R:
			return 1
		default:
			return 0
		}
	})
	if !slices.Equal(gotParents, want) {
		t.Fatalf("summary order=%v, want %v", gotParents, want)
	}
}

func TestSuperHexSummaryIndex_Validation(t *testing.T) {
	t.Parallel()
	if _, err := hexxladb.NewSuperHexSummaryIndex(0); !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("level 0: want ErrInvalidArgument, got %v", err)
	}
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "no-changelog.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	idx, _ := hexxladb.NewSuperHexSummaryIndex(1)
	if err := idx.Rebuild(t.Context(), db); !errors.Is(err, hexxladb.ErrChangelogDisabled) {
		t.Fatalf("rebuild without changelog: want ErrChangelogDisabled, got %v", err)
	}
}

func TestSuperHexSummaryIndex_RandomizedAgainstOracle(t *testing.T) {
	const (
		batches    = 80
		batchSize  = 24
		coordinate = 25
	)
	coords := lattice.WalkRings(nil, lattice.Coord{}, coordinate)

	for _, tc := range []struct {
		level int
		seed  uint64
	}{{1, 1}, {2, 7}, {3, 42}} {
		t.Run(fmt.Sprintf("level_%d_seed_%d", tc.level, tc.seed), func(t *testing.T) {
			db, err := hexxladb.Open(filepath.Join(t.TempDir(), "soak.db"), &hexxladb.Options{
				ChangelogEnabled: true,
				ChangelogLazy:    true,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })

			idx, err := hexxladb.NewSuperHexSummaryIndex(tc.level)
			if err != nil {
				t.Fatal(err)
			}
			if err := idx.Rebuild(t.Context(), db); err != nil {
				t.Fatal(err)
			}

			type mutation struct {
				coord lattice.Coord
				put   bool
			}
			active := make(map[lattice.PackedCoord]lattice.Coord)
			rng := rand.New(rand.NewPCG(tc.seed, tc.seed^0x9e3779b97f4a7c15))
			previousSeq := idx.LastSeq()

			for batch := range batches {
				mutations := make([]mutation, batchSize)
				for i := range mutations {
					mutations[i] = mutation{
						coord: coords[rng.IntN(len(coords))],
						put:   rng.IntN(100) < 70,
					}
				}
				if err := db.Update(func(tx *hexxladb.Tx) error {
					for i, mutation := range mutations {
						packed, err := lattice.Pack(mutation.coord)
						if err != nil {
							return err
						}
						if mutation.put {
							err = tx.PutCell(t.Context(), record.CellRecord{
								Key:        packed,
								RawContent: fmt.Sprintf("batch-%d-mutation-%d", batch, i),
							})
						} else {
							err = tx.DeleteCell(t.Context(), packed)
						}
						if err != nil {
							return err
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}

				for _, mutation := range mutations {
					packed, err := lattice.Pack(mutation.coord)
					if err != nil {
						t.Fatal(err)
					}
					if mutation.put {
						active[packed] = mutation.coord
					} else {
						delete(active, packed)
					}
				}

				for {
					processed, err := idx.Sync(t.Context(), db, 17)
					if err != nil {
						t.Fatal(err)
					}
					if processed == 0 {
						break
					}
				}
				if idx.LastSeq() < previousSeq {
					t.Fatalf("cursor moved backward: previous=%d current=%d", previousSeq, idx.LastSeq())
				}
				previousSeq = idx.LastSeq()
				assertSuperHexSummaryOracle(t, idx, tc.level, active)
			}

			remaining, err := db.ReadChangelogSince(idx.LastSeq(), 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(remaining) != 0 {
				t.Fatalf("index cursor left %d changelog record(s) unapplied", len(remaining))
			}
		})
	}
}

func assertSuperHexSummaryOracle(t *testing.T, idx *hexxladb.SuperHexSummaryIndex, level int, active map[lattice.PackedCoord]lattice.Coord) {
	t.Helper()
	want := make(map[lattice.Coord]int)
	for _, coord := range active {
		want[lattice.SuperHexParentAtLevel(coord, level)]++
	}

	got := idx.Summaries()
	if len(got) != len(want) {
		t.Fatalf("summary count=%d, want %d (active cells=%d)", len(got), len(want), len(active))
	}
	occupied := 0
	for _, summary := range got {
		wantOccupied, ok := want[summary.Parent]
		if !ok {
			t.Fatalf("unexpected summary for parent %v", summary.Parent)
		}
		if summary.Level != level || summary.OccupiedCells != wantOccupied {
			t.Fatalf("summary for %v = %+v, want level=%d occupied=%d", summary.Parent, summary, level, wantOccupied)
		}
		if summary.OccupiedCells <= 0 || summary.OccupiedCells > summary.Capacity {
			t.Fatalf("invalid occupancy for %v: %d/%d", summary.Parent, summary.OccupiedCells, summary.Capacity)
		}
		if summary.LastUpdatedSeq > idx.LastSeq() {
			t.Fatalf("summary sequence %d exceeds index cursor %d", summary.LastUpdatedSeq, idx.LastSeq())
		}
		occupied += summary.OccupiedCells
	}
	if occupied != len(active) {
		t.Fatalf("summed occupancy=%d, want %d", occupied, len(active))
	}
}
