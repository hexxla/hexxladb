package hexxladb_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestTagCooccurrences_CountsEachCellOnce(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "tags.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := mustPackTest(t, hexxladb.Coord{})
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(context.Background(), record.CellRecord{Key: p, Tags: []string{"alpha", "beta"}})
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.View(func(tx *hexxladb.Tx) error {
		pairs, err := tx.TagCooccurrences(t.Context(), 1)
		if err != nil {
			return err
		}
		if len(pairs) != 1 || pairs[0].A != "alpha" || pairs[0].B != "beta" || pairs[0].Count != 1 {
			t.Fatalf("TagCooccurrences: got %#v", pairs)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTagCountsRevalidatesMVCCSecondaryMembership(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "tag-counts.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	first := mustPackTest(t, hexxladb.Coord{Q: 1})
	second := mustPackTest(t, hexxladb.Coord{Q: 2})
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(t.Context(), record.CellRecord{Key: first, Tags: []string{"alpha"}}); err != nil {
			return err
		}
		return tx.PutCell(t.Context(), record.CellRecord{Key: second, Tags: []string{"alpha"}})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{Key: first, Tags: []string{"beta"}})
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.View(func(tx *hexxladb.Tx) error {
		counts, err := tx.TagCounts(t.Context())
		if err != nil {
			return err
		}
		want := []hexxladb.TagCount{{Tag: "alpha", Count: 1}, {Tag: "beta", Count: 1}}
		if !reflect.DeepEqual(counts, want) {
			t.Fatalf("TagCounts = %#v, want %#v", counts, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
