package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// View shows database overview and quick stats
type dashboardView struct {
	db     *hexxladb.DB
	dbPath string
}

func newDashboardView(db *hexxladb.DB) view {
	return dashboardView{db: db}
}

func (v dashboardView) Update(msg tea.Msg) (view, tea.Cmd) {
	return v, nil
}

func (v dashboardView) View() string {
	var content strings.Builder

	// Get database stats
	stats, _ := v.db.StatsMVCC()

	// Cell count
	var cellCount int
	_ = v.db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendRange(nil, []byte("cell\xff"), func(k, v []byte) bool {
			cellCount++
			return cellCount < 10000
		})
	})

	// Color scheme
	accentColor := lipgloss.Color("99")
	secondaryColor := lipgloss.Color("245")
	highlightBg := lipgloss.Color("236")

	// Title with gradient-like effect
	title := lipgloss.NewStyle().
		Foreground(accentColor).
		Bold(true).
		Underline(true).
		MarginBottom(1).
		Render("HexxlaDB Dashboard")

	content.WriteString(title + "\n")

	// Database info card
	infoCard := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Background(highlightBg).
		Padding(1, 2).
		MarginBottom(1).
		Width(40).
		Render(fmt.Sprintf(
			"MVCC Enabled: %s\nCommit Seq: %d\nCell Count: %d",
			lipgloss.NewStyle().Foreground(lipgloss.Color("43")).Render(yesNo(stats.CommitSeq > 0)),
			stats.CommitSeq,
			cellCount,
		))

	sectionHeader := lipgloss.NewStyle().
		Foreground(secondaryColor).
		Bold(true).
		MarginTop(1).
		MarginBottom(0)

	content.WriteString(sectionHeader.Render("Database Info") + "\n")
	content.WriteString(infoCard + "\n")

	// Quick actions with better styling
	actions := []string{
		"f1 - Dashboard",
		"f2 - Cells",
		"f3 - Hex Grid",
		"f4 - Inspector",
		"f5 - Analytics",
		"f6 - Search",
	}

	actionRows := make([]string, len(actions))
	for i, action := range actions {
		key := strings.Split(action, " - ")[0]
		name := strings.Split(action, " - ")[1]
		actionRows[i] = lipgloss.JoinHorizontal(lipgloss.Left,
			lipgloss.NewStyle().
				Foreground(accentColor).
				Bold(true).
				Width(4).
				Render(key),
			lipgloss.NewStyle().
				Foreground(lipgloss.Color("255")).
				Render(name),
		)
	}

	actionCard := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(secondaryColor).
		Padding(1, 2).
		Render(strings.Join(actionRows, "\n"))

	content.WriteString(sectionHeader.Render("Quick Actions") + "\n")
	content.WriteString(actionCard)

	return content.String()
}

// hexGridView displays the hexagonal lattice
type hexGridView struct {
	db     *hexxladb.DB
	center hexxladb.Coord
	radius int
}

func newHexGridView(db *hexxladb.DB) view {
	return hexGridView{
		db:     db,
		center: hexxladb.Coord{Q: 0, R: 0},
		radius: 3,
	}
}

func (v hexGridView) Update(msg tea.Msg) (view, tea.Cmd) {
	return v, nil
}

func (v hexGridView) View() string {
	content := "Hex Grid View\n"
	content += "============\n\n"
	content += fmt.Sprintf("Center: (%d,%d) | Radius: %d\n", v.center.Q, v.center.R, v.radius)
	content += "\nControls:\n"
	content += "Arrow/WASD - Move center\n"
	content += "+/- - Zoom in/out\n"
	content += "Space - Inspect cell\n"
	content += "g - Go to coordinates\n"

	// Try to render a small hex grid
	_ = v.db.View(func(tx *hexxladb.Tx) error {
		grid, err := tx.RenderHexGridFromDB(context.Background(), v.center, v.radius)
		if err == nil {
			content += "\n" + grid
		}
		return nil
	})

	return content
}

// cellTableView displays cells in a table format
type cellTableView struct {
	db      *hexxladb.DB
	cells   []hexxladb.CellView
	cursor  int
	loading bool
}

