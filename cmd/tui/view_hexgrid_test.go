package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestHexGridFOVRendersOccludedCell(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "grid.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	key, err := hexxladb.Pack(hexxladb.Coord{Q: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), hexxladb.CellRecord{Key: key, RawContent: "behind a blocker"})
	}); err != nil {
		t.Fatal(err)
	}

	view := &hexGridView{db: db, radius: 2}
	view.Update(view.loadCmd()())
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	if got := ansi.ReplaceAllString(view.View(), ""); !strings.Contains(got, "[●]") {
		t.Fatalf("regular grid does not show the occupied marker: %q", got)
	}
	view.showFOV = true
	if got := ansi.ReplaceAllString(view.View(), ""); !strings.Contains(got, "[○]") || strings.Contains(got, "[●]") {
		t.Fatalf("FOV grid did not mark the occluded cell: %q", got)
	}
}

func TestSeamsViewShowsReadErrors(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "seams.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	view := &seamsView{db: db}
	view.Update(view.loadCmd()())
	if got := view.View(); !strings.Contains(got, "Seams error") {
		t.Fatalf("view hid the read error: %q", got)
	}
}
