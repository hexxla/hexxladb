// Package engine implements the embedded storage shell: fixed-size pages, primary file,
// append-only redo WAL, replay on open (see ENGINE_FORMAT.md), and an on-disk B+ tree
// (see ORDERED_STORE.md).
//
// It must not import adapters; callers are typically package hexxladb and cmd.
package engine