func newCellTableView(db *hexxladb.DB) view {
	return cellTableView{
		db:      db,
		cells:   make([]hexxladb.CellView, 0),
		cursor:  0,
		loading: true,
	}
}

func (v cellTableView) Update(msg tea.Msg) (view, tea.Cmd) {
	if v.loading {
		// Load cells asynchronously
		return v, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
			var cells []hexxladb.CellView
			_ = v.db.View(func(tx *hexxladb.Tx) error {
				// Iterate over a larger range of coordinates to find cells
				for q := -20; q <= 20; q++ {
					for r := -20; r <= 20; r++ {
						if len(cells) >= 50 {
							return nil
						}
						coord := hexxladb.Coord{Q: q, R: r}
						pk, err := lattice.Pack(coord)
						if err != nil {
							continue
						}
						cell, ok, _ := tx.GetCell(pk)
						if ok {
							cells = append(cells, hexxladb.CellView{
								Coord:      coord,
								RawContent: cell.RawContent,
								Tags:       cell.Tags,
								Provenance: cell.Provenance,
								Validity:   cell.Validity,
							})
						}
					}
				}
				return nil
			})
			return cellsLoadedMsg{cells: cells}
		})
	}

	switch msg := msg.(type) {
	case cellsLoadedMsg:
		v.cells = msg.cells
		v.loading = false
		return v, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			if v.cursor > 0 {
				v.cursor--
			}
		case "down":
			if v.cursor < len(v.cells)-1 {
				v.cursor++
			}
		case "enter":
			// Drill down into selected cell
			if v.cursor < len(v.cells) {
				return v, tea.Tick(time.Millisecond, func(t time.Time) tea.Msg {
					return inspectCellMsg{coord: v.cells[v.cursor].Coord}
				})
			}
		case "e":
			// Export selected cell
			if v.cursor < len(v.cells) {
				cell := v.cells[v.cursor]
				exportData := fmt.Sprintf("Coord: (%d,%d)\nContent: %s\nTags: %s\nProvenance: %s",
					cell.Coord.Q, cell.Coord.R,
					cell.RawContent,
					strings.Join(cell.Tags, ", "),
					cell.Provenance,
				)
				return v, tea.Tick(time.Millisecond, func(t time.Time) tea.Msg {
					return exportCellMsg{data: exportData}
				})
			}
		case "i":
			// Import cells - show import dialog
			return v, tea.Tick(time.Millisecond, func(t time.Time) tea.Msg {
				return showImportMsg{}
			})
		}
	}
	return v, nil
}

func (v cellTableView) View() string {
	if v.loading {
		loading := lipgloss.NewStyle().
			Foreground(lipgloss.Color("99")).
			Bold(true).
			Render("Loading cells...")
		return loading
	}

	if len(v.cells) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Italic(true).
			Render("No cells found")
		return empty
	}

	var content strings.Builder

	// Color scheme
	accentColor := lipgloss.Color("99")
	secondaryColor := lipgloss.Color("245")
	cursorBg := lipgloss.Color("57")
	cursorFg := lipgloss.Color("16")

	// Title
	title := lipgloss.NewStyle().
		Foreground(accentColor).
		Bold(true).
		Underline(true).
		MarginBottom(1).
		Render("Cell Table")

	content.WriteString(title + "\n")

	// Table header
	header := lipgloss.NewStyle().
		Foreground(accentColor).
		Bold(true).
		Render(fmt.Sprintf("%-10s %-35s %-20s", "Coord", "Content", "Tags"))
	content.WriteString(header + "\n")

	// Create separator
	separator := strings.Repeat("─", 70)
	content.WriteString(lipgloss.NewStyle().Foreground(secondaryColor).Render(separator) + "\n")

	// Render cells
	for i, cell := range v.cells {
		var style lipgloss.Style
		if i == v.cursor {
			style = lipgloss.NewStyle().
				Background(cursorBg).
				Foreground(cursorFg).
				Bold(true)
		} else {
			style = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))
		}

		contentStr := truncate(cell.RawContent, 30)
		tagsStr := strings.Join(cell.Tags, ", ")
		if len(tagsStr) > 18 {
			tagsStr = tagsStr[:18] + "..."
		}

		row := style.Render(fmt.Sprintf("%-10s %-35s %-20s",
			fmt.Sprintf("(%d,%d)", cell.Coord.Q, cell.Coord.R),
			contentStr,
			tagsStr))
		content.WriteString(row + "\n")
	}

	// Controls with better styling
	controls := lipgloss.NewStyle().
		Foreground(secondaryColor).
		MarginTop(1).
		Render("↑/↓ Navigate | Enter Inspect | e Export | i Import")

	content.WriteString(controls)

	return content.String()
}

