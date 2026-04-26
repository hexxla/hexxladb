package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hexxla/hexxladb"
)

type hexGridView struct {
	noConsume
	db     *hexxladb.DB
	center hexxladb.Coord
	radius int
	width  int
	height int
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
		case "enter":
			coord := v.center
			return v, func() tea.Msg { return inspectCellMsg{coord: coord} }
		}
	}
	return v, nil
}

func (v *hexGridView) View() string {
	w := max(40, v.width-6)

	// ── grid ─────────────────────────────────────────────────────────────────
	var gridStr string
	_ = v.db.View(func(tx *hexxladb.Tx) error {
		g, err := tx.RenderHexGridFromDB(context.Background(), v.center, v.radius)
		if err == nil {
			gridStr = g
		}
		return nil
	})

	// Color the grid lines
	var coloredGrid strings.Builder
	for line := range strings.SplitSeq(gridStr, "\n") {
		coloredLine := line
		// Dim empty cells, highlight filled
		coloredLine = strings.ReplaceAll(coloredLine, "·",
			lipgloss.NewStyle().Foreground(colorText2).Background(colorBg1).Render("·"))
		coloredLine = strings.ReplaceAll(coloredLine, "●",
			lipgloss.NewStyle().Foreground(colorCyan).Background(colorBg1).Bold(true).Render("●"))
		coloredLine = strings.ReplaceAll(coloredLine, "○",
			lipgloss.NewStyle().Foreground(colorPurple).Background(colorBg1).Render("○"))
		coloredGrid.WriteString(coloredLine + "\n")
	}

	gridPanel := styleBorder.Background(colorBg1).Padding(0, 2).
		Render(
			styleSectionHeader.Render(fmt.Sprintf("Hex Grid — center (%d,%d) radius %d", v.center.Q, v.center.R, v.radius)) + "\n\n" +
				coloredGrid.String(),
		)

	// ── ring stats ────────────────────────────────────────────────────────────
	var ringLines strings.Builder
	var ringDensity []hexxladb.RingDensity
	_ = v.db.View(func(tx *hexxladb.Tx) error {
		var err error
		ringDensity, err = tx.RingDensityMap(context.Background(), v.center, v.radius)
		return err
	})
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
