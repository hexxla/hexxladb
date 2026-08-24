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

func TestIntent_roundTripAndBoundedInlinePolicy(t *testing.T) {
	t.Parallel()
	entry := changelog.Entry{
		Op:      changelog.OpPutCell,
		Key:     []byte("cell/private"),
		Encoded: bytes.Repeat([]byte("x"), 500),
	}
	intent, err := changelog.PrepareIntent(1234, entry, 512)
	if err != nil {
		t.Fatal(err)
	}
	if len(intent.Inline) != 0 || !intent.HashValid || intent.EncodedLen != 500 {
		t.Fatalf("bounded intent metadata: %#v", intent)
	}
	value, err := changelog.EncodeIntentValue(intent)
	if err != nil {
		t.Fatal(err)
	}
	if len(value) > 512 {
		t.Fatalf("intent value length %d exceeds primary bound", len(value))
	}
	decoded, err := changelog.DecodeIntentValue(entry.Key, value)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.WallUnixNs != intent.WallUnixNs || decoded.Op != intent.Op ||
		!bytes.Equal(decoded.Key, entry.Key) || decoded.Hash != intent.Hash ||
		decoded.EncodedLen != intent.EncodedLen || len(decoded.Inline) != 0 {
		t.Fatalf("intent round-trip mismatch: before=%#v after=%#v", intent, decoded)
	}
}

