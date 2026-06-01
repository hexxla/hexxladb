// Package engine provides the B+ tree storage engine for HexxlaDB.
// This file tests the cascading leaf insertion integration.
package engine

import (
	"path/filepath"
	"testing"
)

// createTestEngine creates a temporary engine for testing.
func createTestEngine(t *testing.T) (eng *Engine, cleanup func()) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	var err error
	eng, err = Open(path, &Options{PageSize: 4096, UseFormatV2: true})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	cleanup = func() {
		_ = eng.Close()
	}
	return eng, cleanup
}

// TestInsertIntoLeafCascade_NoSplit verifies no split when page fits.
func TestInsertIntoLeafCascade_NoSplit(t *testing.T) {
	eng, cleanup := createTestEngine(t)
	defer cleanup()
	bt := OpenBTree(eng)

	// Create initial leaf with 2 small entries
	keys := [][]byte{[]byte("key001"), []byte("key002")}
	vals := [][]byte{make([]byte, 100), make([]byte, 100)}

	// Write initial page
	page, err := buildLeafPage(4096, 0, 0, keys, vals)
	if err != nil {
		t.Fatalf("buildLeafPage: %v", err)
	}
	if err := eng.WritePage(1, page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	// Insert another small entry - should not split
	didSplit, result, err := bt.insertIntoLeafCascade(1, page, []byte("key003"), make([]byte, 100))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if didSplit {
		t.Fatal("expected no split for small entries")
	}
	if result != nil {
		t.Fatal("expected nil result for no split")
	}
}

// TestInsertIntoLeafCascade_TwoPages verifies two-page split.
func TestInsertIntoLeafCascade_TwoPages(t *testing.T) {
	eng, cleanup := createTestEngine(t)
	defer cleanup()
	bt := OpenBTree(eng)

	// Create leaf that will overflow after one more insert
	// 5 entries of 600 bytes each = 64 + 5*(4+600) = 3084 < 4096 (fits)
	// Add 6th entry: 64 + 6*(4+600) = 3688 < 4096 (still fits)
	// Add 7th entry: 64 + 7*(4+600) = 4292 > 4096 (overflow, triggers split)
	keys := make([][]byte, 6)
	vals := make([][]byte, 6)
	for i := range 6 {
		keys[i] = []byte("key00000000000000000")
		vals[i] = make([]byte, 600)
	}

	page, err := buildLeafPage(4096, 0, 0, keys, vals)
	if err != nil {
		t.Fatalf("buildLeafPage: %v", err)
	}
	if err := eng.WritePage(1, page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	// Insert 7th entry - should trigger two-page split
	didSplit, result, err := bt.insertIntoLeafCascade(1, page, []byte("key007"), make([]byte, 600))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !didSplit {
		t.Fatal("expected split for overflow")
	}
	if result == nil {
		t.Fatal("expected non-nil result after split")
	}

	// Should produce exactly 2 pages for uniform entries
	if result.pageCount() != 2 {
		t.Fatalf("expected 2 pages, got %d", result.pageCount())
	}
	if result.newPageCount() != 1 {
		t.Fatalf("expected 1 new page, got %d", result.newPageCount())
	}
}

// TestInsertIntoLeafCascade_CascadeSplit verifies a high-variance split.
//
// The fixture is a VALID near-full leaf (fits within pageSize) with unique,
// ordered keys of mixed value sizes. Inserting one more entry tips it over and
// must split. The critical invariant asserted here is that EVERY produced page
// serializes within pageSize — the property whose violation is ErrCorruptTree.
//
// NOTE: insertIntoLeafCascade only ever receives one on-disk leaf (<= pageSize)
// plus a single new entry, so a real split yields 2 pages; 3+ page cascades are
// exercised directly against cascadingLeafSplit in btree_cascade_split_test.go.
func TestInsertIntoLeafCascade_CascadeSplit(t *testing.T) {
	eng, cleanup := createTestEngine(t)
	defer cleanup()
	bt := OpenBTree(eng)

	const pageSize = 4096
	// 1 small + 2 large, unique ordered keys, fits: 64 + 124 + 2*1524 = 3236 < 4096.
	keys := [][]byte{
		[]byte("key-0-small000000000"),
		[]byte("key-1-large000000000"),
		[]byte("key-2-large000000000"),
	}
	vals := [][]byte{make([]byte, 100), make([]byte, 1500), make([]byte, 1500)}

	page, err := buildLeafPage(pageSize, 0, 0, keys, vals)
	if err != nil {
		t.Fatalf("fixture buildLeafPage: %v", err)
	}
	if err := eng.WritePage(1, page); err != nil {
		t.Fatal(err)
	}

	// Insert a 4th large entry to tip the page over and force a split.
	didSplit, result, err := bt.insertIntoLeafCascade(1, page, []byte("key-3-large000000000"), make([]byte, 1500))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !didSplit {
		t.Fatal("expected split for overflow with high variance")
	}
	if result == nil {
		t.Fatal("expected non-nil result after split")
	}
	if result.pageCount() < 2 {
		t.Fatalf("expected 2+ pages, got %d", result.pageCount())
	}

	// Every produced page must serialize within pageSize (no ErrCorruptTree).
	totalEntries := 0
	var lastKey []byte
	for pi, p := range result.pages {
		sz := btreeHeaderSize
		for j := range p.keys {
			sz += 4 + len(p.keys[j]) + len(p.vals[j])
			if lastKey != nil && string(lastKey) >= string(p.keys[j]) {
				t.Fatalf("key order violated at page %d: %q >= %q", pi, lastKey, p.keys[j])
			}
			lastKey = p.keys[j]
		}
		if sz > pageSize {
			t.Fatalf("page %d serializes to %d > pageSize %d", pi, sz, pageSize)
		}
		totalEntries += len(p.keys)
	}
	if totalEntries != 4 { // 3 original + 1 inserted
		t.Fatalf("expected 4 entries total, got %d", totalEntries)
	}
}

// TestInsertIntoLeafCascade_KeyOrder verifies key order preserved.
func TestInsertIntoLeafCascade_KeyOrder(t *testing.T) {
	eng, cleanup := createTestEngine(t)
	defer cleanup()
	bt := OpenBTree(eng)

	// Create leaf with keys that will be split
	keys := [][]byte{
		[]byte("aaa"),
		[]byte("bbb"),
		[]byte("ccc"),
		[]byte("ddd"),
		[]byte("eee"),
	}
	vals := make([][]byte, 5)
	for i := range vals {
		vals[i] = make([]byte, 700) // Large values to trigger split
	}

	page, err := buildLeafPage(4096, 0, 0, keys, vals)
	if err != nil {
		t.Fatalf("buildLeafPage: %v", err)
	}
	if err := eng.WritePage(1, page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	// Insert in middle
	didSplit, result, err := bt.insertIntoLeafCascade(1, page, []byte("bbf"), make([]byte, 700))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !didSplit {
		t.Fatal("expected split")
	}

	// Verify key order across all pages
	var lastKey []byte
	for _, p := range result.pages {
		for _, k := range p.keys {
			if lastKey != nil && string(lastKey) >= string(k) {
				t.Fatalf("key order violated: %s >= %s", lastKey, k)
			}
			lastKey = k
		}
	}
}

// TestInsertIntoLeafCascade_ReplaceExisting verifies value replacement.
func TestInsertIntoLeafCascade_ReplaceExisting(t *testing.T) {
	eng, cleanup := createTestEngine(t)
	defer cleanup()
	bt := OpenBTree(eng)

	keys := [][]byte{[]byte("key001"), []byte("key002")}
	vals := [][]byte{[]byte("old-value-001"), []byte("old-value-002")}

	page, err := buildLeafPage(4096, 0, 0, keys, vals)
	if err != nil {
		t.Fatalf("buildLeafPage: %v", err)
	}
	if err := eng.WritePage(1, page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	// Replace existing key
	didSplit, result, err := bt.insertIntoLeafCascade(1, page, []byte("key001"), []byte("new-value-001"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if didSplit {
		t.Fatal("expected no split for replacement")
	}
	if result != nil {
		t.Fatal("expected nil result when no split occurs")
	}

	// Read back and verify
	newPage, _ := eng.ReadPage(1)
	ld, _ := parseLeafPage(newPage)

	if string(ld.vals[0]) != "new-value-001" {
		t.Fatalf("expected new value, got %s", ld.vals[0])
	}
}
