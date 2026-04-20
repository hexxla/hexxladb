package hexxladb_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// WAL layout must match [internal/engine/wal.go] encodeWALRecord (seq, page_id, crc32, payload).
func encodeTestWALRecord(seq, pageID uint64, payload []byte) []byte {
	if len(payload) != engine.PageSize {
		panic("bad page size")
	}
	const overhead = 8 + 8 + 4
	out := make([]byte, overhead+engine.PageSize)
	binary.BigEndian.PutUint64(out[0:8], seq)
	binary.BigEndian.PutUint64(out[8:16], pageID)
	binary.BigEndian.PutUint32(out[16:20], crc32.ChecksumIEEE(payload))
	copy(out[20:], payload)
	return out
}

func walPathForDB(primary string) string {
	return primary + "-wal"
}

func TestDB_committedKVSurvivesReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "dur.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.Put([]byte("k1"), []byte("v1")); err != nil {
			return err
		}
		return tx.Put([]byte("k2"), []byte("v2"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	err = db2.View(func(tx *hexxladb.Tx) error {
		v, ok, err := tx.Get([]byte("k1"))
		if err != nil || !ok || string(v) != "v1" {
			t.Fatalf("k1: ok=%v v=%q err=%v", ok, v, err)
		}
		v, ok, err = tx.Get([]byte("k2"))
		if err != nil || !ok || string(v) != "v2" {
			t.Fatalf("k2: ok=%v v=%q err=%v", ok, v, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDB_committedCellsSurviveReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cells.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := lattice.Coord{Q: 0, R: 1}
	p, err := lattice.Pack(c)
	if err != nil {
		t.Fatal(err)
	}
	rec := record.CellRecord{Key: p, RawContent: "blob"}
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(context.Background(), rec)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	err = db2.View(func(tx *hexxladb.Tx) error {
		got, ok, err := tx.GetCell(p)
		if err != nil || !ok || got.RawContent != "blob" {
			t.Fatalf("cell: ok=%v content=%q err=%v", ok, got.RawContent, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDB_openReplaysPendingWAL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wal_replay.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	payload := bytes.Repeat([]byte{0x99}, engine.PageSize)
	rec := encodeTestWALRecord(1, 1, payload)
	wal := walPathForDB(path)
	if err := os.WriteFile(wal, rec, 0o600); err != nil {
		t.Fatal(err)
	}

	db2, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	// Public API: after replay the database must accept new writes (no corruption).
	err = db2.Update(func(tx *hexxladb.Tx) error {
		return tx.Put([]byte("after_wal"), []byte("ok"))
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db2.View(func(tx *hexxladb.Tx) error {
		v, ok, err := tx.Get([]byte("after_wal"))
		if err != nil || !ok || string(v) != "ok" {
			t.Fatalf("Get after_wal: ok=%v v=%q err=%v", ok, v, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
