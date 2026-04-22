package hexxladb_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestMVCC_existingV1_notUpgradedByFlag(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "v1.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	hdr, err := engine.ReadHeaderFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.FormatVersion != 1 {
		t.Fatalf("existing file should stay v1: got %d", hdr.FormatVersion)
	}
}

func TestMVCC_newFile_formatV2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "mvcc.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	hdr, err := engine.ReadHeaderFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.FormatVersion != 2 {
		t.Fatalf("format version: got %d want 2", hdr.FormatVersion)
	}
}

func TestMVCC_ViewAt_visibility(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "v.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	p := lattice.PackedCoord{1, 2}
	rec := record.CellRecord{Key: p, RawContent: "a"}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(context.Background(), rec) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		rec2 := record.CellRecord{Key: p, RawContent: "b"}
		return tx.PutCell(context.Background(), rec2)
	}); err != nil {
		t.Fatal(err)
	}

	err = db.ViewAt(1, func(tx *hexxladb.Tx) error {
		got, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if !ok || string(got.RawContent) != "a" {
			t.Fatalf("seq 1: ok=%v content=%q", ok, string(got.RawContent))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.ViewAt(2, func(tx *hexxladb.Tx) error {
		got, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if !ok || string(got.RawContent) != "b" {
			t.Fatalf("seq 2: ok=%v content=%q", ok, string(got.RawContent))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMVCC_ViewAt_rejectsFutureSeq(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	err = db.ViewAt(99, func(*hexxladb.Tx) error { return nil })
	if err == nil {
		t.Fatal("expected error for future read_seq")
	}
	if !errors.Is(err, hexxladb.ErrReadSeqFuture) {
		t.Fatalf("want ErrReadSeqFuture, got %v", err)
	}
}

func TestMVCC_reopen_seesHistory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "r.db")
	{
		db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
		if err != nil {
			t.Fatal(err)
		}
		p := lattice.PackedCoord{3, 4}
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(context.Background(), record.CellRecord{Key: p, RawContent: "x"})
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := lattice.PackedCoord{3, 4}
	err = db.ViewAt(1, func(tx *hexxladb.Tx) error {
		got, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if !ok || string(got.RawContent) != "x" {
			t.Fatalf("after reopen: ok=%v content=%q err=%v", ok, string(got.RawContent), err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMVCC_StatsAndPruneCellVersions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "prune.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := lattice.PackedCoord{9, 9}
	for _, raw := range []string{"v1", "v2", "v3"} {
		r := record.CellRecord{Key: p, RawContent: raw}
		if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(context.Background(), r) }); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}
	if stats.VersionedRows < 3 || stats.LogicalCells < 1 || stats.CommitSeq < 3 {
		t.Fatalf("unexpected stats before prune: %+v", stats)
	}
	deleted, err := db.PruneCellVersions(3, 10)
	if err != nil {
		t.Fatal(err)
	}
	if deleted < 1 {
		t.Fatalf("expected some versions pruned, got %d", deleted)
	}
	statsAfter, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}
	if statsAfter.VersionedRows >= stats.VersionedRows {
		t.Fatalf("rows should shrink: before=%d after=%d", stats.VersionedRows, statsAfter.VersionedRows)
	}
	err = db.View(func(tx *hexxladb.Tx) error {
		got, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if !ok || got.RawContent != "v3" {
			t.Fatalf("expected latest version after prune, ok=%v raw=%q", ok, got.RawContent)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMVCC_SuggestedPruneBeforeSeq_andPlan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "retention.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{
		EnableMVCC:    true,
		MVCCRetention: hexxladb.MVCCRetention{RetainCommitsBehindHead: 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := lattice.PackedCoord{1, 2}
	for i := range 60 {
		raw := fmt.Sprintf("v%d", i)
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(context.Background(), record.CellRecord{Key: p, RawContent: raw})
		}); err != nil {
			t.Fatal(err)
		}
	}
	bs, ok, err := db.SuggestedPruneBeforeSeq()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected suggested beforeSeq")
	}
	hdr, err := engine.ReadHeaderFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := hdr.CommitSeq - 50; bs != want {
		t.Fatalf("beforeSeq: got %d want %d", bs, want)
	}
	before, maxD, ok2, err := db.MVCCPrunePlan(hexxladb.MVCCPruneBalanced)
	if err != nil || !ok2 || maxD <= 0 || before != bs {
		t.Fatalf("plan before=%v max=%v ok=%v err=%v", before, maxD, ok2, err)
	}
	if _, _, _, err := db.MVCCPrunePlan(hexxladb.MVCCPruneProfile("nope")); !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("plan bad profile: %v", err)
	}
	var sched hexxladb.PruneScheduler
	if _, err := sched.Tick(db); err != nil {
		t.Fatal(err)
	}
}

func TestMVCC_PruneCellVersionsByProfile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "prune_profile.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p := lattice.PackedCoord{7, 7}
	for _, raw := range []string{"v1", "v2", "v3"} {
		r := record.CellRecord{Key: p, RawContent: raw}
		if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(context.Background(), r) }); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := db.PruneCellVersionsByProfile(3, hexxladb.MVCCPruneBalanced)
	if err != nil {
		t.Fatal(err)
	}
	if deleted < 1 {
		t.Fatalf("expected versions pruned for profile, got %d", deleted)
	}
	if _, err := db.PruneCellVersionsByProfile(3, hexxladb.MVCCPruneProfile("unknown")); !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for unknown profile, got %v", err)
	}
}

func TestMVCC_ViewAtTime_visibility(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "view_at_time.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	p := lattice.PackedCoord{4, 4}
	beforeFirst := time.Now().UTC()
	time.Sleep(2 * time.Millisecond)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(context.Background(), record.CellRecord{Key: p, RawContent: "v1"})
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	between := time.Now().UTC()
	time.Sleep(2 * time.Millisecond)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(context.Background(), record.CellRecord{Key: p, RawContent: "v2"})
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.ViewAtTime(beforeFirst, func(tx *hexxladb.Tx) error {
		_, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if ok {
			t.Fatal("expected no visible version before first commit")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.ViewAtTime(between, func(tx *hexxladb.Tx) error {
		got, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if !ok || got.RawContent != "v1" {
			t.Fatalf("expected v1 at intermediate snapshot, ok=%v raw=%q", ok, got.RawContent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
