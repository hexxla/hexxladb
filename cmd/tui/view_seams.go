package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

type seamsView struct {
	db     *hexxladb.DB
	seams  []seamRow
	cursor int
	loaded bool
	width  int
	height int
}

func newSeamsView(db *hexxladb.DB) view {
	return &seamsView{db: db}
}

func (v *seamsView) SetSize(w, h int) { v.width = w; v.height = h }

func (v *seamsView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case seamsLoadedMsg:
		if msg.err == nil {
			v.seams = msg.seams
		}
		v.loaded = true
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
			v.loaded = false
			v.seams = nil
			v.cursor = 0
			cmd := v.loadCmd()
			return v, cmd
		}
	}
	if !v.loaded {
		cmd := v.loadCmd()
		return v, cmd
	}
	return v, nil
}

func (v *seamsView) loadCmd() tea.Cmd {
	db := v.db
	return tea.Tick(time.Millisecond, func(_ time.Time) tea.Msg {
		var rows []seamRow
		err := db.View(func(tx *hexxladb.Tx) error {
			return tx.AscendRange(
				[]byte(index.SeamPrefix),
				index.SeamScanUpperBound(),
				func(_, v []byte) bool {
					r, err := record.DecodeSeam(v)
					if err != nil {
						return true
					}
					aCoord, err1 := lattice.Unpack(r.CellA)
					bCoord, err2 := lattice.Unpack(r.CellB)
					if err1 != nil || err2 != nil {
						return true
					}
					status := r.ResolutionStatus
					if status == "" {
						status = "unresolved"
					}
					rows = append(rows, seamRow{
						id:     r.ID,
						coordA: aCoord,
						coordB: bCoord,
						stype:  r.SeamType,
						reason: r.Reason,
						status: status,
					})
					return len(rows) < 10000
				},
			)
		})
		return seamsLoadedMsg{seams: rows, err: err}
	})
}

func (v *seamsView) View() string {
	if !v.loaded {
		return styleLoading.Render("  ⟳  Loading seams…")
	}
	if len(v.seams) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			viewTitle("◈ Seams", v.width),
			"",
			styleDim.Render("  No seams found.  "+styleHelpKey.Render("r")+" to refresh."),
		)
	}

	w := max(60, v.width-6)
	visH := max(3, v.height-8)
	start := 0
	if v.cursor >= visH {
		start = v.cursor - visH + 1
	}
	end := min(start+visH, len(v.seams))

	seamTypeColor := func(st string) lipgloss.Color {
		switch st {
		case hexxladb.SeamTypeSupersedes:
			return colorPink
		case hexxladb.SeamTypeConflict:
			return colorOrange
		default:
			return colorCyan
		}
	}
	statusColor := func(s string) lipgloss.Color {
		switch s {
		case "resolved":
			return colorGreen
		case "unresolved":
			return colorRed
		default:
			return colorText2
		}
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(colorText2)).
		Headers("", "CELL A", "CELL B", "TYPE", "STATUS", "REASON").
		Width(w).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return lipgloss.NewStyle().Foreground(colorPurple).Bold(true).Padding(0, 1)
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
			if actualIdx < len(v.seams) {
				s := v.seams[actualIdx]
				switch col {
				case 3:
					return base.Foreground(seamTypeColor(s.stype))
				case 4:
					return base.Foreground(statusColor(s.status))
				}
			}
			return base
		})

	for i := start; i < end; i++ {
		s := v.seams[i]
		marker := "  "
		if i == v.cursor {
			marker = lipgloss.NewStyle().Foreground(colorPurple).Render(" ▶")
		}
		t = t.Row(
			marker,
			fmt.Sprintf("(%d,%d)", s.coordA.Q, s.coordA.R),
			fmt.Sprintf("(%d,%d)", s.coordB.Q, s.coordB.R),
			s.stype,
			s.status,
			truncStr(s.reason, 30),
		)
	}

	// legend
	legend := lipgloss.JoinHorizontal(lipgloss.Left,
		lipgloss.NewStyle().Foreground(colorOrange).Render("  ⋈ mark_conflict"),
		"   ",
		lipgloss.NewStyle().Foreground(colorPink).Render("↺ supersedes"),
		styleDim.Render(fmt.Sprintf("   total: %d", len(v.seams))),
	)

	help := helpItem("↑↓/jk", "navigate") + "  " + helpItem("r", "refresh")

	return lipgloss.JoinVertical(lipgloss.Left,
		viewTitle("◈ Seams", v.width),
		legend,
		"",
		t.Render(),
		"",
		styleHelp.Render("  "+help),
	)
}