func TestLog_appendIntentsPreservesPreparedMetadata(t *testing.T) {
	t.Parallel()
	log, err := changelog.Open(filepath.Join(t.TempDir(), "intent-log"), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	entry := changelog.Entry{Op: changelog.OpPutCell, Key: []byte("cell/key"), Encoded: []byte("payload")}
	intent, err := changelog.PrepareIntent(9876, entry, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.AppendIntents([]changelog.Intent{intent}); err != nil {
		t.Fatal(err)
	}
	records, err := log.ReadSince(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].WallUnixNs != 9876 || !bytes.Equal(records[0].Inline, entry.Encoded) ||
		records[0].Hash != intent.Hash {
		t.Fatalf("projected intent mismatch: %#v", records)
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

func TestLog_encryptedRoundTripReopenAndCiphertext(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "encrypted")
	key := bytes.Repeat([]byte{0x42}, 32)
	secretKey := []byte("cell/private-coordinate")
	secretPayload := []byte("private-inline-payload")

	log, err := changelog.OpenEncrypted(path, true, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.AppendBatch(100, []changelog.Entry{
		{Op: changelog.OpPutCell, Key: secretKey, Encoded: secretPayload},
		{Op: changelog.OpPutEdge, Key: []byte("edge/private"), Encoded: []byte("edge-payload")},
	}); err != nil {
		t.Fatal(err)
	}
	records, err := log.ReadSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || !bytes.Equal(records[0].Key, secretKey) || !bytes.Equal(records[0].Inline, secretPayload) {
		t.Fatalf("encrypted round-trip mismatch: %#v", records)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte("HXCHGv02")) {
		t.Fatalf("encrypted header magic = %q", raw[:8])
	}
	if bytes.Contains(raw, secretKey) || bytes.Contains(raw, secretPayload) {
		t.Fatal("encrypted changelog exposed logical key or inline payload")
	}

	log, err = changelog.OpenEncrypted(path, true, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	if log.MaxSeq() != 2 {
		t.Fatalf("MaxSeq after reopen = %d, want 2", log.MaxSeq())
	}
	if err := log.Append(101, changelog.OpDeleteCell, []byte("cell/deleted"), nil); err != nil {
		t.Fatal(err)
	}
	tail, err := log.ReadSince(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 1 || tail[0].Seq != 3 || tail[0].Op != changelog.OpDeleteCell {
		t.Fatalf("encrypted tail mismatch: %#v", tail)
	}
}

func TestLog_encryptedModeAndKeyFailures(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	encryptedPath := filepath.Join(dir, "encrypted")
	key := bytes.Repeat([]byte{0x11}, 32)
	log, err := changelog.OpenEncrypted(encryptedPath, true, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := changelog.Open(encryptedPath, true); !errors.Is(err, changelog.ErrEncryptionRequired) {
		t.Fatalf("plaintext open encrypted log: want ErrEncryptionRequired, got %v", err)
	}
	wrongKey := bytes.Repeat([]byte{0x22}, 32)
	if _, err := changelog.OpenEncrypted(encryptedPath, true, wrongKey); !errors.Is(err, changelog.ErrEncryptionKeyMismatch) {
		t.Fatalf("wrong encrypted key: want ErrEncryptionKeyMismatch, got %v", err)
	}

	plaintextPath := filepath.Join(dir, "plaintext")
	plain, err := changelog.Open(plaintextPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := changelog.OpenEncrypted(plaintextPath, true, key); !errors.Is(err, changelog.ErrPlaintext) {
		t.Fatalf("encrypted open plaintext log: want ErrPlaintext, got %v", err)
	}
}

func TestLog_encryptedTamperFailsClosed(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		offset int
	}{
		{name: "header", offset: 16},
		{name: "clear sequence", offset: encryptedHeaderSizeForTest + 4},
		{name: "ciphertext", offset: encryptedHeaderSizeForTest + 4 + 8},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tamper")
			key := bytes.Repeat([]byte{0x33}, 32)
			log, err := changelog.OpenEncrypted(path, true, key)
			if err != nil {
				t.Fatal(err)
			}
			if err := log.Append(1, changelog.OpPutCell, []byte("cell/key"), []byte("secret")); err != nil {
				t.Fatal(err)
			}
			if err := log.Close(); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if test.offset >= len(raw) {
				t.Fatalf("tamper offset %d beyond file size %d", test.offset, len(raw))
			}
			raw[test.offset] ^= 0x80
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = changelog.OpenEncrypted(path, true, key)
			if test.name == "header" {
				if !errors.Is(err, changelog.ErrEncryptionKeyMismatch) {
					t.Fatalf("tampered header: want ErrEncryptionKeyMismatch, got %v", err)
				}
			} else if !errors.Is(err, changelog.ErrCorrupt) {
				t.Fatalf("tampered frame: want ErrCorrupt, got %v", err)
			}
		})
	}
}

func TestLog_encryptedTruncationFailsClosed(t *testing.T) {
	t.Parallel()
	for _, size := range []int64{20, encryptedHeaderSizeForTest + 4 + 8 + 1} {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "truncated")
			key := bytes.Repeat([]byte{0x66}, 32)
			log, err := changelog.OpenEncrypted(path, true, key)
			if err != nil {
				t.Fatal(err)
			}
			if err := log.Append(1, changelog.OpPutCell, []byte("cell/key"), []byte("secret")); err != nil {
				t.Fatal(err)
			}
			if err := log.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.Truncate(path, size); err != nil {
				t.Fatal(err)
			}
			if _, err := changelog.OpenEncrypted(path, true, key); !errors.Is(err, changelog.ErrCorrupt) {
				t.Fatalf("truncated encrypted changelog: want ErrCorrupt, got %v", err)
			}
		})
	}
}

func TestOpenRecoverable_truncatesOnlyIncompleteFinalFrame(t *testing.T) {
	t.Parallel()
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprintf("encrypted_%v", encrypted), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "recoverable-tail")
			key := bytes.Repeat([]byte{0x71}, 32)
			var log *changelog.Log
			var err error
			if encrypted {
				log, err = changelog.OpenEncrypted(path, true, key)
			} else {
				log, err = changelog.Open(path, true)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := log.Append(1, changelog.OpPutCell, []byte("complete"), []byte("value")); err != nil {
				t.Fatal(err)
			}
			if err := log.Close(); err != nil {
				t.Fatal(err)
			}
			complete, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Write([]byte{0, 0, 0, 100, 1, 2, 3}); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}

			if encrypted {
				log, err = changelog.OpenEncryptedRecoverable(path, true, key)
			} else {
				log, err = changelog.OpenRecoverable(path, true)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = log.Close() }()
			if log.MaxSeq() != 1 {
				t.Fatalf("MaxSeq=%d, want 1", log.MaxSeq())
			}
			repaired, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if repaired.Size() != complete.Size() {
				t.Fatalf("repaired size=%d, want %d", repaired.Size(), complete.Size())
			}
		})
	}
}

