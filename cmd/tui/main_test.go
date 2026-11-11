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
	"github.com/charmbracelet/x/ansi"

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
	m := newModel(t.Context(), db, "tabs.db")
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
	v.SetSize(80, 20)
	v.selectCell(hexxladb.Coord{})
	v.Update(cellDetailMsg{request: v.request, cell: hexxladb.CellView{RawContent: "first"}})
	if _, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}}); cmd != nil || v.contextMode {
		t.Fatal("Inspector accepted the removed FOV key")
	}
	pressContext := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}
	if _, cmd := v.Update(pressContext); cmd == nil || !v.contextMode {
		t.Fatal("c did not start radial context loading")
	}
	stale := v.request
	v.selectCell(hexxladb.Coord{Q: 1})
	v.Update(cellDetailMsg{request: v.request, cell: hexxladb.CellView{RawContent: "second"}})
	v.Update(contextLoadedMsg{request: stale, pack: hexxladb.ContextPack{
		Cells: []hexxladb.CellView{{RawContent: "stale"}},
	}})
	if len(v.pack.Cells) != 0 {
		t.Fatal("stale radial context was accepted")
	}
	v.Update(pressContext)
	v.Update(contextLoadedMsg{request: v.request, err: hexxladb.ErrSpatialScanLimit})
	if got := v.View(); !strings.Contains(got, "Context error") {
		t.Fatalf("view hid context load error: %q", got)
	}
}

func TestCellsDisplaysPartialSearchErrorAndIgnoresOlderReply(t *testing.T) {
	t.Parallel()
	v := &cellsView{lifetime: t.Context(), input: "test", width: 120, height: 24}
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

func TestExplorerLayoutFitsWideAndNarrowWindows(t *testing.T) {
	t.Parallel()
	cells := &cellsView{cursor: 29}
	for i := range 30 {
		cells.cells = append(cells.cells, hexxladb.CellView{
			Coord: hexxladb.Coord{Q: i}, RawContent: "猫 café · a longer fixture row", Tags: []string{"example"},
		})
	}
	grid := &hexGridView{radius: 8, request: 1}
	inspector := &inspectorView{selected: true, data: new(hexxladb.CellView{RawContent: strings.Repeat("A long detail.\n", 20)})}
	seams := &seamsView{loaded: true, seams: []seamRow{{stype: "conflict", status: "unresolved", reason: "fixture seam"}}}
	diff := &diffView{loaded: true, diff: new(hexxladb.SnapshotDiff{
		Cells: []hexxladb.CellDiff{{Op: hexxladb.DiffOpPut}},
	})}
	m := model{tabs: [...]view{cells, grid, inspector, seams, diff}}

	for _, size := range []tea.WindowSizeMsg{{Width: 24, Height: 6}, {Width: 40, Height: 12}, {Width: 80, Height: 24}, {Width: 132, Height: 34}} {
		resized, _ := m.Update(size)
		m = resized.(model)
		for i := range m.tabs {
			m.current = i
			lines := strings.Split(m.View(), "\n")
			if len(lines) != size.Height {
				t.Fatalf("view %d at %v has %d rows", i, size, len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size.Width {
					t.Fatalf("view %d at %v overflows on %q", i, size, line)
				}
			}
			if !strings.Contains(ansi.Strip(lines[len(lines)-1]), "q quit") {
				t.Fatalf("view %d at %v lost the quit hint", i, size)
			}
		}
		plain := ansi.Strip(cells.View())
		if !strings.Contains(plain, " COORD") || !strings.Contains(plain, "›(29,0)") || strings.Count(plain, "›") != 1 {
			t.Fatalf("selection is missing or detached at %v: %q", size, plain)
		}
	}
}

func TestInspectorScrollAndReturnToOrigin(t *testing.T) {
	t.Parallel()
	inspector := &inspectorView{
		selected: true,
		data:     new(hexxladb.CellView{RawContent: strings.Repeat("Visible line.\n", 20) + "last line"}),
	}
	inspector.SetSize(40, 10)
	if strings.Contains(inspector.View(), "last line") {
		t.Fatal("last line appeared before scrolling")
	}
	inspector.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !strings.Contains(inspector.View(), "last line") {
		t.Fatal("End did not scroll to the last line")
	}

	grid := &hexGridView{request: 1}
	m := model{current: inspectorTab, returnTo: 1, tabs: [...]view{&cellsView{}, grid, inspector, &seamsView{}, &diffView{}}}
	back, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if back.(model).current != 1 {
		t.Fatal("Escape did not return to the grid")
	}
}
