package hexxladbout

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	hxdb "github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/examples/ports_adapters/internal/app"
	"github.com/hexxla/hexxladb/examples/ports_adapters/internal/domain"
)

var (
	left  = domain.Observation{Text: "The valve is open."}
	right = domain.Observation{At: domain.Position{Q: 1}, Text: "The valve is closed."}
)

func TestContradictionRoundTrip(t *testing.T) {
	t.Parallel()
	db := openTestDB(t, &hxdb.Options{})
	service := app.New(New(db))
	id, err := service.Record(t.Context(), left, right, "conflicting inspections")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Resolve(t.Context(), id, "Verified the valve is closed."); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *hxdb.Tx) error {
		for _, obs := range []domain.Observation{left, right} {
			key, err := hxdb.Pack(hxdb.Coord{Q: obs.At.Q, R: obs.At.R})
			if err != nil {
				return err
			}
			rec, ok, err := tx.GetCell(key)
			if err != nil || !ok || rec.RawContent != obs.Text {
				t.Fatalf("observation = %+v, found=%v, err=%v", rec, ok, err)
			}
		}
		var count int
		err := tx.AscendSeams(t.Context(), func(seam hxdb.SeamRecord) bool {
			count++
			if seam.ID != id || seam.SeamType != hxdb.SeamTypeConflict ||
				seam.ResolutionStatus != "resolved" || seam.ResolutionNote != "Verified the valve is closed." {
				t.Fatalf("unexpected resolved seam: %+v", seam)
			}
			return true
		})
		if count != 1 {
			t.Fatalf("visible seam count = %d, want 1", count)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordConflictRollsBackWhenSeamWriteFails(t *testing.T) {
	t.Parallel()
	rejected := errors.New("seam rejected")
	db := openTestDB(t, &hxdb.Options{

		AfterPutSeam: hxdb.AfterPutSeamHookFunc(func(context.Context, hxdb.SeamRecord) error {
			return rejected
		}),
	})
	id, err := app.New(New(db)).Record(t.Context(), left, right, "conflicting inspections")
	if id != "" || !errors.Is(err, rejected) {
		t.Fatalf("record: id=%q, err=%v, want rejection", id, err)
	}
	report, err := db.HealthCheck(t.Context(), hxdb.HealthCheckConfig{})
	if err != nil || report.CellCount != 0 || report.SeamCount != 0 {
		t.Fatalf("rejected transaction left records: %+v, err=%v", report, err)
	}
}

func openTestDB(t *testing.T, opts *hxdb.Options) *hxdb.DB {
	t.Helper()
	db, err := hxdb.Open(filepath.Join(t.TempDir(), "conflicts.db"), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
