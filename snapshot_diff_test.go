package hexxladb_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func openDiffDB(t *testing.T, mvcc bool) *hexxladb.DB {
	t.Helper()
	var opts *hexxladb.Options
	if mvcc {
		opts = &hexxladb.Options{EnableMVCC: true}
	}
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "diff.db"), opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func packDiffCoord(t *testing.T, q, r int) lattice.PackedCoord {
	t.Helper()
	pk, err := lattice.Pack(lattice.Coord{Q: q, R: r})
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	return pk
}

func putDiffCell(t *testing.T, db *hexxladb.DB, q, r int, content string) {
	t.Helper()
	pk := packDiffCoord(t, q, r)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{Key: pk, RawContent: content})
	}); err != nil {
		t.Fatalf("PutCell(%d,%d): %v", q, r, err)
	}
}

func putDiffSeam(t *testing.T, db *hexxladb.DB, q0, r0, q1, r1 int) string {
	t.Helper()
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	pkA := packDiffCoord(t, q0, r0)
	pkB := packDiffCoord(t, q1, r1)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutSeam(t.Context(), record.SeamRecord{
			ID: id, CellA: pkA, CellB: pkB, SeamType: "diff-test",
		})
	}); err != nil {
		t.Fatalf("PutSeam: %v", err)
	}
	return id
}

// TestSnapshotDiff_ErrMVCCRequired ensures v1 databases return ErrMVCCRequired.
func TestSnapshotDiff_ErrMVCCRequired(t *testing.T) {
	t.Parallel()
	db := openDiffDB(t, false)
	_, err := db.SnapshotDiff(t.Context(), 0, 0, hexxladb.SnapshotDiffConfig{})
	if !errors.Is(err, hexxladb.ErrMVCCRequired) {
		t.Fatalf("expected ErrMVCCRequired, got %v", err)
	}
}

// TestSnapshotDiff_ErrReadSeqFuture rejects toSeq beyond head.
func TestSnapshotDiff_ErrReadSeqFuture(t *testing.T) {
	t.Parallel()
	db := openDiffDB(t, true)
	putDiffCell(t, db, 0, 0, "x") // seq=1
	_, err := db.SnapshotDiff(t.Context(), 0, 999, hexxladb.SnapshotDiffConfig{})
	if !errors.Is(err, hexxladb.ErrReadSeqFuture) {
		t.Fatalf("expected ErrReadSeqFuture, got %v", err)
	}
}

