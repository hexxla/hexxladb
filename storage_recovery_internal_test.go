package hexxladb

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb/internal/engine"
)

func TestOpenExistingRepairsTornMutableHeader(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		name := "plaintext"
		opts := &Options{}
		if encrypted {
			name = "encrypted"
			opts.EncryptionKey = []byte("torn-header-regression-key")
		}
		for _, zeroAllocator := range []bool{false, true} {
			mode := "mixed-header"
			if zeroAllocator {
				mode = "zero-allocator"
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "recovery.db")
				db, err := Open(path, opts)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Close() })
				key := []byte("app/key")
				if err := db.Update(func(tx *Tx) error { return tx.Put(key, []byte("old")) }); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Update(func(tx *Tx) error { return tx.Put(key, []byte("new")) }); err != nil {
					t.Fatal(err)
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if zeroAllocator {
					clear(after[24:32])
				} else {
					copy(after[24:512], before[24:512])
				}
				if err := os.WriteFile(path, after, 0o600); err != nil {
					t.Fatal(err)
				}
				db, err = OpenExisting(path, opts)
				if err != nil {
					t.Fatal(err)
				}
				if got := db.cachedHdr.Load().commitSeq; got != 2 {
					t.Fatalf("recovered commit sequence = %d", got)
				}
				if err := db.View(func(tx *Tx) error {
					got, ok, err := tx.Get(key)
					if err == nil && (!ok || !bytes.Equal(got, []byte("new"))) {
						t.Errorf("recovered value = %q, found %t", got, ok)
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
				if err := db.Update(func(tx *Tx) error { return tx.Put(key, []byte("after-recovery")) }); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestWALAdmissionErrorMapsToInvalidArgument(t *testing.T) {
	err := mapEngineDataError(engine.ErrWALTooLarge)
	if !errors.Is(err, ErrInvalidArgument) || !errors.Is(err, engine.ErrWALTooLarge) {
		t.Fatalf("WAL staging error = %v", err)
	}
}
