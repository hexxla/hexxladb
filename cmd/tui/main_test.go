package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/cmdconfig"
)

func TestOpenExplorerRejectsMissingAndOpensEncryptedDatabase(t *testing.T) {
	for _, name := range []string{"HEXXLA_PASSPHRASE", "HEXXLA_ENCRYPTION_KEY"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "explore.db")
	if _, err := cmdconfig.OpenExisting(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("open missing = %v, want ErrNotExist", err)
	}
	for _, candidate := range []string{path, path + "-wal"} {
		if _, err := os.Stat(candidate); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("stat %s = %v, want ErrNotExist", candidate, err)
		}
	}
	key := bytes.Repeat([]byte{1}, 32)
	db, err := hexxladb.Open(path, &hexxladb.Options{EncryptionKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := cmdconfig.OpenExisting(path); !errors.Is(err, hexxladb.ErrEncryptionKeyRequired) {
		t.Fatalf("open without credential = %v, want ErrEncryptionKeyRequired", err)
	}
	t.Setenv("HEXXLA_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	db, err = cmdconfig.OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
}

func TestTabReplyDeliveredAfterTabSwitch(t *testing.T) {
	t.Parallel()
	db, err := hexxladb.Open(filepath.Join(t.TempDir(), "tabs.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	m := newModel(db, "tabs.db")
	browsing, browseCmd := m.Update(tabActivatedMsg{})
	grid, _ := browsing.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	updated, _ := grid.Update(browseCmd())
	state := updated.(model)
	if state.current != 1 || state.tabs[0].(*cellsView).loading {
		t.Fatalf("browse reply did not update its original tab")
	}
}

func TestInspectorIgnoresStaleContextAndDisplaysErrors(t *testing.T) {
	t.Parallel()
	v := &inspectorView{}
	v.selectCell(hexxladb.Coord{})
	v.Update(cellDetailMsg{request: v.request, cell: hexxladb.CellView{RawContent: "first"}})
	v.loadContext(true)
	stale := v.request
	v.selectCell(hexxladb.Coord{Q: 1})
	v.Update(cellDetailMsg{request: v.request, cell: hexxladb.CellView{RawContent: "second"}})
	v.Update(contextLoadedMsg{request: stale, visible: []hexxladb.CellRecord{{RawContent: "stale"}}})
	if v.fovCells != nil {
		t.Fatal("stale FOV was accepted")
	}
	v.loadContext(false)
	v.Update(contextLoadedMsg{request: v.request, err: hexxladb.ErrSpatialScanLimit})
	if got := v.View(); !strings.Contains(got, "Context error") {
		t.Fatalf("view hid context load error: %q", got)
	}
}

func TestCellsDisplaysPartialSearchErrorAndIgnoresOlderReply(t *testing.T) {
	t.Parallel()
	v := &cellsView{input: "test", width: 120, height: 24}
	v.browse()
	stale := v.request
	v.executeSearch()
	v.Update(cellsLoadedMsg{
		request: v.request,
		hits:    []searchResult{{cell: hexxladb.CellView{RawContent: "partial match"}}},
		err:     hexxladb.ErrQueryScanLimit,
	})
	v.Update(cellsLoadedMsg{request: stale})
	if got := v.View(); !strings.Contains(got, "partial match") || !strings.Contains(got, hexxladb.ErrQueryScanLimit.Error()) {
		t.Fatalf("view hid partial results or scan limit: %q", got)
	}
}

func TestTruncationPreservesUnicodeAndStripsControls(t *testing.T) {
	t.Parallel()
	input := "\x1b]52;c;YWJj\a猫 café"
	if got := truncStr(input, 5); got != "猫 c…" {
		t.Fatalf("truncated text = %q", got)
	}
}
