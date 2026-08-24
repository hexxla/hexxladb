package hexxladb

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
)

func TestChangelogConsumerRejectsCorruptMetadata(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		key  func() []byte
	}{
		{name: "cursor", key: func() []byte { return index.ChangelogConsumerKey("reader") }},
		{name: "checkpoint", key: index.ChangelogProjectionCheckpointKey},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "corrupt.db")
			db, err := Open(path, &Options{ChangelogEnabled: true})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if err := db.AdvanceChangelogConsumer("reader", 0, 0); err != nil {
				t.Fatal(err)
			}
			db.mu.Lock()
			if err := db.eng.BeginWriteTxn(); err != nil {
				db.mu.Unlock()
				t.Fatal(err)
			}
			if err := db.btree.Put(test.key(), []byte("corrupt")); err != nil {
				db.eng.AbortWriteTxn()
				db.mu.Unlock()
				t.Fatal(err)
			}
			if err := db.eng.CommitWriteTxn(); err != nil {
				db.mu.Unlock()
				t.Fatal(err)
			}
			db.mu.Unlock()

			if _, err := db.ListChangelogConsumers(); !errors.Is(err, ErrCorruptDatabase) {
				t.Fatalf("corrupt %s: want ErrCorruptDatabase, got %v", test.name, err)
			}
		})
	}
}
