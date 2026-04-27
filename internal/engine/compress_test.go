package engine

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func openCompressDB(t *testing.T, opts *Options) *Engine {
	t.Helper()
	dir := t.TempDir()
	eng, err := Open(filepath.Join(dir, "compress.db"), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

func TestCompress_CompressDecompressRoundTrip(t *testing.T) {
	t.Parallel()
	val := []byte(strings.Repeat("hello world, this is a test of compression. ", 20))
	compressed := compressValue(val)
	if bytes.Equal(compressed, val) {
		t.Fatal("expected compressed output to differ from input")
	}
	if len(compressed) >= len(val) {
		t.Fatalf("expected compressed to be smaller: %d >= %d", len(compressed), len(val))
	}
	if !isCompressedValue(compressed) {
		t.Fatal("isCompressedValue returned false for compressed data")
	}
	decompressed, err := decompressValue(compressed)
	if err != nil {
		t.Fatalf("decompressValue: %v", err)
	}
	if !bytes.Equal(decompressed, val) {
		t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(decompressed), len(val))
	}
}

func TestCompress_SkipShortValues(t *testing.T) {
	t.Parallel()
	short := []byte("tiny")
	got := compressValue(short)
	if !bytes.Equal(got, short) {
		t.Fatal("short values should not be compressed")
	}
}

func TestCompress_NotCompressedValue(t *testing.T) {
	t.Parallel()
	if isCompressedValue([]byte("short")) {
		t.Fatal("plain value should not be identified as compressed")
	}
	if isCompressedValue(nil) {
		t.Fatal("nil should not be identified as compressed")
	}
	if isCompressedValue([]byte{}) {
		t.Fatal("empty should not be identified as compressed")
	}
}

func TestCompress_BTreePutGetRoundTrip(t *testing.T) {
	t.Parallel()
	eng := openCompressDB(t, nil)
	bt := &BTree{eng: eng}

	key := []byte("compress-key")
	val := []byte(strings.Repeat("ABCDEFGHIJ", 100))

	if err := bt.Put(key, val); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok, err := bt.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("Get: not found")
	}
	if !bytes.Equal(got, val) {
		t.Fatalf("Get: mismatch; got %d bytes, want %d", len(got), len(val))
	}
}

func TestCompress_MixedLongAndShort(t *testing.T) {
	t.Parallel()
	// Write both long (compressed) and short (raw) values; read both back.
	eng := openCompressDB(t, nil)
	bt := &BTree{eng: eng}

	longKey := []byte("long")
	longVal := []byte(strings.Repeat("COMPRESSED DATA ", 50))
	shortKey := []byte("short")
	shortVal := []byte("tiny")

	if err := bt.Put(longKey, longVal); err != nil {
		t.Fatal(err)
	}
	if err := bt.Put(shortKey, shortVal); err != nil {
		t.Fatal(err)
	}

	gotLong, ok, err := bt.Get(longKey)
	if err != nil || !ok {
		t.Fatalf("Get(long): ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(gotLong, longVal) {
		t.Fatalf("long value mismatch: got %d bytes want %d", len(gotLong), len(longVal))
	}
	gotShort, ok, err := bt.Get(shortKey)
	if err != nil || !ok {
		t.Fatalf("Get(short): ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(gotShort, shortVal) {
		t.Fatal("short value mismatch")
	}
}

func TestCompress_AscendRange(t *testing.T) {
	t.Parallel()
	eng := openCompressDB(t, nil)
	bt := &BTree{eng: eng}

	key1 := []byte("aaa")
	val1 := []byte(strings.Repeat("value1 ", 50))
	key2 := []byte("bbb")
	val2 := []byte(strings.Repeat("value2 ", 50))

	if err := bt.Put(key1, val1); err != nil {
		t.Fatal(err)
	}
	if err := bt.Put(key2, val2); err != nil {
		t.Fatal(err)
	}

	var count int
	err := bt.AscendRange(nil, nil, func(k, v []byte) bool {
		count++
		if bytes.Equal(k, key1) && !bytes.Equal(v, val1) {
			t.Error("val1 mismatch in AscendRange")
		}
		if bytes.Equal(k, key2) && !bytes.Equal(v, val2) {
			t.Error("val2 mismatch in AscendRange")
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("AscendRange: got %d entries want 2", count)
	}
}

func TestCompress_OverflowWithCompression(t *testing.T) {
	t.Parallel()
	eng := openCompressDB(t, &Options{
		MaxValueBytes: 65536,
	})
	bt := &BTree{eng: eng}

	key := []byte("big-compress")
	// Highly compressible: should compress well below inline threshold
	// even though the raw value exceeds it.
	threshold := inlineThreshold(eng.pageSize)
	val := []byte(strings.Repeat("X", threshold+500))

	if err := bt.Put(key, val); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok, err := bt.Get(key)
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(got, val) {
		t.Fatalf("mismatch: got %d bytes want %d", len(got), len(val))
	}
}

func TestCompress_IncompressibleOverflow(t *testing.T) {
	t.Parallel()
	eng := openCompressDB(t, &Options{
		MaxValueBytes: 65536,
	})
	bt := &BTree{eng: eng}

	key := []byte("random-key")
	// Random-ish data that won't compress well.
	threshold := inlineThreshold(eng.pageSize)
	val := make([]byte, threshold+500)
	for i := range val {
		val[i] = byte(i*7 + i*i) // pseudo-random pattern
	}

	if err := bt.Put(key, val); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok, err := bt.Get(key)
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(got, val) {
		t.Fatalf("mismatch: got %d bytes want %d", len(got), len(val))
	}
}

func TestCompress_ReopenReadBack(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "reopen.db")

	eng1, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	bt1 := &BTree{eng: eng1}
	key := []byte("persist")
	val := []byte(strings.Repeat("PERSIST ", 50))
	if err := bt1.Put(key, val); err != nil {
		t.Fatal(err)
	}
	_ = eng1.Close()

	// Reopen and verify data survives.
	eng2, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = eng2.Close() }()
	bt2 := &BTree{eng: eng2}
	got, ok, err := bt2.Get(key)
	if err != nil || !ok {
		t.Fatalf("Get after reopen: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(got, val) {
		t.Fatalf("value mismatch after reopen: got %d want %d", len(got), len(val))
	}
}

func TestCompress_DeleteCompressed(t *testing.T) {
	t.Parallel()
	eng := openCompressDB(t, nil)
	bt := &BTree{eng: eng}

	key := []byte("del-me")
	val := []byte(strings.Repeat("delete this value ", 30))

	if err := bt.Put(key, val); err != nil {
		t.Fatal(err)
	}
	if err := bt.Delete(key); err != nil {
		t.Fatal(err)
	}
	_, ok, err := bt.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("key still found after delete")
	}
}
