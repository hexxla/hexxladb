package changelog

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLog_CheckpointsBuildAppendAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoints")
	log, err := Open(path, false)
	if err != nil {
		t.Fatal(err)
	}

	appendEntries := func(count int) {
		t.Helper()
		entries := make([]Entry, count)
		for i := range entries {
			entries[i] = Entry{Op: OpPutCell, Key: []byte("cell"), Encoded: []byte("value")}
		}
		if err := log.AppendBatch(1, entries); err != nil {
			t.Fatal(err)
		}
	}
	appendEntries(600)
	assertCheckpointSeqs(t, log, []uint64{0, 256, 512})
	assertReadSinceRange(t, log, 255, 4, 256)
	assertReadSinceRange(t, log, 256, 4, 257)
	assertReadSinceRange(t, log, 511, 4, 512)
	assertReadSinceRange(t, log, 512, 4, 513)

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	log, err = Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	assertCheckpointSeqs(t, log, []uint64{0, 256, 512})

	appendEntries(200)
	assertCheckpointSeqs(t, log, []uint64{0, 256, 512, 768})
	assertReadSinceRange(t, log, 767, 4, 768)
	assertReadSinceRange(t, log, 768, 4, 769)

	for range 224 {
		if err := log.Append(1, OpPutCell, []byte("cell"), []byte("value")); err != nil {
			t.Fatal(err)
		}
	}
	assertCheckpointSeqs(t, log, []uint64{0, 256, 512, 768, 1024})
	assertReadSinceRange(t, log, 1023, 1, 1024)
}

func TestOpenRejectsNonContiguousSequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sequence-gap")
	log, err := Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	body, err := encodeInner(2, 1, OpPutCell, []byte("cell"), []byte("value"))
	if err != nil {
		t.Fatal(err)
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(body)))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(length[:]); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(path, false)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Open with sequence gap: want ErrCorrupt, got %v", err)
	}
}

func assertCheckpointSeqs(t *testing.T, log *Log, want []uint64) {
	t.Helper()
	if len(log.checkpoints) != len(want) {
		t.Fatalf("checkpoint count=%d, want %d", len(log.checkpoints), len(want))
	}
	previousOffset := int64(0)
	for i, checkpoint := range log.checkpoints {
		if checkpoint.seq != want[i] {
			t.Fatalf("checkpoint %d sequence=%d, want %d", i, checkpoint.seq, want[i])
		}
		if checkpoint.offset <= previousOffset {
			t.Fatalf("checkpoint %d offset=%d, previous=%d", i, checkpoint.offset, previousOffset)
		}
		previousOffset = checkpoint.offset
	}
}

func assertReadSinceRange(t *testing.T, log *Log, afterSeq uint64, limit int, firstSeq uint64) {
	t.Helper()
	records, err := log.ReadSince(afterSeq, limit)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != limit {
		t.Fatalf("ReadSince(%d, %d) returned %d records", afterSeq, limit, len(records))
	}
	for i, record := range records {
		want := firstSeq + uint64(i)
		if record.Seq != want {
			t.Fatalf("ReadSince(%d, %d) record %d sequence=%d, want %d", afterSeq, limit, i, record.Seq, want)
		}
	}
}
