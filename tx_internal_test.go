package hexxladb

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestUpdate_wrapsFinalizationError_changelog(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "chg_fail.db")
	db, err := Open(path, &Options{ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	err = db.Update(func(tx *Tx) error {
		if err := tx.Put([]byte("k"), []byte("v")); err != nil {
			return err
		}
		tx.noteChangelog(1, []byte("k"), []byte("v"))
		if tx.db.changelog != nil {
			_ = tx.db.changelog.Close()
		}
		return nil
	})
	if !errors.Is(err, ErrCommitFinalization) {
		t.Fatalf("want ErrCommitFinalization, got %v", err)
	}
}

func TestUpdate_wrapsFinalizationError_header(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "hdr_fail.db")
	db, err := Open(path, &Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	err = db.Update(func(tx *Tx) error {
		if err := tx.Put([]byte("k"), []byte("v")); err != nil {
			return err
		}
		_ = tx.db.eng.Close()
		return nil
	})
	if !errors.Is(err, ErrCommitFinalization) {
		t.Fatalf("want ErrCommitFinalization, got %v", err)
	}
}
