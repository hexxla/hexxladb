package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hexxla/hexxladb"
)

type seamsView struct {
	noConsume
	db      *hexxladb.DB
	seams   []seamRow
	err     error
	cursor  int
	loaded  bool
	loading bool
	width   int
	height  int
}

func newSeamsView(db *hexxladb.DB) view {
	return &seamsView{db: db}
}

func (v *seamsView) SetSize(w, h int) { v.width = w; v.height = h }

func (v *seamsView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case seamsLoadedMsg:
		v.err = msg.err
		if msg.err == nil {
			v.seams = msg.seams
		}
		v.loaded = true
		v.loading = false
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
			if v.cursor < len(v.seams)-1 {
				v.cursor++
			}
		case "r":
			if v.loading {
				return v, nil
			}
			v.loaded = false
			v.seams = nil
			v.cursor = 0
			cmd := v.loadCmd()
			return v, cmd
		}
	}
	return v, nil
}

func (v *seamsView) loadCmd() tea.Cmd {
	v.loading = true
	db := v.db
	return func() tea.Msg {
		var rows []seamRow
		err := db.View(func(tx *hexxladb.Tx) error {
			return tx.AscendSeams(context.Background(), func(r hexxladb.SeamRecord) bool {
				aCoord, err1 := hexxladb.Unpack(r.CellA)
				bCoord, err2 := hexxladb.Unpack(r.CellB)
				if err1 != nil || err2 != nil {
					return true
				}
				status := r.ResolutionStatus
				if status == "" {
					status = "unresolved"
				}
				rows = append(rows, seamRow{
					coordA: aCoord,
					coordB: bCoord,
					stype:  r.SeamType,
					reason: r.Reason,
					status: status,
				})
				return len(rows) < 10000
			})
		})
		return seamsLoadedMsg{seams: rows, err: err}
	}
}

func (v *seamsView) Help() []string {
	return []string{helpItem("↑↓", "move"), helpItem("r", "refresh")}
}

func (v *seamsView) View() string {
	switch {
	case !v.loaded:
		return styleDim.Render("Loading seams…")
	case v.err != nil:
		return styleBad.Render(fmt.Sprintf("Seams error: %s", terminalText(v.err.Error())))
	case len(v.seams) == 0:
		return styleDim.Render("No seams found.")
	}
	count := len(v.seams)
	room := v.height - 2
	start, end := visibleRows(count, v.cursor, room)
	coordsWidth := len("CELLS")
	typeWidth, statusWidth := len("TYPE"), len("STATUS")
	for _, seam := range v.seams[start:end] {
		coordsWidth = max(coordsWidth, len(coordText(seam.coordA))+len(coordText(seam.coordB))+3)
		typeWidth = max(typeWidth, len(seam.stype))
		statusWidth = max(statusWidth, len(seam.status))
	}
	wide := v.width >= 66
	columns := []column{{label: "CELLS", width: min(coordsWidth, v.width*2/5)}}
	if wide {
		columns = append(columns, column{label: "TYPE", width: min(typeWidth, 16)},
			column{label: "STATUS", width: min(statusWidth, 12)})
	}
	columns = append(columns, column{label: "REASON"})
	table := compactTable{columns: columns, width: v.width}
	summary := "Visible seams"
	if !wide {
		selected := v.seams[v.cursor]
		summary = terminalText(selected.stype + " · " + selected.status)
	}
	return fitRight(styleDim.Render(summary), styleDim.Render(rowPosition(v.cursor, count)), v.width) + "\n" +
		table.render(count, v.cursor, room, func(i int) []string {
			seam := v.seams[i]
			ends := coordText(seam.coordA) + " → " + coordText(seam.coordB)
			if wide {
				return []string{ends, seam.stype, seam.status, seam.reason}
			}
			return []string{ends, seam.reason}
		})
}
