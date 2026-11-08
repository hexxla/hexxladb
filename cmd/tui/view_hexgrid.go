package main

import (
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
	request uint64
	loading bool
	data    gridLoadedMsg
}

func newHexGridView(db *hexxladb.DB) view {
	return &hexGridView{db: db, center: hexxladb.Coord{}, radius: 3}
}

func (v *hexGridView) SetSize(w, h int) { v.width, v.height = w, h }

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

func (v *hexGridView) Help() []string {
	return []string{helpItem("↑↓←→", "pan"), helpItem("+/-", "zoom"), helpItem("v", "FOV"),
		helpItem("Enter", "inspect"), helpItem("0", "origin"), helpItem("r", "refresh")}
}

func (v *hexGridView) View() string {
	if v.loading || v.request == 0 {
		return styleDim.Render("Loading hex grid…")
	}
	if v.data.err != nil {
		return styleBad.Render("Hex grid error: " + terminalText(v.data.err.Error()))
	}
	radius := min(v.radius, max(0, (v.width-1)/4), max(0, (v.height-3)/2))
	title := fmt.Sprintf("%s · radius %d", coordText(v.center), v.radius)
	if radius < v.radius {
		title += fmt.Sprintf(" · showing %d", radius)
	}
	if v.showFOV {
		title += " · FOV on"
	}
	grid := v.renderGrid(radius)
	gridWidth := 4*radius + 1
	density := v.densityLines()
	if v.width >= gridWidth+22 && v.height >= len(density)+1 {
		panel := lipgloss.JoinHorizontal(lipgloss.Top, grid, "   ", strings.Join(density, "\n"))
		return styleAccent.Render(title) + "\n" + panel
	}
	return styleAccent.Render(title) + "\n" + grid + "\n" + density[0]
}

func (v *hexGridView) renderGrid(radius int) string {
	occupied := v.data.occupied
	var visible map[hexxladb.Coord]bool
	if v.showFOV {
		visible = inspect.VisibleHexes(v.center, v.radius, occupied)
	}
	var grid strings.Builder
	for r := -radius; r <= radius; r++ {
		if r > -radius {
			grid.WriteByte('\n')
		}
		first, last := max(-radius, -radius-r), min(radius, radius-r)
		grid.WriteString(strings.Repeat(" ", 2*(first+radius)+r))
		for q := first; q <= last; q++ {
			if q > first {
				grid.WriteByte(' ')
			}
			c := hexxladb.Coord{Q: v.center.Q + q, R: v.center.R + r}
			marker := hexGridMarker(occupied[c], v.showFOV && !visible[c])
			switch {
			case c == v.center && occupied[c]:
				grid.WriteString(styleActive.Render("◆"))
			case c == v.center:
				grid.WriteString(styleActive.Render("◇"))
			case occupied[c] && (!v.showFOV || visible[c]):
				grid.WriteString(styleAccent.Render(marker))
			default:
				grid.WriteString(styleDim.Render(marker))
			}
		}
	}
	return grid.String()
}

func (v *hexGridView) densityLines() []string {
	positions := 1 + 3*v.radius*(v.radius+1)
	lines := []string{styleDim.Render(fmt.Sprintf("Occupied %d/%d", len(v.data.occupied), positions))}
	if v.showFOV {
		lines = append(lines, styleDim.Render("○ occluded · × hidden"))
	}
	for _, ring := range v.data.density {
		lines = append(lines, styleDim.Render(fmt.Sprintf("Ring %d  %d/%d", ring.Ring, ring.Occupied, ring.Total)))
	}
	return lines
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
