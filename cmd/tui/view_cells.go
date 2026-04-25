package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/hexxla/hexxladb"
)

type cellsView struct {
	db      *hexxladb.DB
	cells   []hexxladb.CellView
	cursor  int
	loading bool
	width   int
	height  int
}

func newCellsView(db *hexxladb.DB) view {
	return &cellsView{db: db, loading: true}
}

func (v *cellsView) SetSize(w, h int) { v.width = w; v.height = h }

func (v *cellsView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case cellsLoadedMsg:
		v.cells = msg.cells
		v.loading = false
		return v, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if v.cursor > 0 {
				v.cursor--
			}
		case "down", "j":
			if v.cursor < len(v.cells)-1 {
				v.cursor++
			}
		case "g":
			v.cursor = 0
		case "G":
			if len(v.cells) > 0 {
				v.cursor = len(v.cells) - 1
			}
		case "enter":
			if v.cursor < len(v.cells) {
				coord := v.cells[v.cursor].Coord
				return v, func() tea.Msg { return inspectCellMsg{coord: coord} }
			}
		case "r":
			v.loading = true
			v.cells = nil
			return v, tea.Tick(time.Millisecond, func(_ time.Time) tea.Msg {
				return cellsLoadedMsg{cells: loadCells(v.db, 200)}
			})
		}
	}

	if v.loading && len(v.cells) == 0 {
		return v, tea.Tick(time.Millisecond, func(_ time.Time) tea.Msg {
			return cellsLoadedMsg{cells: loadCells(v.db, 200)}
		})
	}
	return v, nil
}

func (v *cellsView) View() string {
	if v.loading {
		return styleLoading.Render("  ⟳  Loading cells…")
	}
	if len(v.cells) == 0 {
		return styleDim.Render("  No cells found in database.")
	}

	w := max(60, v.width-6)

	// visible window
	visH := max(5, v.height-6)
	start := 0
	if v.cursor >= visH {
		start = v.cursor - visH + 1
	}
	end := min(start+visH, len(v.cells))

	// column widths: coord=12, content=dynamic, tags=20, conf=6
	coordW := 12
	tagsW := 22
	confW := 6
	contentW := max(20, w-coordW-tagsW-confW-8)

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(colorText2)).
		Headers("COORD", "CONTENT", "TAGS", "CONF").
		Width(w).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return lipgloss.NewStyle().
					Foreground(colorPurple).
					Bold(true).
					Padding(0, 1)
			}
			actualIdx := start + row
			base := lipgloss.NewStyle().Padding(0, 1)
			switch {
			case actualIdx == v.cursor:
				base = base.Background(colorBg3).Foreground(colorCyan).Bold(true)
			case row%2 == 0:
				base = base.Foreground(colorText1)
			default:
				base = base.Foreground(colorText0)
			}
			switch col {
			case 1:
				return base.Foreground(func() lipgloss.Color {
					if actualIdx == v.cursor {
						return colorCyan
					}
					return colorText0
				}())
			case 2:
				return base.Foreground(colorYellow)
			case 3:
				return base.Foreground(colorGreen)
			}
			return base
		})

	var rows [][]string
	for i := start; i < end; i++ {
		c := v.cells[i]
		cursor := "  "
		if i == v.cursor {
			cursor = lipgloss.NewStyle().Foreground(colorPurple).Render(" ▶")
		}
		coord := cursor + fmt.Sprintf("(%d,%d)", c.Coord.Q, c.Coord.R)
		content := truncStr(c.RawContent, contentW)
		tags := truncStr(strings.Join(c.Tags, " "), tagsW)
		conf := fmt.Sprintf("%.2f", c.Provenance.Confidence)
		rows = append(rows, []string{coord, content, tags, conf})
	}
	for _, r := range rows {
		t = t.Row(r...)
	}

	// scroll indicator
	pct := 0
	if len(v.cells) > 0 {
		pct = (v.cursor + 1) * 100 / len(v.cells)
	}
	scrollInfo := styleDim.Render(fmt.Sprintf("  %d/%d  (%d%%)", v.cursor+1, len(v.cells), pct))

	help := strings.Join([]string{
		helpItem("↑↓/jk", "navigate"),
		helpItem("g/G", "top/bottom"),
		helpItem("Enter", "inspect"),
		helpItem("r", "refresh"),
	}, "  ")

	return lipgloss.JoinVertical(lipgloss.Left,
		styleViewTitle.Render("◈ Cells"),
		scrollInfo,
		t.Render(),
		styleHelp.Render("  "+help),
	)
}
