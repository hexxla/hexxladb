package changelog_test

// mutation_test.go — tests targeting gremlins survivors in internal/changelog.
// Focus: binary protocol boundaries, offset arithmetic, conditional branches.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexxla/hexxladb/internal/changelog"
)

// ── Close/nil boundary (line 170) ────────────────────────────────────────────

// Kills CONDITIONALS_NEGATION at changelog.go:170 (l == nil || l.f == nil → mutants).
func TestLog_Close_nil(t *testing.T) {
	t.Parallel()
	var l *changelog.Log
	if err := l.Close(); err != nil {
		t.Fatalf("Close(nil) should not error: %v", err)
	}
}

func TestLog_Close_idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	// Second close should be safe (l.f == nil path).
	if err := log.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// ── MaxSeq/Path nil guard ────────────────────────────────────────────────────

func TestLog_MaxSeq_nil(t *testing.T) {
	t.Parallel()
	var l *changelog.Log
	if l.MaxSeq() != 0 {
		t.Fatal("MaxSeq on nil should be 0")
	}
}

func TestLog_Path_nil(t *testing.T) {
	t.Parallel()
	var l *changelog.Log
	if l.Path() != "" {
		t.Fatal("Path on nil should be empty")
	}
}

// ── Append boundary: key length limit (line 199) ─────────────────────────────

// Kills CONDITIONALS_BOUNDARY at changelog.go:199 (len(key) > 65535 → >=).
func TestLog_Append_keyAtMaxLength(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	// Key of exactly 65535 bytes — must succeed.
	maxKey := []byte(strings.Repeat("k", 65535))
	if err := log.Append(1, changelog.OpPutCell, maxKey, []byte("v")); err != nil {
		t.Fatalf("Append with 65535 key should succeed: %v", err)
	}
	// Key of 65536 bytes — must fail.
	overKey := []byte(strings.Repeat("k", 65536))
	if err := log.Append(2, changelog.OpPutCell, overKey, []byte("v")); err == nil {
		t.Fatal("Append with 65536 key should fail")
	}
}

// ── AppendBatch boundary: key length (line 244) ──────────────────────────────

// Kills CONDITIONALS_BOUNDARY at changelog.go:244.
func TestLog_AppendBatch_keyAtMaxLength(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	maxKey := []byte(strings.Repeat("k", 65535))
	entries := []changelog.Entry{
		{Op: changelog.OpPutCell, Key: maxKey, Encoded: []byte("v")},
	}
	if err := log.AppendBatch(1, entries); err != nil {
		t.Fatalf("AppendBatch with 65535 key should succeed: %v", err)
	}

	overKey := []byte(strings.Repeat("k", 65536))
	entries2 := []changelog.Entry{
		{Op: changelog.OpPutCell, Key: overKey, Encoded: []byte("v")},
	}
	if err := log.AppendBatch(2, entries2); err == nil {
		t.Fatal("AppendBatch with 65536 key should fail")
	}
}

// ── ReadSince boundary: limit (line 282) ─────────────────────────────────────

// Kills CONDITIONALS_BOUNDARY at changelog.go:282 (limit <= 0 → < 0).
func TestLog_ReadSince_limitZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	_ = log.Append(1, changelog.OpPutCell, []byte("k"), []byte("v"))

	// limit=0 should return nil/empty.
	recs, err := log.ReadSince(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("ReadSince(0,0) should return empty, got %d", len(recs))
	}
	// limit=1 should return the one record.
	recs, err = log.ReadSince(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("ReadSince(0,1) should return 1, got %d", len(recs))
	}
}

// Kills CONDITIONALS_BOUNDARY at changelog.go:291 (off < st.Size() && len(out) < limit).
func TestLog_ReadSince_exactLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	_ = log.Append(1, changelog.OpPutCell, []byte("a"), []byte("x"))
	_ = log.Append(2, changelog.OpPutCell, []byte("b"), []byte("y"))
	_ = log.Append(3, changelog.OpPutCell, []byte("c"), []byte("z"))

	// Limit exactly matches record count.
	recs, err := log.ReadSince(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("ReadSince(0,3) got %d", len(recs))
	}
	// Limit less than record count.
	recs, err = log.ReadSince(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("ReadSince(0,2) got %d", len(recs))
	}
}

// Kills CONDITIONALS_BOUNDARY at changelog.go:297 (n < 28 || off+4+n > st.Size()).
func TestLog_ReadSince_corruptFrameLen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = log.Append(1, changelog.OpPutCell, []byte("k"), []byte("v"))
	_ = log.Close()

	// Corrupt the first frame's length field to be too small (< 28).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Frame starts at offset 16 (headerSize). First 4 bytes are length.
	if len(raw) < 20 {
		t.Fatal("file too short")
	}
	corrupted := append([]byte(nil), raw...)
	corrupted[16] = 0
	corrupted[17] = 0
	corrupted[18] = 0
	corrupted[19] = 5 // frame len = 5, which is < 28
	if err := os.WriteFile(path, corrupted, 0o600); err != nil {
		t.Fatal(err)
	}
	log2, err := changelog.Open(path, false)
	if err == nil {
		_ = log2.Close()
		t.Fatal("Open should fail with corrupt frame length")
	}
	if !errors.Is(err, changelog.ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}

// ── encodeInner branches: empty vs non-empty payload (lines 319-349) ─────────