func TestLog_CopyToReencryptsAndPreservesHashOnlyRecord(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source, err := changelog.OpenEncrypted(
		filepath.Join(dir, "source"),
		true,
		bytes.Repeat([]byte{0x44}, 32),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	destinationPath := filepath.Join(dir, "destination")
	destination, err := changelog.OpenEncrypted(
		destinationPath,
		true,
		bytes.Repeat([]byte{0x55}, 32),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = destination.Close() })

	payload := bytes.Repeat([]byte("private"), changelog.MaxInlinePayload)
	if err := source.Append(9876, changelog.OpPutCell, []byte("cell/private"), payload); err != nil {
		t.Fatal(err)
	}
	before, err := source.ReadSince(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.CopyTo(destination); err != nil {
		t.Fatal(err)
	}
	after, err := destination.ReadSince(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || len(after) != 1 {
		t.Fatalf("copy counts before=%d after=%d", len(before), len(after))
	}
	if before[0].Seq != after[0].Seq || before[0].WallUnixNs != after[0].WallUnixNs ||
		before[0].Hash != after[0].Hash || before[0].EncodedLen != after[0].EncodedLen || len(after[0].Inline) != 0 {
		t.Fatalf("copied hash-only record mismatch: before=%#v after=%#v", before[0], after[0])
	}
	raw, err := os.ReadFile(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("cell/private")) || bytes.Contains(raw, []byte("private")) {
		t.Fatal("re-encrypted copy exposed logical record bytes")
	}
}

func TestLog_logicalDigestSurvivesReopenAndReencryption(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source")
	source, err := changelog.Open(sourcePath, true)
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]changelog.Entry, 300)
	for i := range entries {
		entries[i] = changelog.Entry{
			Op:      changelog.OpPutCell,
			Key:     []byte(fmt.Sprintf("cell/%03d", i)),
			Encoded: []byte(fmt.Sprintf("value-%03d", i)),
		}
	}
	if err := source.AppendBatch(1234, entries); err != nil {
		t.Fatal(err)
	}
	wantHead, wantHeadDigest, err := source.LogicalCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	wantMidDigest, err := source.LogicalDigestAt(127)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.LogicalDigestAt(wantHead + 1); !errors.Is(err, changelog.ErrCorrupt) {
		t.Fatalf("digest beyond head: want ErrCorrupt, got %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}

	source, err = changelog.Open(sourcePath, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	gotHead, gotHeadDigest, err := source.LogicalCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	gotMidDigest, err := source.LogicalDigestAt(127)
	if err != nil {
		t.Fatal(err)
	}
	if gotHead != wantHead || gotHeadDigest != wantHeadDigest || gotMidDigest != wantMidDigest {
		t.Fatalf("logical digest changed after reopen: head=%d want=%d", gotHead, wantHead)
	}

	destination, err := changelog.OpenEncrypted(
		filepath.Join(dir, "destination"),
		true,
		bytes.Repeat([]byte{0x67}, 32),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = destination.Close() })
	if err := source.CopyTo(destination); err != nil {
		t.Fatal(err)
	}
	copiedHead, copiedDigest, err := destination.LogicalCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if copiedHead != wantHead || copiedDigest != wantHeadDigest {
		t.Fatalf("logical digest changed after reencryption: head=%d want=%d", copiedHead, wantHead)
	}
}

const encryptedHeaderSizeForTest = 48

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