// TestSnapshotDiff_ErrInvalidArgument rejects fromSeq > toSeq.
func TestSnapshotDiff_ErrInvalidArgument(t *testing.T) {
	t.Parallel()
	db := openDiffDB(t, true)
	putDiffCell(t, db, 0, 0, "x")
	_, err := db.SnapshotDiff(t.Context(), 5, 1, hexxladb.SnapshotDiffConfig{})
	if !errors.Is(err, hexxladb.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

// TestSnapshotDiff_CellsOnly verifies cell diffs are captured correctly.
func TestSnapshotDiff_CellsOnly(t *testing.T) {
	t.Parallel()
	db := openDiffDB(t, true)

	putDiffCell(t, db, 0, 0, "before") // seq=1 — baseline

	stats, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}
	fromSeq := stats.CommitSeq // pin after first write

	putDiffCell(t, db, 1, 0, "alpha") // seq=2
	putDiffCell(t, db, 2, 0, "beta")  // seq=3

	stats2, _ := db.StatsMVCC()
	toSeq := stats2.CommitSeq

	f := false
	diff, err := db.SnapshotDiff(t.Context(), fromSeq, toSeq, hexxladb.SnapshotDiffConfig{
		IncludeSeams: &f,
	})
	if err != nil {
		t.Fatalf("SnapshotDiff: %v", err)
	}
	if diff.FromSeq != fromSeq || diff.ToSeq != toSeq {
		t.Errorf("seq bounds: got from=%d to=%d", diff.FromSeq, diff.ToSeq)
	}
	if len(diff.Cells) != 2 {
		t.Fatalf("expected 2 cell diffs, got %d: %+v", len(diff.Cells), diff.Cells)
	}
	if len(diff.Seams) != 0 {
		t.Errorf("expected 0 seam diffs, got %d", len(diff.Seams))
	}
	// Verify content
	contents := make(map[string]bool)
	for _, c := range diff.Cells {
		contents[c.Record.RawContent] = true
		if c.Op != hexxladb.DiffOpPut {
			t.Errorf("unexpected op %q", c.Op)
		}
	}
	if !contents["alpha"] || !contents["beta"] {
		t.Errorf("missing expected contents, got %v", contents)
	}
}

// TestSnapshotDiff_SeamsOnly verifies seam diffs are captured correctly.
func TestSnapshotDiff_SeamsOnly(t *testing.T) {
	t.Parallel()
	db := openDiffDB(t, true)

	putDiffCell(t, db, 0, 0, "a") // need cells to exist for seam context
	putDiffCell(t, db, 1, 0, "b")

	stats, _ := db.StatsMVCC()
	fromSeq := stats.CommitSeq

	seamID := putDiffSeam(t, db, 0, 0, 1, 0)

	stats2, _ := db.StatsMVCC()
	toSeq := stats2.CommitSeq

	f := false
	diff, err := db.SnapshotDiff(t.Context(), fromSeq, toSeq, hexxladb.SnapshotDiffConfig{
		IncludeCells: &f,
	})
	if err != nil {
		t.Fatalf("SnapshotDiff: %v", err)
	}
	if len(diff.Seams) != 1 {
		t.Fatalf("expected 1 seam diff, got %d", len(diff.Seams))
	}
	if diff.Seams[0].ID != seamID {
		t.Errorf("seam ID mismatch: got %q want %q", diff.Seams[0].ID, seamID)
	}
	if diff.Seams[0].Op != hexxladb.DiffOpPut {
		t.Errorf("unexpected seam op %q", diff.Seams[0].Op)
	}
	if len(diff.Cells) != 0 {
		t.Errorf("expected 0 cell diffs, got %d", len(diff.Cells))
	}
}

// TestSnapshotDiff_EmptyRange returns empty slices when no writes in range.
func TestSnapshotDiff_EmptyRange(t *testing.T) {
	t.Parallel()
	db := openDiffDB(t, true)
	putDiffCell(t, db, 0, 0, "x")
	stats, _ := db.StatsMVCC()
	seq := stats.CommitSeq

	diff, err := db.SnapshotDiff(t.Context(), seq, seq, hexxladb.SnapshotDiffConfig{})
	if err != nil {
		t.Fatalf("SnapshotDiff: %v", err)
	}
	if len(diff.Cells) != 0 || len(diff.Seams) != 0 {
		t.Errorf("expected empty diff, got cells=%d seams=%d", len(diff.Cells), len(diff.Seams))
	}
}

// TestSnapshotDiff_BothCellsAndSeams verifies both are returned together.
func TestSnapshotDiff_BothCellsAndSeams(t *testing.T) {
	t.Parallel()
	db := openDiffDB(t, true)
	putDiffCell(t, db, 0, 0, "a")
	putDiffCell(t, db, 1, 0, "b")

	stats, _ := db.StatsMVCC()
	fromSeq := stats.CommitSeq

	putDiffCell(t, db, 2, 0, "c")
	putDiffSeam(t, db, 0, 0, 1, 0)

	stats2, _ := db.StatsMVCC()
	toSeq := stats2.CommitSeq

	diff, err := db.SnapshotDiff(t.Context(), fromSeq, toSeq, hexxladb.SnapshotDiffConfig{})
	if err != nil {
		t.Fatalf("SnapshotDiff: %v", err)
	}
	if len(diff.Cells) != 1 {
		t.Errorf("expected 1 cell diff, got %d", len(diff.Cells))
	}
	if len(diff.Seams) != 1 {
		t.Errorf("expected 1 seam diff, got %d", len(diff.Seams))
	}
}

// TestSnapshotDiff_CommitSeqOrdering verifies diffs are in ascending commit order.
func TestSnapshotDiff_CommitSeqOrdering(t *testing.T) {
	t.Parallel()
	db := openDiffDB(t, true)

	for i := range 5 {
		putDiffCell(t, db, i, 0, "v")
	}
	stats, _ := db.StatsMVCC()

	diff, err := db.SnapshotDiff(t.Context(), 0, stats.CommitSeq, hexxladb.SnapshotDiffConfig{})
	if err != nil {
		t.Fatalf("SnapshotDiff: %v", err)
	}
	for i := 1; i < len(diff.Cells); i++ {
		if diff.Cells[i].CommitSeq < diff.Cells[i-1].CommitSeq {
			t.Errorf("cell diffs not in ascending commit order at index %d", i)
		}
	}
}

func TestSnapshotDiff_OrdersOppositePhysicalKeysByCommit(t *testing.T) {
	t.Parallel()
	db := openDiffDB(t, true)
	first := packDiffCoord(t, -7, 2)
	second := packDiffCoord(t, 7, -2)
	if bytes.Compare(index.CellKeyWithVersion(first, 1), index.CellKeyWithVersion(second, 2)) < 0 {
		first, second = second, first
	}
	for _, packed := range []lattice.PackedCoord{first, second} {
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(t.Context(), record.CellRecord{Key: packed, RawContent: "ordered"})
		}); err != nil {
			t.Fatal(err)
		}
	}

	diff, err := db.SnapshotDiff(t.Context(), 0, 2, hexxladb.SnapshotDiffConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Cells) != 2 || diff.Cells[0].CommitSeq != 1 || diff.Cells[1].CommitSeq != 2 {
		t.Fatalf("cell diff order = %#v, want commit sequences [1 2]", diff.Cells)
	}
}

