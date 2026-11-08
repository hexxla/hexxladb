package main

import (
	"context"
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hexxla/hexxladb"
)

type diffView struct {
	noConsume
	db      *hexxladb.DB
	diff    *hexxladb.SnapshotDiff
	err     error
	loaded  bool
	loading bool
	cursor  int
	width   int
	height  int
}

func newDiffView(db *hexxladb.DB) view {
	return &diffView{db: db}
}

func (v *diffView) SetSize(w, h int) { v.width = w; v.height = h }

func (v *diffView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case snapshotDiffMsg:
		v.diff = msg.diff
		v.err = msg.err
		v.loaded = true
		v.loading = false
		v.cursor = 0
		return v, nil
	case tabActivatedMsg:
		if !v.loaded && !v.loading {
			cmd := v.loadCmd()
			return v, cmd
		}
		return v, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if v.cursor > 0 {
				v.cursor--
			}
		case "down", "j":
			if v.diff != nil && v.cursor < len(v.diff.Cells)-1 {
				v.cursor++
			}
		case "r":
			if v.loading {
				return v, nil
			}
			v.loaded = false
			v.diff = nil
			v.err = nil
			cmd := v.loadCmd()
			return v, cmd
		}
	}
	return v, nil
}

func (v *diffView) loadCmd() tea.Cmd {
	v.loading = true
	db := v.db
	return func() tea.Msg {
		stats, err := db.StatsMVCC()
		if err != nil {
			return snapshotDiffMsg{err: err}
		}
		fromSeq := stats.CommitSeq - min(stats.CommitSeq, 10)
		diff, err := db.SnapshotDiff(context.Background(), fromSeq, stats.CommitSeq, hexxladb.SnapshotDiffConfig{})
		return snapshotDiffMsg{diff: &diff, err: err}
	}
}

func (v *diffView) Help() []string {
	return []string{helpItem("↑↓", "move"), helpItem("r", "refresh")}
}

func (v *diffView) View() string {
	switch {
	case !v.loaded:
		return styleDim.Render("Computing snapshot diff…")
	case errors.Is(v.err, hexxladb.ErrMVCCRequired):
		return styleDim.Render("Snapshot diff requires MVCC.")
	case v.err != nil:
		return styleBad.Render("Diff error: " + terminalText(v.err.Error()))
	}
	d := v.diff
	if d == nil || (len(d.Cells) == 0 && len(d.Seams) == 0) {
		return styleDim.Render("No changes in the last ten commits.")
	}

	summary := styleDim.Render(fmt.Sprintf("%d cell · %d seam changes", len(d.Cells), len(d.Seams)))
	if v.width >= 50 {
		summary += styleDim.Render(fmt.Sprintf(" · commits %d–%d", d.FromSeq, d.ToSeq))
	}
	if len(d.Cells) == 0 {
		return summary
	}
	room := v.height - 2
	start, end := visibleRows(len(d.Cells), v.cursor, room)
	coordWidth := len("COORD")
	for _, cell := range d.Cells[start:end] {
		coordWidth = max(coordWidth, len(coordText(cell.Coord)))
	}
	table := compactTable{
		columns: []column{{label: "OP", width: 6}, {label: "COORD", width: min(coordWidth, v.width/4)}, {label: "CONTENT"}},
		width:   v.width,
	}
	return fitRight(summary, styleDim.Render(rowPosition(v.cursor, len(d.Cells))), v.width) + "\n" +
		table.render(len(d.Cells), v.cursor, room, func(i int) []string {
			cell := d.Cells[i]
			return []string{string(cell.Op), coordText(cell.Coord), cell.Record.RawContent}
		})
}
