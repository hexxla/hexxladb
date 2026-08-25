package hexxladb

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

var errInjectedCommitBoundary = errors.New("injected commit boundary failure")

func TestUpdate_precommitFaultsAbortStateAndRemainRetryable(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(*commitFaultHooks)
	}{
		{
			name: "MVCC header publication",
			apply: func(h *commitFaultHooks) {
				h.beforeCommitSeqPublish = func() error { return errInjectedCommitBoundary }
			},
		},
		{
			name: "engine commit",
			apply: func(h *commitFaultHooks) {
				h.beforeEngineCommit = func() error { return errInjectedCommitBoundary }
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "precommit.db")
			db, coord := openCommitRecoveryDB(t, path, false)
			hooks := &commitFaultHooks{}
			test.apply(hooks)
			db.commitFaults = hooks

			err := putRecoveryCell(t, db, coord, "first-attempt")
			if !errors.Is(err, ErrCommitFinalization) || errors.Is(err, ErrCommitDurable) {
				t.Fatalf("precommit error classification: %v", err)
			}
			assertRecoveryCell(t, db, coord, "", false)
			if changes, err := db.ReadChangelogSince(0, 10); err != nil || len(changes) != 0 {
				t.Fatalf("precommit changelog: records=%#v err=%v", changes, err)
			}

			db.commitFaults = nil
			if err := putRecoveryCell(t, db, coord, "retry"); err != nil {
				t.Fatalf("retry after known abort: %v", err)
			}
			assertRecoveryCell(t, db, coord, "retry", true)
			changes, err := db.ReadChangelogSince(0, 10)
			if err != nil || len(changes) != 1 {
				t.Fatalf("retry changelog: records=%#v err=%v", changes, err)
			}
		})
	}
}

func TestUpdate_postcommitFaultsRecoverStateAndEventAfterReopen(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(*commitFaultHooks)
	}{
		{
			name: "after engine commit",
			apply: func(h *commitFaultHooks) {
				h.afterEngineCommit = func() error { return errInjectedCommitBoundary }
			},
		},
		{
			name: "changelog append",
			apply: func(h *commitFaultHooks) {
				h.beforeChangelogAppend = func() error { return errInjectedCommitBoundary }
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "postcommit.db")
			db, coord := openCommitRecoveryDB(t, path, false)
			hooks := &commitFaultHooks{}
			test.apply(hooks)
			db.commitFaults = hooks

			err := putRecoveryCell(t, db, coord, "durable")
			if !errors.Is(err, ErrCommitFinalization) || !errors.Is(err, ErrCommitDurable) {
				t.Fatalf("postcommit error classification: %v", err)
			}
			assertRecoveryCell(t, db, coord, "durable", true)
			called := false
			err = db.Update(func(*Tx) error {
				called = true
				return nil
			})
			if !errors.Is(err, ErrCommitFinalization) || called {
				t.Fatalf("writer was not blocked pending reopen: called=%v err=%v", called, err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			db, err = Open(path, commitRecoveryOptions(false))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			assertRecoveryCell(t, db, coord, "durable", true)
			changes, err := db.ReadChangelogSince(0, 10)
			if err != nil || len(changes) != 1 || changes[0].Op != ChangelogOpPutCell {
				t.Fatalf("recovered changelog: records=%#v err=%v", changes, err)
			}
			pending, err := db.readPendingChangelogIntents()
			if err != nil || len(pending) != 0 {
				t.Fatalf("recovered outbox: pending=%#v err=%v", pending, err)
			}
		})
	}
}

