package hexxladb

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func resetTestOpen(t *testing.T, path string, opts *Options) *DB {
	t.Helper()
	db, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
func resetTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func resetTestHead(t *testing.T, db *DB) uint64 {
	t.Helper()
	stats, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}
	return stats.CommitSeq
}

func TestResetChangelogHistoryEncryptedCompaction(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "source.db"), filepath.Join(dir, "compact.db")
	key := []byte("reset compacted changelog test key")
	on := &Options{EncryptionKey: key, ChangelogEnabled: true}
	off := &Options{EncryptionKey: key}
	db := resetTestOpen(t, src, on)
	putChangelogIntegrityCell(t, db, "retained cell")
	consumers, err := db.ListChangelogConsumers()
	if err != nil || len(consumers) != 0 {
		t.Fatalf("consumers = %#v, %v", consumers, err)
	}
	head := resetTestHead(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	history := resetTestFile(t, src+"-changelog")
	if err := CompactTo(t.Context(), src, dst, off); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(dst, on)
	if opened != nil {
		_ = opened.Close()
		t.Fatal("accepted fresh history before reset")
	}
	if !errors.Is(err, ErrChangelogCorrupt) {
		t.Fatalf("fresh log error = %v", err)
	}
	if err := os.WriteFile(dst+"-changelog", history, 0600); err != nil {
		t.Fatal(err)
	}
	opened, err = Open(dst, on)
	if opened != nil {
		_ = opened.Close()
		t.Fatal("accepted source encryption identity")
	}
	if !errors.Is(err, ErrChangelogEncryptionKeyMismatch) {
		t.Fatalf("old log error = %v", err)
	}
	admin := resetTestOpen(t, dst, off)
	for range 2 {
		if err := admin.ResetChangelogHistory(); err != nil {
			t.Fatal(err)
		}
	}
	if resetTestHead(t, admin) != head || !bytes.Equal(history, resetTestFile(t, dst+"-changelog")) {
		t.Fatal("reset changed commit or sidecar")
	}
	if err := admin.Close(); err != nil {
		t.Fatal(err)
	}
	archive := dst + "-changelog.archived"
	if err := os.Rename(dst+"-changelog", archive); err != nil {
		t.Fatal(err)
	}
	reopened := resetTestOpen(t, dst, on)
	if resetTestHead(t, reopened) != head {
		t.Fatal("reopen changed commit")
	}
	if err := reopened.View(func(tx *Tx) error {
		cell, ok, err := tx.GetCell(PackedCoord{})
		if err != nil || !ok || cell.RawContent != "retained cell" {
			t.Fatalf("cell = %#v, %t, %v", cell, ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	changes, err := reopened.ReadChangelogSince(0, 10)
	if err != nil || len(changes) != 0 {
		t.Fatalf("fresh history = %#v, %v", changes, err)
	}
	putChangelogIntegrityCell(t, reopened, "new cell content")
	changes, err = reopened.ReadChangelogSince(0, 10)
	if err != nil || len(changes) != 1 || changes[0].Seq != 1 {
		t.Fatalf("new history = %#v, %v", changes, err)
	}
	if resetTestHead(t, reopened) != head+1 {
		t.Fatal("unexpected next commit")
	}
	for _, p := range []string{archive, src + "-changelog"} {
		if !bytes.Equal(history, resetTestFile(t, p)) {
			t.Fatal("changed archived/source history")
		}
	}
}

func TestResetChangelogHistoryGuardsAndAbsentCheckpoint(t *testing.T) {
	var absent *DB
	if err := absent.ResetChangelogHistory(); !errors.Is(err, ErrDatabaseClosed) {
		t.Fatalf("nil reset = %v", err)
	}
	path := filepath.Join(t.TempDir(), "data.db")
	db := resetTestOpen(t, path, nil)
	before, head := resetTestFile(t, path), resetTestHead(t, db)
	for range 2 {
		if err := db.ResetChangelogHistory(); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(before, resetTestFile(t, path)) || resetTestHead(t, db) != head {
		t.Fatal("absent reset changed state")
	}
	if _, err := os.Stat(path + "-changelog"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reset created sidecar: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.ResetChangelogHistory(); !errors.Is(err, ErrDatabaseClosed) {
		t.Fatalf("closed reset = %v", err)
	}
	db = resetTestOpen(t, path, &Options{ChangelogEnabled: true})
	putChangelogIntegrityCell(t, db, "acknowledged")
	before, sidecar := resetTestFile(t, path), resetTestFile(t, path+"-changelog")
	if err := db.ResetChangelogHistory(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("enabled reset = %v", err)
	}
	if !bytes.Equal(before, resetTestFile(t, path)) || !bytes.Equal(sidecar, resetTestFile(t, path+"-changelog")) {
		t.Fatal("rejected reset changed files")
	}
	if err := db.AdvanceChangelogConsumer("reader", 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	admin := resetTestOpen(t, path, nil)
	before = resetTestFile(t, path)
	if err := admin.ResetChangelogHistory(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("consumer reset = %v", err)
	}
	seq, exists, err := admin.GetChangelogConsumerCursor("reader")
	if err != nil || !exists || seq != 1 || !bytes.Equal(before, resetTestFile(t, path)) {
		t.Fatalf("protected cursor = %d, %t, %v", seq, exists, err)
	}
}

func TestResetChangelogHistoryPreservesPendingOutbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	key := []byte("reset pending outbox test key")
	on := &Options{EncryptionKey: key, ChangelogEnabled: true}
	db := resetTestOpen(t, path, on)
	putChangelogIntegrityCell(t, db, "acknowledged")
	db.commitFaults = &commitFaultHooks{beforeChangelogAppend: func() error { return errInjectedCommitBoundary }}
	err := db.Update(func(tx *Tx) error {
		return tx.PutCell(t.Context(), CellRecord{Key: PackedCoord{}, RawContent: "pending"})
	})
	if !errors.Is(err, ErrCommitDurable) {
		t.Fatalf("projection fault = %v", err)
	}
	if err := db.ResetChangelogHistory(); !errors.Is(err, ErrCommitFinalization) {
		t.Fatalf("uncertain reset = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	history := resetTestFile(t, path+"-changelog")
	admin := resetTestOpen(t, path, &Options{EncryptionKey: key})
	pending, err := admin.readPendingChangelogIntents()
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %#v, %v", pending, err)
	}
	head := resetTestHead(t, admin)
	if err := admin.ResetChangelogHistory(); err != nil {
		t.Fatal(err)
	}
	after, err := admin.readPendingChangelogIntents()
	if err != nil || !reflect.DeepEqual(pending, after) {
		t.Fatalf("changed pending = %#v, %v", after, err)
	}
	if resetTestHead(t, admin) != head || !bytes.Equal(history, resetTestFile(t, path+"-changelog")) {
		t.Fatal("reset changed commit or sidecar")
	}
	if err := admin.Close(); err != nil {
		t.Fatal(err)
	}
	archive := path + "-changelog.archived"
	if err := os.Rename(path+"-changelog", archive); err != nil {
		t.Fatal(err)
	}
	recovered := resetTestOpen(t, path, on)
	changes, err := recovered.ReadChangelogSince(0, 10)
	if err != nil || len(changes) != 1 || changes[0].Seq != 1 || !bytes.Contains(changes[0].Inline, []byte("pending")) {
		t.Fatalf("recovered = %#v, %v", changes, err)
	}
	remaining, err := recovered.readPendingChangelogIntents()
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining = %#v, %v", remaining, err)
	}
	if resetTestHead(t, recovered) != head || !bytes.Equal(history, resetTestFile(t, archive)) {
		t.Fatal("recovery changed commit or archive")
	}
}
