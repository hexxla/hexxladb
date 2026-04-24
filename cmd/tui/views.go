package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// View shows database overview and quick stats
type dashboardView struct {
	db *hexxladb.DB
}

func newDashboardView(db *hexxladb.DB) view {
	return dashboardView{db: db}
}

func (v dashboardView) Update(_ tea.Msg) (view, tea.Cmd) {
	return v, nil
}

func (v dashboardView) View() string {
	var content strings.Builder

	// Get database stats
	stats, _ := v.db.StatsMVCC()

	// Cell count
	var cellCount int
	_ = v.db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendRange(nil, []byte("cell\xff"), func(_, _ []byte) bool {
			cellCount++
			return cellCount < 1000
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

// hexGridView shows a hex grid visualization
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

func (v hexGridView) Update(_ tea.Msg) (view, tea.Cmd) {
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
	// Trigger loading if needed
	if v.loading && len(v.cells) == 0 {
		return v, tea.Tick(time.Millisecond, func(_ time.Time) tea.Msg {
			var cellsWithSeq []cellWithSeq
			_ = v.db.View(func(tx *hexxladb.Tx) error {
				return tx.AscendRange(nil, []byte("cell\xff"), func(k, _ []byte) bool {
					// Key format: "cell/" + packed_coord (16 bytes, big-endian) + commit_seq (8 bytes)
					if len(k) < 5+16+8 {
						return true
					}
					// Skip "cell/" prefix
					packedBytes := k[5:21]
					// Extract commit sequence (last 8 bytes, big-endian)
					commitSeq := uint64(k[21])<<56 | uint64(k[22])<<48 | uint64(k[23])<<40 | uint64(k[24])<<32 | uint64(k[25])<<24 | uint64(k[26])<<16 | uint64(k[27])<<8 | uint64(k[28])

					// Convert bytes to PackedCoord (big-endian: Hi first, then Lo)
					hi := uint64(packedBytes[0])<<56 | uint64(packedBytes[1])<<48 | uint64(packedBytes[2])<<40 | uint64(packedBytes[3])<<32 | uint64(packedBytes[4])<<24 | uint64(packedBytes[5])<<16 | uint64(packedBytes[6])<<8 | uint64(packedBytes[7])
					lo := uint64(packedBytes[8])<<56 | uint64(packedBytes[9])<<48 | uint64(packedBytes[10])<<40 | uint64(packedBytes[11])<<32 | uint64(packedBytes[12])<<24 | uint64(packedBytes[13])<<16 | uint64(packedBytes[14])<<8 | uint64(packedBytes[15])
					pk := lattice.PackedCoord{lo, hi}
					coord, err := lattice.Unpack(pk)
					if err != nil {
						return true
					}
					cell, ok, _ := tx.GetCell(pk)
					if ok {
						cellsWithSeq = append(cellsWithSeq, cellWithSeq{
							cell: hexxladb.CellView{
								Coord:      coord,
								RawContent: cell.RawContent,
								Tags:       cell.Tags,
								Provenance: cell.Provenance,
								Validity:   cell.Validity,
							},
							seq: commitSeq,
						})
					}
					return len(cellsWithSeq) < 50
				})
			})
			// Sort by commit sequence (chronological order)
			sort.Slice(cellsWithSeq, func(i, j int) bool {
				return cellsWithSeq[i].seq < cellsWithSeq[j].seq
			})

			// Extract just the cells
			cells := make([]hexxladb.CellView, len(cellsWithSeq))
			for i, cw := range cellsWithSeq {
				cells[i] = cw.cell
			}

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
				return v, tea.Tick(time.Millisecond, func(_ time.Time) tea.Msg {
					return inspectCellMsg{coord: v.cells[v.cursor].Coord}
				})
			}
		case "e":
			// Export selected cell
			if v.cursor < len(v.cells) {
				cell := v.cells[v.cursor]
				exportData := fmt.Sprintf("Coord: (%d,%d)\nContent: %s\nTags: %s",
					cell.Coord.Q, cell.Coord.R,
					cell.RawContent,
					strings.Join(cell.Tags, ", "),
				)
				return v, tea.Tick(time.Millisecond, func(_ time.Time) tea.Msg {
					return exportCellMsg{data: exportData}
				})
			}
		case "i":
			// Import cells - show import dialog
			return v, tea.Tick(time.Millisecond, func(_ time.Time) tea.Msg {
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

		row := fmt.Sprintf("%-10s %-35s %-20s",
			fmt.Sprintf("(%d,%d)", cell.Coord.Q, cell.Coord.R),
			contentStr,
			tagsStr,
		)
		content.WriteString(style.Render(row) + "\n")
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

type cellWithSeq struct {
	cell hexxladb.CellView
	seq  uint64
}

type inspectCellMsg struct {
	coord hexxladb.Coord
}

type exportCellMsg struct {
	data string
}

type showImportMsg struct{}

type contextPackLoadedMsg struct {
	coord hexxladb.Coord
	pack  string
}

// cellInspectorView shows detailed cell information
type cellInspectorView struct {
	db    *hexxladb.DB
	cell  hexxladb.Coord
	found bool
	data  hexxladb.CellView
	pack  string
}

func newCellInspectorView(db *hexxladb.DB) view {
	return cellInspectorView{
		db:   db,
		cell: hexxladb.Coord{Q: 0, R: 0},
	}
}

func (v cellInspectorView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case contextPackLoadedMsg:
		v.pack = msg.pack
		return v, nil
	case tea.KeyMsg:
		if msg.String() == "c" {
			// Load context pack
			return v, tea.Tick(time.Millisecond, func(_ time.Time) tea.Msg {
				// TODO: Implement actual context pack loading from API
				pack := fmt.Sprintf("Context pack for cell (%d,%d)\n[Context data would be loaded here]", v.cell.Q, v.cell.R)
				return contextPackLoadedMsg{coord: v.cell, pack: pack}
			})
		}
	}
	return v, nil
}

func (v cellInspectorView) View() string {
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
		Render("Cell Inspector")

	content.WriteString(title + "\n")

	// Load cell data
	if !v.found {
		pk, err := lattice.Pack(v.cell)
		if err == nil {
			_ = v.db.View(func(tx *hexxladb.Tx) error {
				c, o, _ := tx.GetCell(pk)
				v.data = hexxladb.CellView{
					Coord:      v.cell,
					RawContent: c.RawContent,
					Tags:       c.Tags,
					Provenance: c.Provenance,
					Validity:   c.Validity,
				}
				v.found = o
				return nil
			})
		}
	}

	if v.found {
		coordStyle := lipgloss.NewStyle().
			Foreground(secondaryColor).
			Bold(true)
		content.WriteString(coordStyle.Render(fmt.Sprintf("Coordinate: (%d,%d)", v.cell.Q, v.cell.R)) + "\n")

		contentStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))
		content.WriteString(contentStyle.Render(fmt.Sprintf("Content: %s", v.data.RawContent)) + "\n")

		tagsStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("43"))
		content.WriteString(tagsStyle.Render(fmt.Sprintf("Tags: %s", strings.Join(v.data.Tags, ", "))) + "\n")
	} else {
		fmt.Fprintf(&content, "No cell at (%d,%d)\n", v.cell.Q, v.cell.R)
	}

	// Display context pack if loaded
	if v.pack != "" {
		packBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentColor).
			Padding(1, 2).
			MarginTop(1).
			Render(v.pack)
		content.WriteString(packBox + "\n")
	}

	// Controls
	controls := lipgloss.NewStyle().
		Foreground(secondaryColor).
		Italic(true).
		Render("g - Go to coordinates | c - Load context pack")
	content.WriteString(controls)

	return content.String()
}

// analyticsView shows tag analytics and statistics
type analyticsView struct {
	db *hexxladb.DB
}

func newAnalyticsView(db *hexxladb.DB) view {
	return analyticsView{db: db}
}

func (v analyticsView) Update(_ tea.Msg) (view, tea.Cmd) {
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
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Type == tea.KeyEnter {
			// Perform search
			return v, tea.Tick(time.Millisecond, func(_ time.Time) tea.Msg {
				return performSearchMsg{query: v.query}
			})
		}
		if keyMsg.Type == tea.KeyEsc {
			// Clear search
			v.query = ""
			v.results = make([]hexxladb.CellView, 0)
			return v, nil
		}
		if keyMsg.Type == tea.KeyBackspace {
			if v.query != "" {
				v.query = v.query[:len(v.query)-1]
			}
			return v, nil
		}
		// Add character to query
		if len(keyMsg.String()) == 1 {
			v.query += keyMsg.String()
		}
		return v, nil
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

func truncate(s string, length int) string {
	if len(s) <= length {
		return s
	}
	return s[:length]
}
