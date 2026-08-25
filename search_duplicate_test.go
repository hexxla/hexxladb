package hexxladb_test

import (
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestSearchCells_DuplicateTagsDoNotInflateScore(t *testing.T) {
	t.Parallel()
	db := openSearchDB(t)
	ctx := t.Context()
	first := mustPackTest(t, hexxladb.Coord{Q: 0})
	second := mustPackTest(t, hexxladb.Coord{Q: 1})
	if err := db.Update(func(tx *hexxladb.Tx) error {
		one := hexxladb.NewFactCell(first, "neutral", "src", "topic", 0.9)
		two := hexxladb.NewFactCell(second, "neutral", "src", "topic", 0.9)
		two.Tags = append(two.Tags, "topic", "TOPIC")
		if err := tx.PutCell(ctx, one); err != nil {
			return err
		}
		return tx.PutCell(ctx, two)
	}); err != nil {
		t.Fatal(err)
	}

	var results []hexxladb.CellSearchResult
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		results, err = tx.SearchCells(ctx, hexxladb.CellSearchConfig{Query: "topic", MaxResults: 10})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Score != results[1].Score {
		t.Fatalf("scores = %v and %v, duplicate tags changed relevance", results[0].Score, results[1].Score)
	}
}

func TestNewFactCellDoesNotDuplicateFactTag(t *testing.T) {
	t.Parallel()
	for _, factType := range []string{"", "fact", "FACT"} {
		rec := hexxladb.NewFactCell(hexxladb.PackedCoord{}, "content", "source", factType, 1)
		if len(rec.Tags) != 1 || rec.Tags[0] != "fact" {
			t.Fatalf("factType %q produced tags %#v, want [fact]", factType, rec.Tags)
		}
	}
}
