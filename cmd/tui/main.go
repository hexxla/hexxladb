// TUI for HexxlaDB - Interactive database explorer and visualization
//
// A terminal interface for browsing HexxlaDB databases with:
// - Hex grid visualization
// - Cell inspection and navigation
// - Tag analytics and statistics
// - Search and filtering
//
// Run: go run ./cmd/tui -path /path/to/database.db
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
)

func main() {
	os.Exit(run())
}

func run() int {
	pathFlag := flag.String("path", "", "database file path (overrides HEXXLA_DB_PATH)")
	flag.Parse()

	dbPath := *pathFlag
	if dbPath == "" {
		dbPath = os.Getenv("HEXXLA_DB_PATH")
	}
	if dbPath == "" {
		fmt.Fprintln(os.Stderr, "Error: database path required (use -path or HEXXLA_DB_PATH)")
		return 1
	}

	// Open database
	db, err := hexxladb.Open(dbPath, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	// Initialize logger
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	// Create and start TUI
	model := newModel(db, dbPath)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		return 1
	}

	return 0
}

// model holds the application state
type model struct {
	db      *hexxladb.DB
	dbPath  string
	current int // index of current view
	views   []view
	width   int
	height  int
}

// view represents different UI screens
type view interface {
	Update(msg tea.Msg) (view, tea.Cmd)
	View() string
}

// newModel creates the initial application model
func newModel(db *hexxladb.DB, dbPath string) model {
	return model{
		db:      db,
		dbPath:  dbPath,
		current: 0,
		views: []view{
			newDashboardView(db),
			newCellTableView(db),
			newHexGridView(db),
			newCellInspectorView(db),
			newAnalyticsView(db),
			newSearchView(db),
		},
	}
}

// Init implements tea.Model
func (m model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// Handle global keys first
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		}

		// Then handle specific key combinations
		switch msg.String() {
		case "q":
			return m, tea.Quit

		case "1":
			m.current = 0
			return m, nil

		case "2":
			m.current = 1
			return m, nil

		case "3":
			m.current = 2
			return m, nil

		case "4":
			m.current = 3
			return m, nil

		case "5":
			m.current = 4
			return m, nil

		case "6":
			m.current = 5
			return m, nil

		case "tab":
			m.current = (m.current + 1) % len(m.views)
			return m, nil

		case "shift+tab":
			m.current = (m.current - 1 + len(m.views)) % len(m.views)
			return m, nil

		case "left", "h":
			if m.current > 0 {
				m.current--
			}
			return m, nil

		case "right", "l":
			if m.current < len(m.views)-1 {
				m.current++
			}
			return m, nil
		}

	case inspectCellMsg:
		// Switch to cell inspector view with the selected cell
		inspector := m.views[3].(cellInspectorView)
		inspector.cell = msg.coord
		inspector.found = false
		m.views[3] = inspector
		m.current = 3
		return m, nil

	case exportCellMsg:
		// Show export dialog with cell data
		fmt.Printf("Exported cell data:\n%s\n", msg.data)
		return m, nil

	case showImportMsg:
		// Show import dialog (placeholder for now)
		fmt.Println("Import dialog - not yet implemented")
		return m, nil

	case performSearchMsg:
		// Perform search in the search view
		search := m.views[5].(searchView)
		search.loading = true
		m.views[5] = search
		return m, tea.Tick(time.Millisecond, func(t time.Time) tea.Msg {
			var results []hexxladb.CellView
			_ = m.db.View(func(tx *hexxladb.Tx) error {
				return tx.AscendRange(nil, []byte("cell\xff"), func(k, v []byte) bool {
					if len(results) >= 50 {
						return false
					}
					// Simple search - in real implementation, this would be more sophisticated
					pk, err := lattice.Pack(hexxladb.Coord{Q: 0, R: 0})
					if err != nil {
						return true
					}
					cell, ok, _ := tx.GetCell(pk)
					if ok {
						results = append(results, hexxladb.CellView{
							Coord:      hexxladb.Coord{Q: 0, R: 0},
							RawContent: cell.RawContent,
							Tags:       cell.Tags,
						})
					}
					return true
				})
			})
			return searchResultsMsg{results: results}
		})

	case searchResultsMsg:
		// Update search view with results
		search := m.views[5].(searchView)
		search.loading = false
		search.results = msg.results
		m.views[5] = search
		return m, nil
	}

	// Delegate to current view
	if m.current >= 0 && m.current < len(m.views) {
		newView, cmd := m.views[m.current].Update(msg)
		m.views[m.current] = newView
		return m, cmd
	}
	return m, nil
}

