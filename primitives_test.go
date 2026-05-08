package hexxladb_test

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestTx_PutCell_GetCell_roundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "m6.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	c := lattice.Coord{Q: 1, R: -1}
	p, err := lattice.Pack(c)
	if err != nil {
		t.Fatal(err)
	}
	rec := record.CellRecord{
		Key:        p,
		RawContent: "hello",
	}

	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(context.Background(), rec)
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		got, ok, err := tx.GetCell(p)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("expected cell")
		}
		if got.RawContent != "hello" {
			t.Fatalf("content: %q", got.RawContent)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTx_WalkRing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "ring.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	center := lattice.Coord{Q: 0, R: 0}
	ring1 := lattice.Ring(center, 1)
	if len(ring1) != 6 {
		t.Fatalf("ring1 len %d", len(ring1))
	}

	err = db.Update(func(tx *hexxladb.Tx) error {
		for i, c := range ring1 {
			p, err := lattice.Pack(c)
			if err != nil {
				return err
			}
			if err := tx.PutCell(context.Background(), record.CellRecord{Key: p, RawContent: string(rune('a' + i))}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	var seen int
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.WalkRing(ctx, center, 1, func(c lattice.Coord, raw []byte, ok bool) bool {
			if !ok {
				t.Fatal("expected stored cell on ring 1")
			}
			_, err := record.DecodeCell(raw)
			if err != nil {
				t.Fatal(err)
			}
			seen++
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen != 6 {
		t.Fatalf("walked %d", seen)
	}
}

func TestTx_FindSeams_ResolveSeam(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "seam.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	p0, _ := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	p1, _ := lattice.Pack(lattice.Coord{Q: 1, R: 0})
	seam := record.SeamRecord{
		ID:               id,
		CellA:            p0,
		CellB:            p1,
		SeamType:         "test",
		ResolutionStatus: "",
	}

	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutSeam(context.Background(), seam)
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	center := lattice.Coord{Q: 0, R: 0}
	err = db.View(func(tx *hexxladb.Tx) error {
		seams, err := tx.FindSeams(ctx, center, 2, true)
		if err != nil {
			return err
		}
		if len(seams) != 1 || seams[0].ID != id {
			t.Fatalf("FindSeams: %+v", seams)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.ResolveSeam(id, "resolved", "note")
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		seams, err := tx.FindSeams(ctx, center, 2, true)
		if err != nil {
			return err
		}
		if len(seams) != 0 {
			t.Fatalf("expected no unresolved, got %+v", seams)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTx_LoadContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "ctx.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	center := lattice.Coord{Q: 0, R: 0}
	p0, _ := lattice.Pack(center)
	err = db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(context.Background(), record.CellRecord{Key: p0, RawContent: "center"}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	err = db.View(func(tx *hexxladb.Tx) error {
		recs, err := tx.ScanContextRaw(ctx, center, 1, 10)
		if err != nil {
			return err
		}
		if len(recs) != 1 {
			t.Fatalf("len %d", len(recs))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTx_PutSeam_endpointMismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "ep.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	p0, _ := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	p1, _ := lattice.Pack(lattice.Coord{Q: 1, R: 0})
	p2, _ := lattice.Pack(lattice.Coord{Q: 2, R: 0})
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutSeam(context.Background(), record.SeamRecord{
			ID: id, CellA: p0, CellB: p1, SeamType: "t",
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutSeam(context.Background(), record.SeamRecord{
			ID: id, CellA: p0, CellB: p2, SeamType: "t",
		})
	})
	if err == nil {
		t.Fatal("expected ErrSeamEndpointMismatch")
	}
	if !errors.Is(err, hexxladb.ErrSeamEndpointMismatch) {
		t.Fatalf("got %v", err)
	}
}

func TestTx_FindSeams_dedupesBothEndpointsInBall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "dedup.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	p0, _ := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	p1, _ := lattice.Pack(lattice.Coord{Q: 1, R: 0})
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutSeam(context.Background(), record.SeamRecord{ID: id, CellA: p0, CellB: p1, SeamType: "t"})
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	center := lattice.Coord{Q: 0, R: 0}
	err = db.View(func(tx *hexxladb.Tx) error {
		seams, err := tx.FindSeams(ctx, center, 2, false)
		if err != nil {
			return err
		}
		if len(seams) != 1 {
			t.Fatalf("want 1 seam, got %d", len(seams))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTx_ResolveSeam_notFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "nf.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.ResolveSeam("01ARZ3NDEKTSV4RRFFQ69G5FAV", "x", "y")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, hexxladb.ErrSeamNotFound) {
		t.Fatalf("got %v", err)
	}
}