func TestSnapshotDiffReportsOnlyRetainedVersionsAfterPrune(t *testing.T) {
	t.Parallel()
	db := openDiffDB(t, true)
	for _, content := range []string{"one", "two", "three"} {
		putDiffCell(t, db, 0, 0, content)
	}
	deleted, err := db.PruneCellVersions(4, 10)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("pruned versions = %d, want 2", deleted)
	}

	diff, err := db.SnapshotDiff(t.Context(), 0, 3, hexxladb.SnapshotDiffConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Cells) != 1 || diff.Cells[0].CommitSeq != 3 || diff.Cells[0].Record.RawContent != "three" {
		t.Fatalf("retained diff = %#v, want only seq 3", diff.Cells)
	}
}

func TestSnapshotDiff_IncludesCellDeletion(t *testing.T) {
	t.Parallel()
	db := openDiffDB(t, true)
	putDiffCell(t, db, 0, 0, "delete-me")
	before, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}
	p := packDiffCoord(t, 0, 0)
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.DeleteCell(t.Context(), p) }); err != nil {
		t.Fatal(err)
	}
	after, err := db.StatsMVCC()
	if err != nil {
		t.Fatal(err)
	}

	diff, err := db.SnapshotDiff(t.Context(), before.CommitSeq, after.CommitSeq, hexxladb.SnapshotDiffConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Cells) != 1 || diff.Cells[0].Op != hexxladb.DiffOpDelete {
		t.Fatalf("cell deletion diff: got %#v", diff.Cells)
	}
}

func TestSnapshotDiff_ReportsCorruptCellVersion(t *testing.T) {
	t.Parallel()
	db := openDiffDB(t, true)
	p := packDiffCoord(t, 0, 0)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.Put(index.CellKeyWithVersion(p, 1), []byte("not-a-cell-record"))
	}); err != nil {
		t.Fatal(err)
	}

	_, err := db.SnapshotDiff(t.Context(), 0, 1, hexxladb.SnapshotDiffConfig{})
	if !errors.Is(err, hexxladb.ErrCorruptDatabase) {
		t.Fatalf("corrupt cell diff: want ErrCorruptDatabase, got %v", err)
	}
}

// TestSnapshotDiff_ContextCancellation respects ctx cancellation.
func TestSnapshotDiff_ContextCancellation(t *testing.T) {
	t.Parallel()
	db := openDiffDB(t, true)
	for i := range 10 {
		putDiffCell(t, db, i, 0, "x")
	}
	stats, _ := db.StatsMVCC()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := db.SnapshotDiff(ctx, 0, stats.CommitSeq, hexxladb.SnapshotDiffConfig{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
