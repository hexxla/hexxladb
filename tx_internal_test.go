package hexxladb

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestUpdate_changelogFailureRecoversCommittedStateAndEventAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recover-finalization.db")
	opts := &Options{EnableMVCC: true, ChangelogEnabled: true}
	db, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	coord, err := lattice.Pack(lattice.Coord{Q: 7, R: -3})
	if err != nil {
		t.Fatal(err)
	}
	cellRecord := record.CellRecord{Key: coord, RawContent: "recoverable-event"}

	err = db.Update(func(tx *Tx) error {
		if err := tx.PutCell(context.Background(), cellRecord); err != nil {
			return err
		}
		return tx.db.changelog.Close()
	})
	if !errors.Is(err, ErrCommitFinalization) {
		t.Fatalf("want ErrCommitFinalization, got %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.View(func(tx *Tx) error {
		got, ok, err := tx.GetCell(coord)
		if err != nil {
			return err
		}
		if !ok || got.RawContent != cellRecord.RawContent {
			t.Fatalf("committed cell after reopen: ok=%v record=%#v", ok, got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	changes, err := db.ReadChangelogSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Op != ChangelogOpPutCell {
		t.Fatalf("recovered changelog: %#v", changes)
	}
}

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
