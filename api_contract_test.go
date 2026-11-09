package hexxladb_test

import (
	"errors"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
)

func TestHistoricalViewRejectsCurrentGraphAndVectorData(t *testing.T) {
	t.Parallel()
	db := openHookDB(t, &hexxladb.Options{})
	ctx := t.Context()
	from, to := hexxladb.Coord{}, hexxladb.Coord{Q: 1}
	key := mustPackTest(t, from)
	toKey := mustPackTest(t, to)
	for _, text := range []string{"old", "new"} {
		if err := db.Update(func(tx *hexxladb.Tx) error {
			if err := tx.PutCell(ctx, hexxladb.CellRecord{Key: key, RawContent: text}); err != nil {
				return err
			}
			if err := tx.PutEmbedding(key, []float32{1, 0}); err != nil {
				return err
			}
			return tx.LinkCells(from, to, "rel", 1, hexxladb.ProvenanceWire{})
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.ViewAt(1, func(tx *hexxladb.Tx) error {
		cell, ok, err := tx.GetCell(key)
		if err != nil || !ok || cell.RawContent != "old" {
			t.Fatalf("historical cell = %+v, %t, %v", cell, ok, err)
		}
		_, _, edgeErr := tx.GetEdge(key, toKey, "rel")
		_, _, vecErr := tx.GetEmbedding(key)
		_, annErr := tx.SearchByEmbedding([]float32{1, 0}, hexxladb.EmbeddingSearchConfig{})
		_, hybridErr := tx.QueryCells(ctx, hexxladb.CellQuery{Embedding: []float32{1, 0}})
		_, walkErr := tx.WalkEdges(ctx, from, "rel", 1, 1)
		_, graphErr := tx.LoadContext(ctx, hexxladb.LoadContextConfig{Seeds: []hexxladb.Coord{from}, EdgeFilter: "rel"})
		_, viewErr := tx.AssembleCellView(ctx, from, nil, hexxladb.AssembleCellViewOpts{IncludeEdges: true})
		for op, err := range map[string]error{
			"edge": edgeErr, "vector": vecErr, "ANN": annErr, "hybrid": hybridErr,
			"walk": walkErr, "graph context": graphErr, "edge enrichment": viewErr,
		} {
			if !errors.Is(err, hexxladb.ErrHistoricalDataUnavailable) {
				t.Errorf("%s on old snapshot: %v", op, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.ViewAt(2, func(tx *hexxladb.Tx) error {
		_, ok, err := tx.GetEdge(key, toKey, "rel")
		if err != nil || !ok {
			t.Fatalf("head edge: found=%v err=%v", ok, err)
		}
		_, ok, err = tx.GetEmbedding(key)
		if err != nil || !ok {
			t.Fatalf("head vector: found=%v err=%v", ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadContextGraphCountsOnlyLiveResultsAndStopsScanning(t *testing.T) {
	t.Parallel()
	db := openHookDB(t, &hexxladb.Options{})
	seed, target, later := hexxladb.Coord{}, hexxladb.Coord{Q: 1}, hexxladb.Coord{Q: 2}
	seedKey := mustPackTest(t, seed)
	targetKey := mustPackTest(t, target)
	laterKey := mustPackTest(t, later)
	badKey, err := index.EdgeKey(seedKey, laterKey, "irrelevant")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(t.Context(), hexxladb.CellRecord{Key: targetKey}); err != nil {
			return err
		}
		if err := tx.LinkCells(seed, target, "follow", 1, hexxladb.ProvenanceWire{}); err != nil {
			return err
		}
		if err := tx.Put(badKey, []byte("invalid")); !errors.Is(err, hexxladb.ErrInvalidArgument) {
			t.Fatalf("raw edge write: %v", err)
		}
		return hexxladb.PutRawFixture(tx, badKey, []byte("invalid"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *hexxladb.Tx) error {
		pack, err := tx.LoadContext(t.Context(), hexxladb.LoadContextConfig{
			Seeds: []hexxladb.Coord{seed}, EdgeFilter: "follow", MaxCells: 1,
		})
		if err != nil || len(pack.Cells) != 1 || pack.Cells[0].Coord != target || pack.Stats.CandidatesScanned != 2 {
			t.Fatalf("graph pack = %+v, %v", pack, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRawPutProtectsDatabaseNamespaces(t *testing.T) {
	t.Parallel()
	db := openHookDB(t, nil)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		for _, key := range []string{
			"cell/x", "facet/x", "edge/x", "seam/x", "seam-by-cells/x",
			"seam-source/x", "seam-time/x", "source/x", "time/x", "tag/x",
			"embed/x", "hnsw/state", "__meta/x",
		} {
			if err := tx.Put([]byte(key), nil); !errors.Is(err, hexxladb.ErrInvalidArgument) {
				t.Errorf("Put(%q) = %v", key, err)
			}
		}
		return tx.Put([]byte("app/cell/x"), []byte("opaque"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *hexxladb.Tx) error {
		value, ok, err := tx.Get([]byte("app/cell/x"))
		if err != nil || !ok || string(value) != "opaque" {
			t.Fatalf("application key = %q, %v, %v", value, ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueryCellsHitIncludesClusterHintAndSearchRetainsScope(t *testing.T) {
	t.Parallel()
	db := openHookDB(t, nil)
	coord, hint := hexxladb.Coord{Q: 40}, hexxladb.Coord{Q: 39}
	key, hintKey := mustPackTest(t, coord), mustPackTest(t, hint)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), hexxladb.CellRecord{
			Key: key, RawContent: "needle", ClusterHint: new(hintKey),
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.View(func(tx *hexxladb.Tx) error {
		hits, err := tx.QueryCells(t.Context(), hexxladb.CellQuery{Query: "needle"})
		if err != nil || len(hits) != 1 || hits[0].Cell.ClusterHint == nil || *hits[0].Cell.ClusterHint != hint {
			t.Fatalf("query hits = %+v, %v", hits, err)
		}
		legacy, err := tx.SearchCells(t.Context(), hexxladb.CellSearchConfig{Query: "needle"})
		if err != nil || len(legacy) != 0 {
			t.Fatalf("compatibility search scope = %+v, %v", legacy, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
