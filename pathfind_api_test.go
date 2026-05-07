package hexxladb_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func openPathfindTestDB(t *testing.T) *hexxladb.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "pf.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func putCell(tx *hexxladb.Tx, c lattice.Coord, content string) error {
	p, err := lattice.Pack(c)
	if err != nil {
		return err
	}
	return tx.PutCell(context.Background(), record.CellRecord{
		Key:        p,
		RawContent: content,
		Tags:       []string{"test"},
	})
}

func TestFindEdgePath_basic(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)

	coords := []lattice.Coord{{Q: 0, R: 0}, {Q: 1, R: 0}, {Q: 2, R: 0}, {Q: 3, R: 0}}
	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range coords {
			if err := putCell(tx, c, "cell"); err != nil {
				return err
			}
		}
		for i := 0; i < len(coords)-1; i++ {
			if err := tx.LinkCells(coords[i], coords[i+1], "related", 1.0, record.ProvenanceWire{}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		path, err := tx.FindEdgePath(context.Background(), coords[0], coords[3], "", 0)
		if err != nil {
			return err
		}
		if len(path) != 4 {
			t.Fatalf("expected path len=4, got %d: %v", len(path), path)
		}
		if path[0] != coords[0] || path[3] != coords[3] {
			t.Fatalf("endpoints wrong: %v", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFindEdgePath_noEdges(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)

	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range []lattice.Coord{{Q: 0, R: 0}, {Q: 5, R: 5}} {
			if err := putCell(tx, c, "cell"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		path, err := tx.FindEdgePath(context.Background(), lattice.Coord{Q: 0, R: 0}, lattice.Coord{Q: 5, R: 5}, "", 0)
		if err != nil {
			return err
		}
		if path != nil {
			t.Fatalf("expected nil path with no edges, got %v", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFindEdgePath_filtered(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)

	a := lattice.Coord{Q: 0, R: 0}
	b := lattice.Coord{Q: 1, R: 0}
	c := lattice.Coord{Q: 2, R: 0}

	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, coord := range []lattice.Coord{a, b, c} {
			if err := putCell(tx, coord, "cell"); err != nil {
				return err
			}
		}
		if err := tx.LinkCells(a, b, "related", 1.0, record.ProvenanceWire{}); err != nil {
			return err
		}
		if err := tx.LinkCells(b, c, "derived", 1.0, record.ProvenanceWire{}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		path, err := tx.FindEdgePath(context.Background(), a, c, "related", 0)
		if err != nil {
			return err
		}
		if path != nil {
			t.Fatalf("expected nil path with filter=related (can't reach c), got %v", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWalkEdges_basic(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)

	coords := []lattice.Coord{{Q: 0, R: 0}, {Q: 1, R: 0}, {Q: 2, R: 0}}
	err := db.Update(func(tx *hexxladb.Tx) error {
		for _, c := range coords {
			if err := putCell(tx, c, "cell"); err != nil {
				return err
			}
		}
		if err := tx.LinkCells(coords[0], coords[1], "next", 1.0, record.ProvenanceWire{}); err != nil {
			return err
		}
		if err := tx.LinkCells(coords[1], coords[2], "next", 1.0, record.ProvenanceWire{}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		result, err := tx.WalkEdges(context.Background(), coords[0], "", 2, 0)
		if err != nil {
			return err
		}
		if len(result) != 3 {
			t.Fatalf("expected 3 reachable coords, got %d: %v", len(result), result)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextByEdges_basic(t *testing.T) {
	t.Parallel()
	db := openPathfindTestDB(t)

	coords := []lattice.Coord{{Q: 0, R: 0}, {Q: 1, R: 0}, {Q: 2, R: 0}, {Q: 10, R: 10}}
	err := db.Update(func(tx *hexxladb.Tx) error {
		for i, c := range coords {
			if err := putCell(tx, c, fmt.Sprintf("content-%d", i)); err != nil {
				return err
			}
		}
		if err := tx.LinkCells(coords[0], coords[1], "seq", 1.0, record.ProvenanceWire{}); err != nil {
			return err
		}
		if err := tx.LinkCells(coords[1], coords[2], "seq", 1.0, record.ProvenanceWire{}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *hexxladb.Tx) error {
		recs, err := tx.LoadContextByEdges(context.Background(), coords[0], "", 5, 10)
		if err != nil {
			return err
		}
		if len(recs) != 3 {
			t.Fatalf("expected 3 records via edges, got %d", len(recs))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
