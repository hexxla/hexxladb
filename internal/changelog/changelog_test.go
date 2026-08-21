package changelog_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb/internal/changelog"
)

func TestLog_appendRead_roundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	key := []byte("cell/\x00\x00\x00\x00\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x02")
	enc := []byte("hello-world-payload")
	if err := log.Append(42, changelog.OpPutCell, key, enc); err != nil {
		t.Fatal(err)
	}
	recs, err := log.ReadSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("len=%d", len(recs))
	}
	r := recs[0]
	if r.Seq != 1 || r.Op != changelog.OpPutCell {
		t.Fatalf("seq=%d op=%d", r.Seq, r.Op)
	}
	if !bytes.Equal(r.Key, key) {
		t.Fatalf("key mismatch")
	}
	if !r.HashValid {
		t.Fatal("hash")
	}
	if !bytes.Equal(r.Inline, enc) {
		t.Fatalf("inline %q", r.Inline)
	}
	more, err := log.ReadSince(1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(more) != 0 {
		t.Fatalf("expected empty after seq 1, got %d", len(more))
	}
}

func TestLog_appendBatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl2")
	log, err := changelog.Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	entries := []changelog.Entry{
		{Op: changelog.OpPutCell, Key: []byte("a"), Encoded: []byte("x")},
		{Op: changelog.OpPutEdge, Key: []byte("b"), Encoded: []byte("y")},
	}
	if err := log.AppendBatch(100, entries); err != nil {
		t.Fatal(err)
	}
	if log.MaxSeq() != 2 {
		t.Fatalf("maxSeq=%d", log.MaxSeq())
	}
	all, err := log.ReadSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("len=%d", len(all))
	}
}

func TestLog_largePayload_hashOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl3")
	log, err := changelog.Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	large := make([]byte, changelog.MaxInlinePayload+1)
	for i := range large {
		large[i] = byte(i % 251)
	}
	if err := log.Append(1, changelog.OpPutCell, []byte("k"), large); err != nil {
		t.Fatal(err)
	}
	recs, err := log.ReadSince(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatal(len(recs))
	}
	if len(recs[0].Inline) != 0 {
		t.Fatal("expected inline omitted")
	}
	if recs[0].EncodedLen != uint32(len(large)) {
		t.Fatalf("encodedLen=%d", recs[0].EncodedLen)
	}
}

func TestOpen_recoversMaxSeq(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cl4")
	log1, err := changelog.Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = log1.Append(1, changelog.OpPutCell, []byte("a"), nil)
	_ = log1.Close()

	log2, err := changelog.Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log2.Close() })
	if log2.MaxSeq() != 1 {
		t.Fatalf("maxSeq=%d", log2.MaxSeq())
	}
	if err := log2.Append(2, changelog.OpPutCell, []byte("b"), nil); err != nil {
		t.Fatal(err)
	}
	if log2.MaxSeq() != 2 {
		t.Fatalf("maxSeq=%d", log2.MaxSeq())
	}
}

func TestCorrupt_badMagic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad")
	if err := os.WriteFile(path, make([]byte, 16), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := changelog.Open(path, true)
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, changelog.ErrCorrupt) {
		t.Fatalf("got %v", err)
	}
}

func BenchmarkLog_ReadSinceTail(b *testing.B) {
	for _, history := range []int{512, 2_000, 10_000, 100_000} {
		path := filepath.Join(b.TempDir(), fmt.Sprintf("history-%d", history))
		log, err := changelog.Open(path, false)
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { _ = log.Close() })

		const appendBatchSize = 4_096
		for start := 0; start < history; start += appendBatchSize {
			end := min(start+appendBatchSize, history)
			entries := make([]changelog.Entry, end-start)
			for i := range entries {
				entries[i] = changelog.Entry{
					Op:      changelog.OpPutCell,
					Key:     fmt.Appendf(nil, "cell/%016x", start+i),
					Encoded: []byte("value"),
				}
			}
			if err := log.AppendBatch(1, entries); err != nil {
				b.Fatal(err)
			}
		}

		for _, limit := range []int{1, 256} {
			b.Run(fmt.Sprintf("history_%d/limit_%d", history, limit), func(b *testing.B) {
				afterSeq := uint64(history - limit)
				b.ReportMetric(float64(history), "history-records")
				b.ReportMetric(float64(limit), "records/op")
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					records, err := log.ReadSince(afterSeq, limit)
					if err != nil {
						b.Fatal(err)
					}
					if len(records) != limit {
						b.Fatalf("ReadSince returned %d records, want %d", len(records), limit)
					}
				}
			})
		}
	}
}
