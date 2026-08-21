package hexxladb_test

import (
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
