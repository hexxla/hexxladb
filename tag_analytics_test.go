package hexxladb_test

import (
	"context"
	"path/filepath"
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
