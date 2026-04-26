// TUI for HexxlaDB — interactive database explorer.
//
// Tabs: Dashboard | Cells | Hex Grid | Inspector | Analytics | Seams
//
// Run: go run ./cmd/tui -path /path/to/database.db
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hexxla/hexxladb"
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
		fmt.Fprintln(os.Stderr, "error: database path required (-path or HEXXLA_DB_PATH)")
		return 1
	}

	db, err := hexxladb.Open(dbPath, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	slog.SetDefault(log)

	p := tea.NewProgram(newModel(db, dbPath), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		return 1
	}
	return 0
}

// ── tab definitions ──────────────────────────────────────────────────────────

var tabNames = []string{"Dashboard", "Cells", "Hex Grid", "Inspector", "Analytics", "Seams", "Health", "Diff"}

// ── model ────────────────────────────────────────────────────────────────────

type model struct {
	db      *hexxladb.DB
	dbPath  string
	tabs    []view
	current int
	width   int
	height  int
}

func newModel(db *hexxladb.DB, dbPath string) model {
	return model{
		db:     db,
		dbPath: dbPath,
		tabs: []view{
			newDashboardView(db),
			newCellsView(db),
			newHexGridView(db),
			newInspectorView(db),
			newAnalyticsView(db),
			newSeamsView(db),
			newHealthView(db),
			newDiffView(db),
		},
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		contentH := m.height - 5 // tabs(3) + statusbar(1) + 1 for gap line
		contentW := m.width - 4  // subtract Padding(0,2) added in renderContent
		for _, t := range m.tabs {
			t.SetSize(contentW, contentH)
		}
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		// Only handle global shortcuts when the current tab is not consuming
		// keypresses (e.g. the Cells search bar is closed).
		if !m.tabs[m.current].Consuming() {
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "1", "2", "3", "4", "5", "6", "7", "8":
				idx := int(msg.String()[0] - '1')
				if idx >= 0 && idx < len(m.tabs) {
					return m.switchTab(idx)
				}
			case "tab":
				return m.switchTab((m.current + 1) % len(m.tabs))
			case "shift+tab":
				return m.switchTab((m.current - 1 + len(m.tabs)) % len(m.tabs))
			}
		}

	case inspectCellMsg:
		// Switch to Inspector tab and deliver the message to it.
		const inspectorIdx = 3
		m.current = inspectorIdx
		newV, cmd := m.tabs[inspectorIdx].Update(msg)
		m.tabs[inspectorIdx] = newV
		return m, cmd
	}

	// Delegate all other messages to the current tab.
	newV, cmd := m.tabs[m.current].Update(msg)
	m.tabs[m.current] = newV
	return m, cmd
}

func (m model) switchTab(idx int) (tea.Model, tea.Cmd) {
	m.current = idx
	// Trigger lazy load on the target tab by sending a tick with no-op message.
	newV, cmd := m.tabs[idx].Update(tabActivatedMsg{})
	m.tabs[idx] = newV
	return m, cmd
}

// tabActivatedMsg is sent when a tab becomes active (for lazy loading).
type tabActivatedMsg struct{}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}

	tabBar := m.renderTabBar()
	content := m.renderContent()
	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, tabBar, content, statusBar)
}

func (m model) renderTabBar() string {
	var rendered []string
	for i, name := range tabNames {
		if i == m.current {
			rendered = append(rendered, styleTabActive.Render(name))
		} else {
			rendered = append(rendered, styleTabInactive.Render(name))
		}
	}
	row := lipgloss.JoinHorizontal(lipgloss.Bottom, rendered...)
	rowH := lipgloss.Height(row)
	gapW := max(0, m.width-lipgloss.Width(row))
	// Gap fills horizontally; height must match the tab row so bottom-edge aligns.
	gap := lipgloss.NewStyle().
		Background(colorBg0).
		Foreground(colorText2).
		Width(gapW).
		Height(rowH).
		AlignVertical(lipgloss.Bottom).
		Render(strings.Repeat("─", gapW))
	return lipgloss.JoinHorizontal(lipgloss.Bottom, row, gap)
}

func (m model) renderContent() string {
	contentH := m.height - 5
	contentW := m.width
	if contentH < 1 {
		contentH = 1
	}

	inner := ""
	if m.current >= 0 && m.current < len(m.tabs) {
		inner = m.tabs[m.current].View()
	}

	// Place fills the entire content area with colorBg1, then overlays the inner
	// view top-left. This eliminates background "holes" from unstyled fragments.
	return lipgloss.NewStyle().Background(colorBg1).Render(
		lipgloss.Place(
			contentW, contentH,
			lipgloss.Left, lipgloss.Top,
			lipgloss.NewStyle().Padding(0, 2).MaxHeight(contentH).Render(inner),
			lipgloss.WithWhitespaceBackground(colorBg1),
		),
	)
}

func (m model) renderStatusBar() string {
	stats, _ := m.db.StatsMVCC()
	mvcc := styleGood.Render("MVCC on")
	if stats.CommitSeq == 0 {
		mvcc = styleDim.Render("MVCC off")
	}

	left := styleStatusLeft.Render(" ◈ HexxlaDB ")
	mid := styleStatusMid.Render(fmt.Sprintf(" %s  seq:%d  %s ", truncStr(m.dbPath, 40), stats.CommitSeq, mvcc))
	keys := styleStatusRight.Render(" 1-8 tabs · Tab cycle · q quit ")

	midW := max(0, m.width-lipgloss.Width(left)-lipgloss.Width(keys))
	mid = styleStatusMid.Width(midW).Render(mid)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, mid, keys)
}
