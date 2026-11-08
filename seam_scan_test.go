package hexxladb_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestAscendSeamsUsesTransactionSnapshot(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "seams.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	a, err := hexxladb.Pack(hexxladb.Coord{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := hexxladb.Pack(hexxladb.Coord{Q: 1})
	if err != nil {
		t.Fatal(err)
	}
	first := hexxladb.SeamRecord{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", CellA: a, CellB: b,
		SeamType: hexxladb.SeamTypeConflict, Reason: "original",
	}
	second := first
	second.ID, second.Reason = "01ARZ3NDEKTSV4RRFFQ69G5FAW", "second"
	ctx := t.Context()
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutSeam(ctx, first); err != nil {
			return err
		}
		return tx.PutSeam(ctx, second)
	}); err != nil {
		t.Fatal(err)
	}
	check := func(tx *hexxladb.Tx, limit int, want ...string) error {
		var got []string
		err := tx.AscendSeams(ctx, func(seam hexxladb.SeamRecord) bool {
			got = append(got, seam.Reason)
			return len(got) < limit
		})
		if !slices.Equal(got, want) {
			t.Errorf("seam reasons = %v, want %v", got, want)
		}
		return err
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		first.Reason = "edited"
		if err := tx.PutSeam(ctx, first); err != nil {
			return err
		}
		if err := tx.Put([]byte("seam0"), []byte("application key")); err != nil {
			return err
		}
		return check(tx, 10, "edited", "second")
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		seq   uint64
		limit int
		want  []string
	}{
		{seq: 0, limit: 10},
		{seq: 1, limit: 10, want: []string{"original", "second"}},
		{seq: 2, limit: 10, want: []string{"edited", "second"}},
		{seq: 2, limit: 1, want: []string{"edited"}},
	} {
		if err := db.ViewAt(tc.seq, func(tx *hexxladb.Tx) error { return check(tx, tc.limit, tc.want...) }); err != nil {
			t.Fatal(err)
		}
	}
	report, err := db.HealthCheck(ctx, hexxladb.HealthCheckConfig{})
	if err != nil || report.SeamCount != 2 {
		t.Fatalf("health seams = %d, err=%v", report.SeamCount, err)
	}
}
