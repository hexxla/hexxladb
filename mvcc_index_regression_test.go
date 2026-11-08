package hexxladb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hexxla/hexxladb/internal/index"
)

func TestMVCCSecondaryIndexesFollowSnapshot(t *testing.T) {
	t.Parallel()
	db := openMVCCRegressionDB(t, filepath.Join(t.TempDir(), "index.db"), &Options{EnableMVCC: true})
	ctx := t.Context()
	coord, err := Pack(Coord{Q: 1})
	if err != nil {
		t.Fatal(err)
	}
	origin, err := Pack(Coord{})
	if err != nil {
		t.Fatal(err)
	}
	cell := CellRecord{
		Key: coord, RawContent: "first", Tags: []string{"old"},
		Provenance: ProvenanceWire{SourceID: "old"}, Validity: ValidityWire{ValidFrom: new(int64(1))},
	}
	seam := SeamRecord{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", CellA: origin, CellB: coord, SeamType: "test",
		Provenance: cell.Provenance, Validity: cell.Validity,
	}
	if err := db.Update(func(tx *Tx) error {
		if err := tx.PutCell(ctx, cell); err != nil {
			return err
		}
		return tx.PutSeam(ctx, seam)
	}); err != nil {
		t.Fatal(err)
	}

	oldWeek, _ := index.WeekBucketFromValidity(cell.Validity)
	cell.RawContent, cell.Tags, cell.Provenance.SourceID = "new", []string{"new"}, "new"
	cell.Validity.ValidFrom = new(int64((8 * 24 * time.Hour).Nanoseconds()))
	seam.Provenance, seam.Validity = cell.Provenance, cell.Validity
	newWeek, _ := index.WeekBucketFromValidity(cell.Validity)
	if err := db.Update(func(tx *Tx) error {
		if err := tx.PutCell(ctx, cell); err != nil {
			return err
		}
		if err := tx.PutSeam(ctx, seam); err != nil {
			return err
		}
		if err := tx.WalkRing(ctx, Coord{Q: 1}, 0, func(_ Coord, raw []byte, _ bool) bool {
			clear(raw)
			return true
		}); err != nil {
			return err
		}
		// Write again in this commit so the first staged indexes must be replaced.
		cell.Tags[0], cell.Provenance.SourceID = "final", "final"
		seam.Provenance = cell.Provenance
		if err := tx.PutCell(ctx, cell); err != nil {
			return err
		}
		if err := tx.PutSeam(ctx, seam); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *Tx) error { return tx.DeleteCell(ctx, coord) }); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		seq          uint64
		label        string
		week         int64
		cells, seams int
	}{
		{1, "old", oldWeek, 1, 1},
		{2, "old", oldWeek, 0, 0},
		{2, "new", oldWeek, 0, 0},
		{2, "final", newWeek, 1, 1},
		{3, "final", newWeek, 0, 1},
	} {
		if err := db.ViewAt(tc.seq, func(tx *Tx) error {
			checkMVCCSecondaryCounts(t, tx, tc.label, tc.week, tc.cells, tc.seams)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	pruned, err := db.PruneCellVersions(3, 2)
	if err != nil || pruned != 2 {
		t.Fatalf("prune = %d, %v, want two historical cell versions", pruned, err)
	}
	report, err := db.HealthCheck(ctx, HealthCheckConfig{CheckTagIndex: true, CheckSourceIndex: true})
	if err != nil || report.TagIndexErrors != 0 || report.SourceIndexErrors != 0 {
		t.Fatalf("secondary health after prune: %+v, %v", report, err)
	}
	if err := db.View(func(tx *Tx) error {
		rows := 0
		err := tx.AscendRange([]byte(index.TimePrefix), []byte("time0"), func(_, _ []byte) bool {
			rows++
			return true
		})
		if err != nil || rows != 0 {
			t.Fatalf("obsolete physical time rows = %d, %v", rows, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRepairMVCCIndexesToRestoresRetainedHistory(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"plaintext", Options{EnableMVCC: true}},
		{"authenticated", Options{EncryptionKey: []byte("mvcc index repair fixture")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			source, destination := filepath.Join(dir, "source.db"), filepath.Join(dir, "repaired.db")
			db := openMVCCRegressionDB(t, source, &tc.opts)
			ctx := t.Context()
			key, err := Pack(Coord{Q: -2, R: 3})
			if err != nil {
				t.Fatal(err)
			}
			cell := CellRecord{
				Key: key, RawContent: "retained primary", Tags: []string{"historical"},
				Provenance: ProvenanceWire{SourceID: " historical "}, Validity: ValidityWire{ValidFrom: new(int64(-1))},
			}
			origin, err := Pack(Coord{})
			if err != nil {
				t.Fatal(err)
			}
			seam := SeamRecord{
				ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", CellA: origin, CellB: key, SeamType: "test",
				Provenance: cell.Provenance, Validity: cell.Validity,
			}
			rawKey := []byte("time/application-key")
			if err := db.Update(func(tx *Tx) error {
				if err := tx.PutCell(ctx, CellRecord{Key: origin}); err != nil {
					return err
				}
				if err := tx.PutCell(ctx, cell); err != nil {
					return err
				}
				if err := tx.PutSeam(ctx, seam); err != nil {
					return err
				}
				return tx.putDirect(rawKey, []byte("application data"))
			}); err != nil {
				t.Fatal(err)
			}
			stale, err := index.TagKeyWithVersion("stale", key, 1)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Update(func(tx *Tx) error {
				if err := tx.DeleteCell(ctx, key); err != nil {
					return err
				}
				// Recreate the missing history and stale entries left by old writers.
				if err := tx.removeCellSecondaryIndex(cell, 1); err != nil {
					return err
				}
				if err := tx.removeSeamSecondaryIndex(seam, 1); err != nil {
					return err
				}
				return tx.putDirect(stale, nil)
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			canceled, cancel := context.WithCancel(ctx)
			err = RepairMVCCIndexesTo(canceled, source, destination, &tc.opts, &CompactOptions{
				BatchSize: 1, OnProgress: func(CompactProgress) { cancel() },
			})
			cancel()
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("interrupted repair error = %v, want context cancellation", err)
			}
			if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("partial destination remains: %v", err)
			}
			if err := RepairMVCCIndexesTo(ctx, source, destination, &tc.opts, &CompactOptions{BatchSize: 1}); err != nil {
				t.Fatal(err)
			}
			week, _ := index.WeekBucketFromValidity(cell.Validity)
			for _, result := range []struct {
				path string
				want int
			}{{source, 0}, {destination, 1}} {
				reopened := openMVCCRegressionDB(t, result.path, &tc.opts)
				if err := reopened.ViewAt(1, func(tx *Tx) error {
					got, ok, err := tx.GetCell(key)
					if err != nil || !ok || got.RawContent != cell.RawContent {
						t.Fatalf("historical primary in %s = %v, %t, %v", result.path, got, ok, err)
					}
					checkMVCCSecondaryCounts(t, tx, "historical", week, result.want, result.want)
					raw, exists, err := tx.Get(rawKey)
					if err != nil || !exists || string(raw) != "application data" {
						t.Fatalf("raw application row in %s = %q, %t, %v", result.path, raw, exists, err)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if err := reopened.ViewAt(2, func(tx *Tx) error {
					_, ok, err := tx.GetCell(key)
					if ok || err != nil {
						t.Fatalf("tombstone in %s = %t, %v", result.path, ok, err)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestRepairMVCCIndexesToRequiresExistingSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "missing.db"), filepath.Join(dir, "candidate.db")
	err := RepairMVCCIndexesTo(t.Context(), source, destination, &Options{EnableMVCC: true}, nil)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source error = %v, want os.ErrNotExist", err)
	}
	for _, path := range []string{source, destination} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("repair created %s: %v", path, err)
		}
	}
}

func TestViewAtTimeAcrossUnixEpoch(t *testing.T) {
	t.Parallel()
	db := openMVCCRegressionDB(t, filepath.Join(t.TempDir(), "timeline.db"), &Options{EnableMVCC: true})
	key, err := Pack(Coord{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *Tx) error {
		return tx.PutCell(t.Context(), CellRecord{Key: key, RawContent: "later"})
	}); err != nil {
		t.Fatal(err)
	}
	beforeEpoch := time.Unix(-1, 0)
	assertSnapshot := func(asOf time.Time, want uint64) {
		t.Helper()
		if err := db.ViewAtTime(asOf, func(tx *Tx) error {
			if tx.readSeq != want {
				t.Errorf("snapshot at %s = %d, want %d", asOf, tx.readSeq, want)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	assertSnapshot(beforeEpoch, 0)
	// Model an existing timeline recorded with a clock on the other side of epoch.
	if err := db.Update(func(tx *Tx) error {
		return tx.putDirect(index.CommitTimeKey(beforeEpoch.UnixNano(), 1), nil)
	}); err != nil {
		t.Fatal(err)
	}
	assertSnapshot(beforeEpoch.Add(-time.Nanosecond), 0)
	assertSnapshot(beforeEpoch, 1)
	assertSnapshot(time.Unix(0, 0), 1)
	assertSnapshot(time.Time{}, 0)
	assertSnapshot(time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC), 2)
}

func openMVCCRegressionDB(t *testing.T, path string, opts *Options) *DB {
	t.Helper()
	db, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func checkMVCCSecondaryCounts(t *testing.T, tx *Tx, label string, week int64, wantCells, wantSeams int) {
	t.Helper()
	ctx := t.Context()
	tagCount, sourceCount, weekCount := 0, 0, 0
	seamSourceCount, seamWeekCount := 0, 0
	for _, err := range []error{
		tx.AscendCellsByTag(ctx, label, func(CellRecord) bool { tagCount++; return true }),
		tx.AscendCellsBySource(ctx, label, func(CellRecord) bool { sourceCount++; return true }),
		tx.AscendCellsInTimeBucket(ctx, week, func(CellRecord) bool { weekCount++; return true }),
		tx.AscendSeamsBySource(ctx, label, func(SeamRecord) bool { seamSourceCount++; return true }),
		tx.AscendSeamsInTimeBucket(ctx, week, func(SeamRecord) bool { seamWeekCount++; return true }),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if tagCount != wantCells || sourceCount != wantCells || weekCount != wantCells ||
		seamSourceCount != wantSeams || seamWeekCount != wantSeams {
		t.Errorf("snapshot %d, %q: cells tag/source/week = %d/%d/%d, want %d each; seams source/week = %d/%d, want %d each",
			tx.readSeq, label, tagCount, sourceCount, weekCount, wantCells, seamSourceCount, seamWeekCount, wantSeams)
	}
}