// View implements tea.Model
func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Layout: tabs | title | content | status bar
	content := m.renderCenterPanel()
	status := m.renderStatusBar()

	// Style components
	tab := lipgloss.NewStyle().
		Border(tabBorder, true).
		BorderForeground(highlight).
		Padding(0, 1)

	activeTab := tab.Border(activeTabBorder, true)

	tabGap := tab.BorderTop(false).BorderLeft(false).BorderRight(false)

	titleStyle := lipgloss.NewStyle().
		MarginLeft(1).
		MarginRight(5).
		Padding(0, 1).
		Italic(true).
		Foreground(lipgloss.Color("#FFF7DB")).
		Render("HexxlaDB TUI")

	contentStyle := lipgloss.NewStyle().
		Width(m.width-2).
		Height(m.height-8).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(textSecondary).
		Background(contentBg).
		Foreground(textPrimary).
		Padding(1, 2).
		MarginTop(1)

	statusStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(1).
		Background(activeBg).
		Foreground(textSecondary)

	// Render tabs
	tabRow := m.renderTabRow(activeTab, tab, tabGap)

	// Render title row
	titleRow := lipgloss.JoinHorizontal(lipgloss.Top, titleStyle)

	// Build layout
	layout := lipgloss.JoinVertical(lipgloss.Left,
		tabRow,
		titleRow,
		contentStyle.Render(content),
		statusStyle.Render(status),
	)

	return layout
}

// renderTabRow creates the tab navigation bar
func (m model) renderTabRow(activeTab, tab, tabGap lipgloss.Style) string {
	tabNames := []string{"Dashboard", "Cells", "Hex Grid", "Inspector", "Analytics", "Search"}
	var tabs []string

	for i, name := range tabNames {
		if i == m.current {
			tabs = append(tabs, activeTab.Render(name))
		} else {
			tabs = append(tabs, tab.Render(name))
		}
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	gap := tabGap.Render(strings.Repeat(" ", max(0, m.width-lipgloss.Width(row)-2)))
	return lipgloss.JoinHorizontal(lipgloss.Bottom, row, gap)
}

// renderCenterPanel shows the current view content
func (m model) renderCenterPanel() string {
	if m.current >= 0 && m.current < len(m.views) {
		return m.views[m.current].View()
	}
	return "Invalid view"
}

// renderStatusBar shows database information
func (m model) renderStatusBar() string {
	mvccEnabled := "no"
	stats, _ := m.db.StatsMVCC()
	if stats.CommitSeq > 0 {
		mvccEnabled = "yes"
	}

	return fmt.Sprintf(" %s | MVCC: %s | f1-f5: navigate | tab: cycle | q: quit",
		m.dbPath, mvccEnabled)
}

// Color scheme - inspired by lipgloss example with neon accents
var (
	// Main colors
	highlight = lipgloss.Color("#874BFD") // purple highlight
	special   = lipgloss.Color("#43BF6D") // green accent
	accent    = lipgloss.Color("#F25D94") // pink accent

	// Backgrounds
	activeBg   = lipgloss.Color("#2a2a2a") // dark gray
	inactiveBg = lipgloss.Color("#1a1a1a") // darker gray
	contentBg  = lipgloss.Color("#0f0f0f") // almost black

	// Text
	textPrimary   = lipgloss.Color("#FAFAFA") // white text
	textSecondary = lipgloss.Color("#969B86") // gray text
	textMuted     = lipgloss.Color("#696969") // muted gray

	// Tab borders
	activeTabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      " ",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "┘",
		BottomRight: "└",
	}

	tabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      "─",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "┴",
		BottomRight: "┴",
	}
)
