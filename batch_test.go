package hexxladb_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestImportCellsJSON_RequiresArray(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "import.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ImportCellsJSON(t.Context(), strings.NewReader(`{"key":"value"}`))
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("non-array import: want ErrInvalidArgument, got %v", err)
	}
}

func TestImportCellsJSON_StreamsAcrossBatchBoundary(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "import-batches.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cells := make([]record.CellRecord, 130)
	for i := range cells {
		p, err := lattice.Pack(lattice.Coord{Q: i, R: 0})
		if err != nil {
			t.Fatal(err)
		}
		cells[i] = record.CellRecord{Key: p, RawContent: "streamed"}
	}
	data, err := json.Marshal(cells)
	if err != nil {
		t.Fatal(err)
	}
	written, err := db.ImportCellsJSON(t.Context(), strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if written != len(cells) {
		t.Fatalf("written = %d, want %d", written, len(cells))
	}
}

func TestBatchPutCells_DoesNotReportRolledBackWrites(t *testing.T) {
	t.Parallel()
	rejected := errors.New("rejected cell")
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "batch-rollback.db"), &hexxladb.Options{
		CellValidator: hexxladb.CellValidatorFunc(func(rec record.CellRecord) error {
			if rec.RawContent == "reject" {
				return rejected
			}
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cells := make([]record.CellRecord, 2)
	for i := range cells {
		p, err := lattice.Pack(lattice.Coord{Q: i, R: 0})
		if err != nil {
			t.Fatal(err)
		}
		cells[i] = record.CellRecord{Key: p, RawContent: "accept"}
	}
	cells[1].RawContent = "reject"
	progressCalls := 0
	result, err := db.BatchPutCells(t.Context(), cells, &hexxladb.BatchPutCellOptions{
		OnProgress: func(int) { progressCalls++ },
	})
	if !errors.Is(err, rejected) {
		t.Fatalf("BatchPutCells error = %v, want validator error", err)
	}
	if result.Written != 0 || progressCalls != 0 {
		t.Fatalf("rolled-back batch reported written=%d progress=%d", result.Written, progressCalls)
	}
}

func TestBatchPutCells_SkippedCellLeavesNoPrimaryIndexOrEvent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		mvcc bool
	}{{"v1", false}, {"mvcc", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := openHookDB(t, &hexxladb.Options{EnableMVCC: tc.mvcc, ChangelogEnabled: true})
			rejected := mustPackTest(t, hexxladb.Coord{})
			accepted := mustPackTest(t, hexxladb.Coord{Q: 1})
			result, err := db.BatchPutCells(t.Context(), []hexxladb.CellRecord{
				{Key: rejected, RawContent: "rejected", Tags: []string{"first", strings.Repeat("z", 221)}},
				{Key: accepted, RawContent: "accepted"},
			}, &hexxladb.BatchPutCellOptions{ContinueOnError: true})
			if err != nil || result.Written != 1 || len(result.Errors) != 1 || result.Errors[0].Index != 0 ||
				!errors.Is(result.Errors[0], hexxladb.ErrInvalidArgument) {
				t.Fatalf("batch = %+v, %v", result, err)
			}
			if err := db.View(func(tx *hexxladb.Tx) error {
				if _, found, err := tx.GetCell(rejected); found || err != nil {
					t.Fatalf("rejected primary: found=%v err=%v", found, err)
				}
				return tx.AscendCellsByTag(t.Context(), "first", func(rec hexxladb.CellRecord) bool {
					t.Errorf("rejected row remained indexed: %+v", rec)
					return false
				})
			}); err != nil {
				t.Fatal(err)
			}
			changes, err := db.ReadChangelogSince(0, 10)
			if err != nil || len(changes) != 1 || !strings.Contains(string(changes[0].Inline), "accepted") {
				t.Fatalf("batch changes = %+v, %v", changes, err)
			}
		})
	}
}

func TestBatchPutCells_HookFailureAbortsChunk(t *testing.T) {
	t.Parallel()
	hookErr := errors.New("hook rejected staged cell")
	db := openHookDB(t, &hexxladb.Options{
		EnableMVCC:       true,
		ChangelogEnabled: true,
		AfterPutCell: hexxladb.AfterPutCellHookFunc(func(_ context.Context, cell hexxladb.CellRecord) error {
			if cell.RawContent == "hook error" {
				return hookErr
			}
			return nil
		}),
	})
	first := mustPackTest(t, hexxladb.Coord{})
	second := mustPackTest(t, hexxladb.Coord{Q: 1})
	result, err := db.BatchPutCells(t.Context(), []hexxladb.CellRecord{
		{Key: first}, {Key: second, RawContent: "hook error"},
	}, &hexxladb.BatchPutCellOptions{ContinueOnError: true})
	if !errors.Is(err, hookErr) || result.Written != 0 || len(result.Errors) != 0 {
		t.Fatalf("batch = %+v, %v", result, err)
	}
	err = db.Update(func(tx *hexxladb.Tx) error {
		_ = tx.PutCell(t.Context(), hexxladb.CellRecord{Key: second, RawContent: "hook error"})
		return nil
	})
	if !errors.Is(err, hookErr) {
		t.Fatalf("ignored hook error allowed update: %v", err)
	}
	if err := db.View(func(tx *hexxladb.Tx) error {
		for _, key := range []hexxladb.PackedCoord{first, second} {
			if _, found, err := tx.GetCell(key); found || err != nil {
				t.Fatalf("rolled-back cell: key=%v found=%v err=%v", key, found, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	changes, err := db.ReadChangelogSince(0, 10)
	if err != nil || len(changes) != 0 {
		t.Fatalf("rolled-back changes = %+v, %v", changes, err)
	}
}
