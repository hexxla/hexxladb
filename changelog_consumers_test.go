package hexxladb_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func openConsumerDB(t *testing.T, path string) *hexxladb.DB {
	t.Helper()
	db, err := hexxladb.Open(path, &hexxladb.Options{ChangelogEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func putConsumerCell(t *testing.T, db *hexxladb.DB, q int) {
	t.Helper()
	key, err := hexxladb.Pack(hexxladb.Coord{Q: q, R: -q})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), hexxladb.CellRecord{Key: key, RawContent: "value"})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestChangelogConsumerDurableAtLeastOnceResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consumer.db")
	db := openConsumerDB(t, path)
	putConsumerCell(t, db, 1)
	putConsumerCell(t, db, 2)
	if err := db.AdvanceChangelogConsumer("projector", 0, 0); err != nil {
		t.Fatal(err)
	}
	records, err := db.ReadChangelogSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("metadata write emitted a changefeed record: got %d records, want 2", len(records))
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db = openConsumerDB(t, path)
	cursor, exists, err := db.GetChangelogConsumerCursor("projector")
	if err != nil {
		t.Fatal(err)
	}
	if !exists || cursor != 0 {
		t.Fatalf("reopened cursor: exists=%v seq=%d, want true/0", exists, cursor)
	}
	redelivered, err := db.ReadChangelogSince(cursor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(redelivered) != 2 {
		t.Fatalf("unacknowledged delivery after restart: got %d records, want 2", len(redelivered))
	}
	if err := db.AdvanceChangelogConsumer("projector", cursor, redelivered[0].Seq); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db = openConsumerDB(t, path)
	t.Cleanup(func() { _ = db.Close() })
	cursor, exists, err = db.GetChangelogConsumerCursor("projector")
	if err != nil {
		t.Fatal(err)
	}
	if !exists || cursor != records[0].Seq {
		t.Fatalf("advanced cursor after restart: exists=%v seq=%d, want true/%d", exists, cursor, records[0].Seq)
	}
	tail, err := db.ReadChangelogSince(cursor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 1 || tail[0].Seq != records[1].Seq {
		t.Fatalf("resume omitted or reordered the tail: %#v", tail)
	}
	if err := db.AdvanceChangelogConsumer("projector", cursor, tail[0].Seq); err != nil {
		t.Fatal(err)
	}
	empty, err := db.ReadChangelogSince(tail[0].Seq, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("tail after final acknowledgment: %#v", empty)
	}
}

func TestChangelogConsumerCompareAdvanceRetentionAndDelete(t *testing.T) {
	t.Parallel()
	db := openConsumerDB(t, filepath.Join(t.TempDir(), "consumer.db"))
	t.Cleanup(func() { _ = db.Close() })

	for _, id := range []string{"", " leading", "slash/id"} {
		if err := db.AdvanceChangelogConsumer(id, 0, 0); !errors.Is(err, hexxladb.ErrInvalidArgument) {
			t.Fatalf("consumer ID %q: want ErrInvalidArgument, got %v", id, err)
		}
	}
	if err := db.AdvanceChangelogConsumer("beta", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.AdvanceChangelogConsumer("alpha", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.AdvanceChangelogConsumer("alpha", 1, 1); !errors.Is(err, hexxladb.ErrChangelogCursorConflict) {
		t.Fatalf("stale expected sequence: want ErrChangelogCursorConflict, got %v", err)
	}
	if err := db.AdvanceChangelogConsumer("alpha", 0, 1); !errors.Is(err, hexxladb.ErrChangelogCursorBeyondHead) {
		t.Fatalf("advance beyond head: want ErrChangelogCursorBeyondHead, got %v", err)
	}
	putConsumerCell(t, db, 3)
	records, err := db.ReadChangelogSince(0, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("read head: records=%#v err=%v", records, err)
	}
	if err := db.AdvanceChangelogConsumer("alpha", 0, records[0].Seq); err != nil {
		t.Fatal(err)
	}
	if err := db.AdvanceChangelogConsumer("alpha", records[0].Seq, 0); !errors.Is(err, hexxladb.ErrChangelogCursorRegression) {
		t.Fatalf("regression: want ErrChangelogCursorRegression, got %v", err)
	}

	consumers, err := db.ListChangelogConsumers()
	if err != nil {
		t.Fatal(err)
	}
	if len(consumers) != 2 || consumers[0].ConsumerID != "alpha" || consumers[1].ConsumerID != "beta" {
		t.Fatalf("ordered consumers: %#v", consumers)
	}
	floor, exists, err := db.ChangelogRetentionFloor()
	if err != nil {
		t.Fatal(err)
	}
	if !exists || floor != 0 {
		t.Fatalf("retention floor: exists=%v seq=%d, want true/0", exists, floor)
	}
	if err := db.DeleteChangelogConsumer("beta", 1); !errors.Is(err, hexxladb.ErrChangelogCursorConflict) {
		t.Fatalf("delete conflict: want ErrChangelogCursorConflict, got %v", err)
	}
	if err := db.DeleteChangelogConsumer("beta", 0); err != nil {
		t.Fatal(err)
	}
	floor, exists, err = db.ChangelogRetentionFloor()
	if err != nil {
		t.Fatal(err)
	}
	if !exists || floor != records[0].Seq {
		t.Fatalf("retention floor after delete: exists=%v seq=%d, want true/%d", exists, floor, records[0].Seq)
	}
	if err := db.DeleteChangelogConsumer("beta", 0); !errors.Is(err, hexxladb.ErrChangelogConsumerNotFound) {
		t.Fatalf("second delete: want ErrChangelogConsumerNotFound, got %v", err)
	}
	if _, exists, err := db.GetChangelogConsumerCursor("missing"); err != nil || exists {
		t.Fatalf("missing consumer: exists=%v err=%v", exists, err)
	}
	if got, err := db.ReadChangelogSince(0, 10); err != nil || len(got) != 1 {
		t.Fatalf("cursor metadata recursively emitted records: got=%#v err=%v", got, err)
	}
}

func TestChangelogConsumerBackupPreservesCursor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db := openConsumerDB(t, sourcePath)
	putConsumerCell(t, db, 4)
	records, err := db.ReadChangelogSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AdvanceChangelogConsumer("backup-reader", 0, records[0].Seq); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(dir, "backup.db")
	if err := db.BackupTo(t.Context(), backupPath); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	restored := openConsumerDB(t, backupPath)
	t.Cleanup(func() { _ = restored.Close() })
	cursor, exists, err := restored.GetChangelogConsumerCursor("backup-reader")
	if err != nil {
		t.Fatal(err)
	}
	if !exists || cursor != records[0].Seq {
		t.Fatalf("restored cursor: exists=%v seq=%d, want true/%d", exists, cursor, records[0].Seq)
	}
}

func TestChangelogConsumerCompactionRequiresMatchingSidecar(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	db := openConsumerDB(t, sourcePath)
	putConsumerCell(t, db, 5)
	records, err := db.ReadChangelogSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AdvanceChangelogConsumer("compacted-reader", 0, records[0].Seq); err != nil {
		t.Fatal(err)
	}
	goodPath := filepath.Join(dir, "good.db")
	badPath := filepath.Join(dir, "bad.db")
	if err := db.Compact(t.Context(), goodPath); err != nil {
		t.Fatal(err)
	}
	if err := db.Compact(t.Context(), badPath); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	copyTestFile(t, sourcePath+"-changelog", goodPath+"-changelog")
	good := openConsumerDB(t, goodPath)
	if err := good.Close(); err != nil {
		t.Fatal(err)
	}

	replacementPath := filepath.Join(dir, "replacement.db")
	replacement := openConsumerDB(t, replacementPath)
	putConsumerCell(t, replacement, 99)
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	copyTestFile(t, replacementPath+"-changelog", badPath+"-changelog")
	opened, err := hexxladb.Open(badPath, &hexxladb.Options{ChangelogEnabled: true})
	if opened != nil {
		_ = opened.Close()
		t.Fatal("compacted database opened with a mismatched changelog")
	}
	if !errors.Is(err, hexxladb.ErrChangelogConsumerInvalidated) {
		t.Fatalf("mismatched compacted sidecar: want ErrChangelogConsumerInvalidated, got %v", err)
	}

	admin, err := hexxladb.Open(badPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	consumers, err := admin.ListChangelogConsumers()
	if err != nil {
		t.Fatal(err)
	}
	if len(consumers) != 1 || consumers[0].ConsumerID != "compacted-reader" {
		t.Fatalf("administrative cursor list: %#v", consumers)
	}
	if err := admin.DeleteChangelogConsumer("compacted-reader", records[0].Seq); err != nil {
		t.Fatal(err)
	}
	if err := admin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(badPath + "-changelog"); err != nil {
		t.Fatal(err)
	}
	rebootstrapped := openConsumerDB(t, badPath)
	t.Cleanup(func() { _ = rebootstrapped.Close() })
	if err := rebootstrapped.AdvanceChangelogConsumer("compacted-reader", 0, 0); err != nil {
		t.Fatal(err)
	}
}

func copyTestFile(t *testing.T, source, destination string) {
	t.Helper()
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
