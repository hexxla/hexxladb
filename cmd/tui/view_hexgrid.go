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
	request uint64
	loading bool
	data    gridLoadedMsg
}

func newHexGridView(db *hexxladb.DB) view {
	return &hexGridView{db: db, center: hexxladb.Coord{}, radius: 3}
}

func (v *hexGridView) SetSize(w, _ int) { v.width = w }

type gridLoadedMsg struct {
	request  uint64
	occupied map[hexxladb.Coord]bool
	density  []hexxladb.RingDensity
	err      error
}

func (v *hexGridView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case tabActivatedMsg:
		if v.request == 0 {
			cmd := v.loadCmd()
			return v, cmd
		}
	case gridLoadedMsg:
		if msg.request == v.request {
			v.loading = false
			v.data = msg
		}
	case tea.KeyMsg:
		cmd := v.move(msg.String())
		return v, cmd
	}
	return v, nil
}

func (v *hexGridView) move(key string) tea.Cmd {
	center, radius := v.center, v.radius
	switch key {
	case "up", "k":
		center.R--
	case "down", "j":
		center.R++
	case "left", "h":
		center.Q--
	case "right", "l":
		center.Q++
	case "+", "=":
		radius = min(8, radius+1)
	case "-":
		radius = max(1, radius-1)
	case "0":
		center = hexxladb.Coord{}
	case "v":
		v.showFOV = !v.showFOV
	case "r":
		return v.loadCmd()
	case "enter":
		return func() tea.Msg { return inspectCellMsg{coord: center} }
	}
	if center == v.center && radius == v.radius {
		return nil
	}
	if _, err := hexxladb.Pack(center); err != nil {
		return nil
	}
	v.center, v.radius = center, radius
	return v.loadCmd()
}

func (v *hexGridView) loadCmd() tea.Cmd {
	v.request++
	v.loading = true
	db, center, radius, request := v.db, v.center, v.radius, v.request
	return func() tea.Msg {
		result := loadGrid(db, center, radius)
		result.request = request
		return result
	}
}

func loadGrid(db *hexxladb.DB, center hexxladb.Coord, radius int) gridLoadedMsg {
	occupied := make(map[hexxladb.Coord]bool)
	ringDensity := make([]hexxladb.RingDensity, radius+1)
	err := db.View(func(tx *hexxladb.Tx) error {
		for ring := range ringDensity {
			density := &ringDensity[ring]
			density.Ring = ring
			coords := hexxladb.Ring(center, ring)
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
	})
	return gridLoadedMsg{occupied: occupied, density: ringDensity, err: err}
}

func (v *hexGridView) View() string {
	if v.loading || v.request == 0 {
		return styleLoading.Render("  ⟳  Loading hex grid…")
	}
	if v.data.err != nil {
		return styleBad.Render(fmt.Sprintf("  ✗  Hex grid error: %v", v.data.err))
	}
	w := max(40, v.width-6)
	occupied := v.data.occupied

	var visible map[hexxladb.Coord]bool
	if v.showFOV {
		visible = inspect.VisibleHexes(v.center, v.radius, occupied)
	}
	grid := hexxladb.RenderHexGrid(context.Background(), v.center, v.radius, func(c hexxladb.Coord) string {
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
	for _, rd := range v.data.density {
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
		helpItem("r", "refresh"),
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
