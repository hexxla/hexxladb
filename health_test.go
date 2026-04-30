package hexxladb_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/record"
)

func mustPackTest(t *testing.T, coord hexxladb.Coord) hexxladb.PackedCoord {
	t.Helper()
	pk, err := hexxladb.Pack(coord)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

func TestHealthCheck_EmptyDB(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "health.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	report, err := db.HealthCheck(context.Background(), hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if report.CellCount != 0 {
		t.Errorf("CellCount = %d, want 0", report.CellCount)
	}
	if report.SeamCount != 0 {
		t.Errorf("SeamCount = %d, want 0", report.SeamCount)
	}
	if len(report.OrphanedSeams) != 0 {
		t.Errorf("OrphanedSeams = %v, want none", report.OrphanedSeams)
	}
	if report.TagIndexErrors != 0 {
		t.Errorf("TagIndexErrors = %d, want 0", report.TagIndexErrors)
	}
	if report.SourceIndexErrors != 0 {
		t.Errorf("SourceIndexErrors = %d, want 0", report.SourceIndexErrors)
	}
	if len(report.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", report.Warnings)
	}
}

func TestHealthCheck_WithCellsAndSeams(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "health2.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	ctx := context.Background()
	coordA := hexxladb.Coord{Q: 0, R: 0}
	coordB := hexxladb.Coord{Q: 1, R: 0}
	pkA := mustPackTest(t, coordA)
	pkB := mustPackTest(t, coordB)

	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(ctx, hexxladb.NewFactCell(pkA, "alpha fact", "src", "science", 0.9)); err != nil {
			return err
		}
		if err := tx.PutCell(ctx, hexxladb.NewFactCell(pkB, "beta fact", "src", "science", 0.8)); err != nil {
			return err
		}
		return tx.MarkConflict(coordA, coordB, "contradicts")
	}); err != nil {
		t.Fatal(err)
	}

	report, err := db.HealthCheck(ctx, hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if report.CellCount != 2 {
		t.Errorf("CellCount = %d, want 2", report.CellCount)
	}
	if report.SeamCount != 1 {
		t.Errorf("SeamCount = %d, want 1", report.SeamCount)
	}
	if report.SeamsUnresolved != 1 {
		t.Errorf("SeamsUnresolved = %d, want 1", report.SeamsUnresolved)
	}
	if len(report.OrphanedSeams) != 0 {
		t.Errorf("OrphanedSeams = %v, want none", report.OrphanedSeams)
	}
}

func TestHealthCheck_OrphanedSeam(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "health3.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	ctx := context.Background()
	coordA := hexxladb.Coord{Q: 0, R: 0}
	coordB := hexxladb.Coord{Q: 1, R: 0}
	pkA := mustPackTest(t, coordA)
	pkB := mustPackTest(t, coordB)

	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(ctx, hexxladb.NewUserMessageCell(pkA, "hello", "sess1", 0.9)); err != nil {
			return err
		}
		if err := tx.PutCell(ctx, hexxladb.NewUserMessageCell(pkB, "world", "sess1", 0.9)); err != nil {
			return err
		}
		return tx.MarkConflict(coordA, coordB, "test conflict")
	}); err != nil {
		t.Fatal(err)
	}

	report, err := db.HealthCheck(ctx, hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if len(report.OrphanedSeams) != 0 {
		t.Errorf("OrphanedSeams = %v, want none (both endpoints exist)", report.OrphanedSeams)
	}
}