func TestUpdate_cleanupFaultMayRedeliverButDoesNotDuplicatePrimaryCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cleanup.db")
	db, coord := openCommitRecoveryDB(t, path, false)
	db.commitFaults = &commitFaultHooks{
		beforeOutboxCleanup: func() error { return errInjectedCommitBoundary },
	}
	err := putRecoveryCell(t, db, coord, "once")
	if !errors.Is(err, ErrCommitFinalization) || !errors.Is(err, ErrCommitDurable) {
		t.Fatalf("cleanup error classification: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path, commitRecoveryOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryCell(t, db, coord, "once", true)
	stats, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}
	if stats.CommitSeq != 1 || stats.VersionedRows != 1 {
		t.Fatalf("primary commit duplicated: %#v", stats)
	}
	changes, err := db.ReadChangelogSince(0, 10)
	if err != nil || len(changes) != 2 {
		t.Fatalf("at-least-once redelivery: records=%#v err=%v", changes, err)
	}
	if changes[0].Op != changes[1].Op || !bytes.Equal(changes[0].Key, changes[1].Key) || changes[0].Hash != changes[1].Hash {
		t.Fatalf("redelivered records do not describe the same mutation: %#v", changes)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path, commitRecoveryOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	changes, err = db.ReadChangelogSince(0, 10)
	if err != nil || len(changes) != 2 {
		t.Fatalf("cleaned outbox redelivered again: records=%#v err=%v", changes, err)
	}
}

func TestUpdate_lazyChangelogSyncsAndCleansOutboxOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lazy.db")
	db, coord := openCommitRecoveryDB(t, path, true)
	if err := putRecoveryCell(t, db, coord, "lazy"); err != nil {
		t.Fatal(err)
	}
	pending, err := db.readPendingChangelogIntents()
	if err != nil || len(pending) != 1 {
		t.Fatalf("lazy pending outbox: pending=%#v err=%v", pending, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path, commitRecoveryOptions(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pending, err = db.readPendingChangelogIntents()
	if err != nil || len(pending) != 0 {
		t.Fatalf("lazy outbox after clean close: pending=%#v err=%v", pending, err)
	}
	changes, err := db.ReadChangelogSince(0, 10)
	if err != nil || len(changes) != 1 {
		t.Fatalf("lazy changelog after reopen: records=%#v err=%v", changes, err)
	}
}

func TestUpdate_formatV1ChangelogFailureRecoversAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "format-v1.db")
	opts := &Options{ChangelogEnabled: true}
	db, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	coord, err := lattice.Pack(lattice.Coord{Q: -5, R: 2})
	if err != nil {
		t.Fatal(err)
	}
	db.commitFaults = &commitFaultHooks{
		beforeChangelogAppend: func() error { return errInjectedCommitBoundary },
	}
	err = putRecoveryCell(t, db, coord, "format-v1")
	if !errors.Is(err, ErrCommitDurable) {
		t.Fatalf("format-v1 durable classification: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	assertRecoveryCell(t, db, coord, "format-v1", true)
	changes, err := db.ReadChangelogSince(0, 10)
	if err != nil || len(changes) != 1 {
		t.Fatalf("format-v1 recovered changes: records=%#v err=%v", changes, err)
	}
}

func TestOpen_recoversIncompleteProjectedTailFromPrimaryOutbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "incomplete-tail.db")
	db, coord := openCommitRecoveryDB(t, path, false)
	db.commitFaults = &commitFaultHooks{
		beforeOutboxCleanup: func() error { return errInjectedCommitBoundary },
	}
	if err := putRecoveryCell(t, db, coord, "repair-tail"); !errors.Is(err, ErrCommitDurable) {
		t.Fatalf("cleanup failure: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	changelogPath := path + "-changelog"
	info, err := os.Stat(changelogPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(changelogPath, info.Size()-3); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path, commitRecoveryOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	assertRecoveryCell(t, db, coord, "repair-tail", true)
	changes, err := db.ReadChangelogSince(0, 10)
	if err != nil || len(changes) != 1 {
		t.Fatalf("recovered incomplete tail: records=%#v err=%v", changes, err)
	}
}

func TestOpen_encryptedPrimaryOutboxRecoversEncryptedChangelog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "encrypted-outbox.db")
	opts := &Options{
		EnableMVCC:       true,
		ChangelogEnabled: true,
		EncryptionKey:    bytes.Repeat([]byte{0x39}, 32),
	}
	db, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	coord, err := lattice.Pack(lattice.Coord{Q: 9, R: -4})
	if err != nil {
		t.Fatal(err)
	}
	db.commitFaults = &commitFaultHooks{
		beforeChangelogAppend: func() error { return errInjectedCommitBoundary },
	}
	if err := putRecoveryCell(t, db, coord, "encrypted-recovery"); !errors.Is(err, ErrCommitDurable) {
		t.Fatalf("encrypted finalization: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	assertRecoveryCell(t, db, coord, "encrypted-recovery", true)
	changes, err := db.ReadChangelogSince(0, 10)
	if err != nil || len(changes) != 1 || changes[0].Op != ChangelogOpPutCell || len(changes[0].Inline) == 0 {
		t.Fatalf("encrypted recovered changes: records=%#v err=%v", changes, err)
	}
}

func openCommitRecoveryDB(t *testing.T, path string, lazy bool) (*DB, lattice.PackedCoord) {
	t.Helper()
	db, err := Open(path, commitRecoveryOptions(lazy))
	if err != nil {
		t.Fatal(err)
	}
	coord, err := lattice.Pack(lattice.Coord{Q: 3, R: -2})
	if err != nil {
		t.Fatal(err)
	}
	return db, coord
}

func commitRecoveryOptions(lazy bool) *Options {
	return &Options{EnableMVCC: true, ChangelogEnabled: true, ChangelogLazy: lazy}
}

func putRecoveryCell(t *testing.T, db *DB, coord lattice.PackedCoord, content string) error {
	t.Helper()
	return db.Update(func(tx *Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{Key: coord, RawContent: content})
	})
}

func assertRecoveryCell(t *testing.T, db *DB, coord lattice.PackedCoord, content string, want bool) {
	t.Helper()
	if err := db.View(func(tx *Tx) error {
		got, ok, err := tx.GetCell(coord)
		if err != nil {
			return err
		}
		if ok != want || ok && got.RawContent != content {
			t.Fatalf("cell: ok=%v want=%v record=%#v", ok, want, got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
