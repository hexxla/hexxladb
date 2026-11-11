package hexxladb_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestCapabilityStatusWithoutPhysicalKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.hexxla")
	db, err := hexxladb.Open(path, &hexxladb.Options{EmbeddingDimension: 2, ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	retained, err := db.HasRetainedChangefeed()
	if err != nil || retained {
		t.Fatalf("empty status=%v,%v", retained, err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if tx.ReadSequence() != 0 {
			t.Fatalf("base seq=%d", tx.ReadSequence())
		}
		for i := range 2 {
			key, _ := hexxladb.Pack(hexxladb.Coord{Q: i})
			if err := tx.PutEmbedding(key, []float32{1, 0}); err != nil {
				return err
			}
		}
		key, _ := hexxladb.Pack(hexxladb.Coord{})
		return tx.PutCell(t.Context(), hexxladb.CellRecord{Key: key, RawContent: "event"})
	}); err != nil {
		t.Fatal(err)
	}
	retained, err = db.HasRetainedChangefeed()
	if err != nil || !retained {
		t.Fatalf("committed status=%v,%v", retained, err)
	}
	if err := db.View(func(tx *hexxladb.Tx) error {
		if tx.ReadSequence() != 1 {
			t.Fatalf("seq=%d", tx.ReadSequence())
		}
		count, err := tx.CountEmbeddings(t.Context(), 1)
		if err != nil || count != 1 {
			t.Fatalf("bounded count=%d,%v", count, err)
		}
		count, err = tx.CountEmbeddings(t.Context(), 10)
		if err != nil || count != 2 {
			t.Fatalf("count=%d,%v", count, err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err = tx.CountEmbeddings(ctx, 1)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled=%v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.ViewAt(0, func(tx *hexxladb.Tx) error {
		_, err := tx.CountEmbeddings(t.Context(), 1)
		if !errors.Is(err, hexxladb.ErrHistoricalDataUnavailable) {
			t.Fatalf("historical vectors=%v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = hexxladb.OpenExisting(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	retained, err = db.HasRetainedChangefeed()
	if err != nil || !retained {
		t.Fatalf("disabled handle status=%v,%v", retained, err)
	}
}
