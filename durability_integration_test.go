//go:build integration

package hexxladb_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestIntegration_manyPutsSurviveReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "stress.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	const n = 500
	for i := range n {
		k := []byte(fmt.Sprintf("key%04d", i))
		v := []byte(fmt.Sprintf("val%04d", i))
		err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.Put(k, v)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	err = db2.View(func(tx *hexxladb.Tx) error {
		for i := range n {
			k := []byte(fmt.Sprintf("key%04d", i))
			want := []byte(fmt.Sprintf("val%04d", i))
			got, ok, err := tx.Get(k)
			if err != nil {
				return err
			}
			if !ok || string(got) != string(want) {
				t.Fatalf("key %s: ok=%v got=%q", k, ok, got)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
