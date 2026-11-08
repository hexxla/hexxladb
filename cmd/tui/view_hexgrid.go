package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/inspect"
)

type hexGridView struct {
	noConsume
	db      *hexxladb.DB
	center  hexxladb.Coord
	radius  int
	showFOV bool // toggle FOV overlay
	width   int
	height  int
}

func newHexGridView(db *hexxladb.DB) view {
	return &hexGridView{db: db, center: hexxladb.Coord{}, radius: 3}
}

func (v *hexGridView) SetSize(w, h int) { v.width = w; v.height = h }

func (v *hexGridView) Update(msg tea.Msg) (view, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "up", "k":
			v.center.R--
		case "down", "j":
			v.center.R++
		case "left", "h":
			v.center.Q--
		case "right", "l":
			v.center.Q++
		case "+", "=":
			if v.radius < 8 {
				v.radius++
			}
		case "-":
			if v.radius > 1 {
				v.radius--
			}
		case "0":
			v.center = hexxladb.Coord{}
		case "v":
			v.showFOV = !v.showFOV
		case "enter":
			coord := v.center
			return v, func() tea.Msg { return inspectCellMsg{coord: coord} }
		}
	}
	return v, nil
}

func (v *hexGridView) View() string {
	w := max(40, v.width-6)

	ctx := context.Background()
	occupied := make(map[hexxladb.Coord]bool)
	ringDensity := make([]hexxladb.RingDensity, v.radius+1)
	if err := v.db.View(func(tx *hexxladb.Tx) error {
		for ring := range ringDensity {
			density := &ringDensity[ring]
			density.Ring = ring
			coords := hexxladb.Ring(v.center, ring)
			density.Total = len(coords)
			for _, c := range coords {
				key, err := hexxladb.Pack(c)
				if err != nil {
					return err
				}
				_, exists, err := tx.GetCell(key)
				if err != nil {
					return err
				}
				if exists {
					occupied[c] = true
					density.Occupied++
				}
			}
		}
		return nil
	}); err != nil {
		return styleBad.Render(fmt.Sprintf("  ✗  Hex grid error: %v", err))
	}

	var visible map[hexxladb.Coord]bool
	if v.showFOV {
		visible = inspect.VisibleHexes(v.center, v.radius, occupied)
	}
	grid := hexxladb.RenderHexGrid(ctx, v.center, v.radius, func(c hexxladb.Coord) string {
		return hexGridMarker(occupied[c], v.showFOV && !visible[c])
	})
	coloredGrid := strings.NewReplacer(
		"·", styleDim.Render("·"),
		"●", lipgloss.NewStyle().Foreground(colorCyan).Background(colorBg1).Bold(true).Render("●"),
		"○", styleDim.Render("○"),
		"×", styleDim.Render("×"),
	).Replace(grid)

	fovLabel := ""
	if v.showFOV {
		fovLabel = lipgloss.NewStyle().Foreground(colorGreen).Background(colorBg1).Render("  FOV ON") +
			styleDim.Render(fmt.Sprintf(" (%d visible)", len(visible)))
	}

	gridPanel := styleBorder.Background(colorBg1).Padding(0, 2).
		Render(
			styleSectionHeader.Render(fmt.Sprintf("Hex Grid — center (%d,%d) radius %d", v.center.Q, v.center.R, v.radius)) + fovLabel + "\n\n" +
				coloredGrid,
		)

	// ── ring stats ────────────────────────────────────────────────────────────
	var ringLines strings.Builder
	for _, rd := range ringDensity {
		pct := 0
		if rd.Total > 0 {
			pct = rd.Occupied * 100 / rd.Total
		}
		bar := barGraph(rd.Occupied, rd.Total, 20, colorCyan)
		ringLines.WriteString(
			lipgloss.JoinHorizontal(lipgloss.Left,
				styleDim.Render(fmt.Sprintf("  ring %d  ", rd.Ring)),
				bar,
				styleDim.Render(fmt.Sprintf("  %d/%d (%d%%)", rd.Occupied, rd.Total, pct)),
			) + "\n",
		)
	}
	statsPanel := styleBorderSubtle.Background(colorBg1).Width(w/3).Padding(0, 1).
		Render(styleSectionHeader.Render("Ring Density") + "\n" + ringLines.String())

	help := strings.Join([]string{
		helpItem("↑↓←→/hjkl", "move center"),
		helpItem("+/-", "zoom"),
		helpItem("v", "toggle FOV (○ hidden cell, × hidden space)"),
		helpItem("0", "origin"),
		helpItem("Enter", "inspect center"),
	}, "  ")

	return lipgloss.JoinVertical(lipgloss.Left,
		viewTitle("◈ Hex Grid", v.width),
		"",
		lipgloss.JoinHorizontal(lipgloss.Top, gridPanel, "  ", statsPanel),
		"",
		styleHelp.Render("  "+help),
	)
}

func hexGridMarker(occupied, hidden bool) string {
	switch {
	case occupied && hidden:
		return "○"
	case occupied:
		return "●"
	case hidden:
		return "×"
	default:
		return "·"
	}
}
