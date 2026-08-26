// Package engine provides the B+ tree storage engine for HexxlaDB.
// This file defines types for cascading split operations.
package engine

// cascadeSplitResult represents the result of a cascade leaf split.
// When entry sizes are highly variable, a single split may produce 2+ pages.
//
// Hexagonal Architecture: This is a pure domain type with no I/O dependencies.
// It captures the structural result of a split operation for the caller to execute.
type cascadeSplitResult struct {
	// pages contains all pages created from the split, ordered left-to-right.
	// pages[0] is the leftmost (original page), pages[1:] are new pages.
	pages []cascadePage

	// sepKeys contains the separator keys between pages.
	// len(sepKeys) == len(pages) - 1
	// sepKeys[i] is the first key of pages[i+1]
	sepKeys [][]byte
}

// cascadePage represents one page from a cascade split.
type cascadePage struct {
	keys   [][]byte // Keys for this page
	vals   [][]byte // Values for this page
	isNew  bool     // true if this page needs a new ID allocated
	pageID uint64   // pageID is set if !isNew (original page)
}