// Kills CONDITIONALS_BOUNDARY at changelog.go:319 (len(encoded) > 0 → >=)
// and changelog.go:326, 328, 349 (inline/hash flag logic).
func TestLog_Append_emptyPayload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	// Empty encoded — no hash, no inline.
	if err := log.Append(1, changelog.OpDeleteCell, []byte("k"), nil); err != nil {
		t.Fatal(err)
	}
	recs, err := log.ReadSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatal("expected 1 record")
	}
	r := recs[0]
	if r.HashValid {
		t.Fatal("empty payload should have no hash")
	}
	if len(r.Inline) != 0 {
		t.Fatal("empty payload should have no inline")
	}
	if r.EncodedLen != 0 {
		t.Fatalf("EncodedLen=%d want 0", r.EncodedLen)
	}
}

func TestLog_Append_smallPayload_inlined(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	enc := []byte("small")
	if err := log.Append(1, changelog.OpPutCell, []byte("k"), enc); err != nil {
		t.Fatal(err)
	}
	recs, err := log.ReadSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	r := recs[0]
	if !r.HashValid {
		t.Fatal("non-empty payload should have hash")
	}
	if !bytes.Equal(r.Inline, enc) {
		t.Fatalf("inline %q want %q", r.Inline, enc)
	}
	if r.EncodedLen != uint32(len(enc)) {
		t.Fatalf("EncodedLen=%d want %d", r.EncodedLen, len(enc))
	}
}

// Payload exactly at MaxInlinePayload boundary — should be inlined.
func TestLog_Append_payloadAtMaxInlineBoundary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	// Exactly MaxInlinePayload bytes — should be inlined.
	atMax := bytes.Repeat([]byte{0xAB}, changelog.MaxInlinePayload)
	if err := log.Append(1, changelog.OpPutCell, []byte("k"), atMax); err != nil {
		t.Fatal(err)
	}
	recs, err := log.ReadSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	r := recs[0]
	if !bytes.Equal(r.Inline, atMax) {
		t.Fatal("payload at MaxInlinePayload should be inlined")
	}

	// One byte over MaxInlinePayload — should NOT be inlined.
	overMax := bytes.Repeat([]byte{0xCD}, changelog.MaxInlinePayload+1)
	if err := log.Append(2, changelog.OpPutCell, []byte("k2"), overMax); err != nil {
		t.Fatal(err)
	}
	recs, err = log.ReadSince(1, 10)
	if err != nil {
		t.Fatal(err)
	}
	r2 := recs[0]
	if len(r2.Inline) != 0 {
		t.Fatal("payload over MaxInlinePayload should NOT be inlined")
	}
	if !r2.HashValid {
		t.Fatal("large payload should still have hash")
	}
	if r2.EncodedLen != uint32(len(overMax)) {
		t.Fatalf("EncodedLen=%d want %d", r2.EncodedLen, len(overMax))
	}
}

// ── decodeInner CRC validation (lines 406-413) ──────────────────────────────

func TestLog_ReadSince_corruptCRC(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = log.Append(1, changelog.OpPutCell, []byte("k"), []byte("v"))
	_ = log.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Flip a byte in the body area (after header + frame length).
	if len(raw) < 25 {
		t.Fatal("file too short")
	}
	corrupted := append([]byte(nil), raw...)
	corrupted[24] ^= 0xFF // flip byte in frame body
	if err := os.WriteFile(path, corrupted, 0o600); err != nil {
		t.Fatal(err)
	}
	log2, err := changelog.Open(path, false)
	if err == nil {
		_ = log2.Close()
		t.Fatal("Open should fail with corrupt CRC")
	}
	if !errors.Is(err, changelog.ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}

// ── ReadSince afterSeq filtering (line 308) ──────────────────────────────────

// Tests that records with seq <= afterSeq are skipped.
func TestLog_ReadSince_afterSeqFiltering(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	for i := range 5 {
		_ = log.Append(int64(i), changelog.OpPutCell, []byte("k"), []byte("v"))
	}

	// afterSeq=3 should return seq 4 and 5.
	recs, err := log.ReadSince(3, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("ReadSince(3,100) got %d want 2", len(recs))
	}
	if recs[0].Seq != 4 || recs[1].Seq != 5 {
		t.Fatalf("seqs: %d, %d", recs[0].Seq, recs[1].Seq)
	}
}

// ── Reopen after multiple appends (scanMaxSeq offset arithmetic) ─────────────

func TestLog_reopenAfterMultipleAppends(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = log.Append(1, changelog.OpPutCell, []byte("k1"), []byte("v1"))
	_ = log.Append(2, changelog.OpPutSeam, []byte("k2"), nil)
	_ = log.Append(3, changelog.OpPutFacet, []byte("k3"), []byte("payload"))
	_ = log.Close()

	log2, err := changelog.Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log2.Close() })
	if log2.MaxSeq() != 3 {
		t.Fatalf("MaxSeq=%d want 3", log2.MaxSeq())
	}
	// Append continues from seq 4.
	_ = log2.Append(4, changelog.OpPutEdge, []byte("k4"), []byte("edge"))
	if log2.MaxSeq() != 4 {
		t.Fatalf("MaxSeq=%d want 4", log2.MaxSeq())
	}
}

// ── Append on closed log ─────────────────────────────────────────────────────

func TestLog_appendAfterClose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = log.Close()
	if err := log.Append(1, changelog.OpPutCell, []byte("k"), []byte("v")); err == nil {
		t.Fatal("Append on closed log should fail")
	}
}

func TestLog_readSinceAfterClose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cl")
	log, err := changelog.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = log.Close()
	_, err = log.ReadSince(0, 10)
	if err == nil {
		t.Fatal("ReadSince on closed log should fail")
	}
}
