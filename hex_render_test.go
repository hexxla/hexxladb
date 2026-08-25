package hexxladb_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hexxla/hexxladb"
)

func TestRenderHexGridTruncatesAtRuneBoundary(t *testing.T) {
	t.Parallel()
	rendered := hexxladb.RenderHexGrid(t.Context(), hexxladb.Coord{}, 0, func(hexxladb.Coord) string {
		return "éééééé"
	})
	if !utf8.ValidString(rendered) {
		t.Fatalf("rendered grid is invalid UTF-8: %q", rendered)
	}
	if !strings.Contains(rendered, "[ééééé]") {
		t.Fatalf("rendered grid = %q, want five-rune label", rendered)
	}
}

func TestRenderHexGridFromDBUsesDocumentedMarker(t *testing.T) {
	t.Parallel()
	db := openQueryDB(t)
	coord := hexxladb.Coord{}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), hexxladb.NewFactCell(mustPackTest(t, coord), "occupied", "test", "render", 1))
	}); err != nil {
		t.Fatal(err)
	}
	var rendered string
	if err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		rendered, err = tx.RenderHexGridFromDB(t.Context(), coord, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "[*]") {
		t.Fatalf("rendered grid = %q, want occupied marker [*]", rendered)
	}
}
