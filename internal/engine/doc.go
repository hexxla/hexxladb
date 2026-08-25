// Package engine implements the embedded storage engine: configurable fixed-size pages,
// primary file, redo WAL and replay, write transactions, and an on-disk B+ tree. See
// ENGINE_FORMAT.md and ORDERED_STORE.md.
//
// It must not import adapters; callers are typically package hexxladb and cmd.
package engine
