package hexxladb_test

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestRawOverflowMarkerRoundTrip(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		name := "plaintext"
		opts := &hexxladb.Options{}
		if encrypted {
			name = "encrypted"
			opts.EncryptionKey = []byte("storage-regression-test-key")
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "raw.db")
			db, err := hexxladb.Open(path, opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			marker := []byte{0xff, 0x4f}
			marker = binary.BigEndian.AppendUint32(marker, 1)
			marker = binary.BigEndian.AppendUint64(marker, 0)
			key := []byte("app/raw-marker")
			if err := db.Update(func(tx *hexxladb.Tx) error { return tx.Put(key, marker) }); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db, err = hexxladb.OpenExisting(path, opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.View(func(tx *hexxladb.Tx) error {
				got, ok, err := tx.Get(key)
				if err == nil && (!ok || !bytes.Equal(got, marker)) {
					t.Errorf("marker round trip = %x, found %t", got, ok)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Update(func(tx *hexxladb.Tx) error { return tx.Put(key, []byte("replacement")) }); err != nil {
				t.Fatal(err)
			}
		})
	}
}