type cellsLoadedMsg struct {
	cells []hexxladb.CellView
}

type inspectCellMsg struct {
	coord hexxladb.Coord
}

type exportCellMsg struct {
	data string
}

type showImportMsg struct{}

// cellInspectorView shows detailed cell information
type cellInspectorView struct {
	db    *hexxladb.DB
	cell  hexxladb.Coord
	found bool
	data  hexxladb.CellView
}

func newCellInspectorView(db *hexxladb.DB) view {
	return cellInspectorView{
		db:   db,
		cell: hexxladb.Coord{Q: 0, R: 0},
	}
}

func (v cellInspectorView) Update(msg tea.Msg) (view, tea.Cmd) {
	return v, nil
}

func (v cellInspectorView) View() string {
	content := "Cell Inspector\n"
	content += "==============\n\n"

	// Load cell data
	if !v.found {
		pk, err := lattice.Pack(v.cell)
		if err == nil {
			_ = v.db.View(func(tx *hexxladb.Tx) error {
				cell, ok, err := tx.GetCell(pk)
				if err == nil && ok {
					v.data = hexxladb.CellView{
						Coord:      v.cell,
						RawContent: cell.RawContent,
						Tags:       cell.Tags,
						Provenance: cell.Provenance,
						Validity:   cell.Validity,
					}
					v.found = true
				}
				return nil
			})
		}
	}

	if v.found {
		content += fmt.Sprintf("Coordinate: (%d,%d)\n", v.cell.Q, v.cell.R)
		content += fmt.Sprintf("Content: %s\n", truncate(v.data.RawContent, 100))
		content += fmt.Sprintf("Tags: %s\n", strings.Join(v.data.Tags, ", "))
		content += fmt.Sprintf("Source: %s\n", v.data.Provenance.SourceID)
		content += fmt.Sprintf("Confidence: %.2f\n", v.data.Provenance.Confidence)
	} else {
		content += fmt.Sprintf("No cell at (%d,%d)\n", v.cell.Q, v.cell.R)
	}

	content += "\nControls:\n"
	content += "Arrow/WASD - Navigate\n"
	content += "g - Go to coordinates\n"

	return content
}

// analyticsView shows tag analytics and statistics
type analyticsView struct {
	db *hexxladb.DB
}

func newAnalyticsView(db *hexxladb.DB) view {
	return analyticsView{db: db}
}

func (v analyticsView) Update(msg tea.Msg) (view, tea.Cmd) {
	return v, nil
}