func TestHealthCheck_ResolvedSeamCounting(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "health4.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	ctx := context.Background()
	coordA := hexxladb.Coord{Q: 0, R: 0}
	coordB := hexxladb.Coord{Q: 2, R: 1}

	now := time.Now().UTC().UnixNano()
	pkA := mustPackTest(t, coordA)
	pkB := mustPackTest(t, coordB)
	var seamID string
	if err := db.Update(func(tx *hexxladb.Tx) error {
		cellA := record.CellRecord{
			Key:        pkA,
			RawContent: "cell a",
			Provenance: record.ProvenanceWire{SourceID: "src", Confidence: 0.9, CreatedAt: now},
		}
		cellB := record.CellRecord{
			Key:        pkB,
			RawContent: "cell b",
			Provenance: record.ProvenanceWire{SourceID: "src", Confidence: 0.7, CreatedAt: now},
		}
		if err := tx.PutCell(ctx, cellA); err != nil {
			return err
		}
		if err := tx.PutCell(ctx, cellB); err != nil {
			return err
		}
		return tx.MarkConflict(coordA, coordB, "conflict")
	}); err != nil {
		t.Fatal(err)
	}

	// Find the seam ID.
	if err := db.View(func(tx *hexxladb.Tx) error {
		seams, err := tx.FindSeams(ctx, hexxladb.Coord{}, 5, false)
		if err != nil {
			return err
		}
		if len(seams) > 0 {
			seamID = seams[0].ID
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if seamID == "" {
		t.Fatal("no seam found")
	}

	// Resolve the seam.
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.ResolveSeam(seamID, "resolved", "test resolution")
	}); err != nil {
		t.Fatal(err)
	}

	report, err := db.HealthCheck(ctx, hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if report.SeamsResolved != 1 {
		t.Errorf("SeamsResolved = %d, want 1", report.SeamsResolved)
	}
	if report.SeamsUnresolved != 0 {
		t.Errorf("SeamsUnresolved = %d, want 0", report.SeamsUnresolved)
	}
}

func TestHealthCheck_MVCC(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "health_mvcc.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	coords := []hexxladb.Coord{{Q: 0, R: 0}, {Q: 1, R: 0}, {Q: 0, R: 1}}
	for _, c := range coords {
		pk := mustPackTest(t, c)
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(ctx, hexxladb.NewFactCell(pk, "mvcc cell", "src-mvcc", "tag-mvcc", 0.9))
		}); err != nil {
			t.Fatal(err)
		}
	}

	report, err := db.HealthCheck(ctx, hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if report.CellCount != 3 {
		t.Errorf("CellCount = %d, want 3", report.CellCount)
	}
	if report.TagIndexErrors != 0 {
		t.Errorf("TagIndexErrors = %d, want 0", report.TagIndexErrors)
	}
	if report.SourceIndexErrors != 0 {
		t.Errorf("SourceIndexErrors = %d, want 0", report.SourceIndexErrors)
	}
}

// Regression: MVCC stores one primary row per seam commit; HealthCheck must not count
// both the initial PutSeam and a later ResolveSeam as two seams.
func TestHealthCheck_MVCC_seam_resolve_not_double_counted(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "health_mvcc_seam.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	pkA := mustPackTest(t, hexxladb.Coord{Q: 0, R: 0})
	pkB := mustPackTest(t, hexxladb.Coord{Q: 1, R: 0})
	var seamID string
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(ctx, hexxladb.NewFactCell(pkA, "a", "s", "t", 0.9)); err != nil {
			return err
		}
		if err := tx.PutCell(ctx, hexxladb.NewFactCell(pkB, "b", "s", "t", 0.9)); err != nil {
			return err
		}
		return tx.MarkConflict(hexxladb.Coord{Q: 0, R: 0}, hexxladb.Coord{Q: 1, R: 0}, "dup")
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *hexxladb.Tx) error {
		seams, err := tx.FindSeams(ctx, hexxladb.Coord{Q: 0, R: 0}, 2, false)
		if err != nil {
			return err
		}
		if len(seams) != 1 {
			return fmt.Errorf("FindSeams want 1 seam, got %d", len(seams))
		}
		seamID = seams[0].ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.ResolveSeam(seamID, "resolved", "fixed")
	}); err != nil {
		t.Fatal(err)
	}
	report, err := db.HealthCheck(ctx, hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if report.SeamCount != 1 {
		t.Errorf("SeamCount = %d, want 1 (one logical seam; two MVCC versions)", report.SeamCount)
	}
	if report.SeamsResolved != 1 || report.SeamsUnresolved != 0 {
		t.Errorf("resolved=%d unresolved=%d want 1/0", report.SeamsResolved, report.SeamsUnresolved)
	}
}

// Regression: MVCC retains historical secondary keys per PutCell seq; deletes only strip the
// visible version. HealthCheck must validate tag/source keys against the matching cell/<coord><seq>,
// not only “visible head cell exists”.
func TestHealthCheck_MVCC_churn_then_delete_cleanSecondaries(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "health_mvcc_churn.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	pk := mustPackTest(t, hexxladb.Coord{Q: 2, R: -1})
	for i := range 4 {
		if err := db.Update(func(tx *hexxladb.Tx) error {
			tag := []string{"t-hist"}
			if i%2 == 0 {
				tag = append(tag, "t-even")
			}
			rec := record.CellRecord{
				Key:        pk,
				RawContent: "rev",
				Provenance: record.ProvenanceWire{SourceID: "sess-churn", Confidence: 1, CreatedAt: 1, UpdatedAt: 1},
				Tags:       tag,
			}
			return tx.PutCell(ctx, rec)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.DeleteCell(ctx, pk)
	}); err != nil {
		t.Fatal(err)
	}

	report, err := db.HealthCheck(ctx, hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if report.TagIndexErrors != 0 {
		t.Errorf("TagIndexErrors = %d, want 0 (historical secondaries are valid); warnings=%v", report.TagIndexErrors, report.Warnings)
	}
	if report.SourceIndexErrors != 0 {
		t.Errorf("SourceIndexErrors = %d, want 0; warnings=%v", report.SourceIndexErrors, report.Warnings)
	}
}

func TestHealthCheck_MaxErrors(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "health5.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})

	ctx := context.Background()
	coord := hexxladb.Coord{Q: 0, R: 0}
	pk := mustPackTest(t, coord)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(ctx, hexxladb.NewFactCell(pk, "some fact", "src", "topic", 1.0))
	}); err != nil {
		t.Fatal(err)
	}

	cfg := hexxladb.DefaultHealthCheckConfig()
	cfg.MaxErrors = 1
	report, err := db.HealthCheck(ctx, cfg)
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	// No errors expected in a healthy DB with MaxErrors set.
	if report.TagIndexErrors > 1 {
		t.Errorf("TagIndexErrors = %d, expected ≤ MaxErrors=1", report.TagIndexErrors)
	}
}