func (v analyticsView) View() string {
	var content strings.Builder

	// Color scheme
	accentColor := lipgloss.Color("99")
	secondaryColor := lipgloss.Color("245")

	// Title
	title := lipgloss.NewStyle().
		Foreground(accentColor).
		Bold(true).
		Underline(true).
		MarginBottom(1).
		Render("Analytics")

	content.WriteString(title + "\n")

	// Get tag counts
	_ = v.db.View(func(tx *hexxladb.Tx) error {
		counts, err := tx.TagCounts(context.Background())
		if err == nil && len(counts) > 0 {
			sectionHeader := lipgloss.NewStyle().
				Foreground(secondaryColor).
				Bold(true).
				MarginTop(1).
				Render("Tag Counts")
			content.WriteString(sectionHeader + "\n")

			for i, tc := range counts {
				if i >= 10 {
					break
				}
				bar := lipgloss.NewStyle().
					Foreground(lipgloss.Color("43")).
					Background(lipgloss.Color("236")).
					Width(min(tc.Count, 30)).
					Render(strings.Repeat("█", min(tc.Count, 30)))
				row := lipgloss.JoinHorizontal(lipgloss.Left,
					lipgloss.NewStyle().Width(15).Render(tc.Tag),
					bar,
					lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(fmt.Sprintf(" %d", tc.Count)),
				)
				content.WriteString(row + "\n")
			}
		}

		return nil
	})

	// Database stats
	stats, _ := v.db.StatsMVCC()
	sectionHeader := lipgloss.NewStyle().
		Foreground(secondaryColor).
		Bold(true).
		MarginTop(1).
		Render("MVCC Statistics")
	content.WriteString(sectionHeader + "\n")

	statsCard := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(secondaryColor).
		Padding(1, 2).
		Render(fmt.Sprintf(
			"Commit Seq: %d",
			stats.CommitSeq,
		))
	content.WriteString(statsCard + "\n")

	return content.String()
}

// searchView allows searching and filtering cells
type searchView struct {
	db      *hexxladb.DB
	query   string
	results []hexxladb.CellView
	loading bool
}

func newSearchView(db *hexxladb.DB) view {
	return searchView{
		db:      db,
		query:   "",
		results: make([]hexxladb.CellView, 0),
		loading: false,
	}
}

func (v searchView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			// Perform search
			return v, tea.Tick(time.Millisecond, func(t time.Time) tea.Msg {
				return performSearchMsg{query: v.query}
			})
		case tea.KeyEsc:
			// Clear search
			v.query = ""
			v.results = make([]hexxladb.CellView, 0)
			return v, nil
		case tea.KeyBackspace:
			if len(v.query) > 0 {
				v.query = v.query[:len(v.query)-1]
			}
			return v, nil
		default:
			// Add character to query
			if len(msg.String()) == 1 {
				v.query += msg.String()
			}
			return v, nil
		}
	}
	return v, nil
}

func (v searchView) View() string {
	var content strings.Builder

	// Color scheme
	accentColor := lipgloss.Color("99")
	secondaryColor := lipgloss.Color("245")

	// Title
	title := lipgloss.NewStyle().
		Foreground(accentColor).
		Bold(true).
		Underline(true).
		MarginBottom(1).
		Render("Search")

	content.WriteString(title + "\n")

	// Input field
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(secondaryColor).
		Padding(0, 1).
		Width(60)

	inputContent := v.query
	if inputContent == "" {
		inputContent = "Search cells by content or tag..."
		inputStyle = inputStyle.Foreground(lipgloss.Color("241"))
	} else {
		inputStyle = inputStyle.Foreground(lipgloss.Color("255"))
	}

	content.WriteString(inputStyle.Render(inputContent) + "\n\n")

	// Instructions
	instructions := lipgloss.NewStyle().
		Foreground(secondaryColor).
		Italic(true).
		Render("Type to search, Enter to execute, Esc to clear")
	content.WriteString(instructions + "\n\n")

	if v.loading {
		content.WriteString("Searching...")
		return content.String()
	}

	if len(v.results) > 0 {
		sectionHeader := lipgloss.NewStyle().
			Foreground(secondaryColor).
			Bold(true).
			Render(fmt.Sprintf("Results (%d)", len(v.results)))
		content.WriteString(sectionHeader + "\n")

		for i, cell := range v.results {
			if i >= 10 {
				break
			}
			contentStr := truncate(cell.RawContent, 40)
			tagsStr := strings.Join(cell.Tags, ", ")
			row := fmt.Sprintf("%-10s %-40s %s",
				fmt.Sprintf("(%d,%d)", cell.Coord.Q, cell.Coord.R),
				contentStr,
				tagsStr,
			)
			content.WriteString(row + "\n")
		}
	}

	return content.String()
}

type performSearchMsg struct {
	query string
}

type searchResultsMsg struct {
	results []hexxladb.CellView
}

// Helper functions
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
