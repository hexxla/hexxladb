// TUI for HexxlaDB — interactive database explorer.
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
	"github.com/hexxla/hexxladb/internal/cmdconfig"
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

	db, err := cmdconfig.OpenExisting(dbPath)
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

var tabNames = [...]string{"Cells", "Hex Grid", "Inspector", "Seams", "Diff"}

const inspectorTab = 2

type model struct {
	dbPath   string
	metadata string
	tabs     [len(tabNames)]view
	current  int
	width    int
	height   int
}

func newModel(db *hexxladb.DB, dbPath string) model {
	return model{
		dbPath:   dbPath,
		metadata: fmt.Sprintf("%d-byte pages · vector dim %d", db.PageSize(), db.EmbeddingDimension()),
		tabs: [len(tabNames)]view{
			newCellsView(db),
			newHexGridView(db),
			newInspectorView(db),
			newSeamsView(db),
			newDiffView(db),
		},
	}
}

func (m model) Init() tea.Cmd {
	// Per bubbletea docs: Init fires the first command. Send tabActivatedMsg as
	// a Cmd so it arrives in Update after the program is running (not discarded).
	// Also request the current window size explicitly for the first render.
	return tea.Batch(
		tea.WindowSize(),
		func() tea.Msg { return tabActivatedMsg{} },
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		tabH := lipgloss.Height(m.renderTabBar())
		statusH := 1
		contentH := max(1, m.height-tabH-statusH)
		contentW := m.width
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
			case "1", "2", "3", "4", "5":
				return m.switchTab(int(msg.String()[0] - '1'))
			case "tab":
				return m.switchTab((m.current + 1) % len(m.tabs))
			case "shift+tab":
				return m.switchTab((m.current - 1 + len(m.tabs)) % len(m.tabs))
			}
		}

	case tabReply:
		if selection, ok := msg.message.(inspectCellMsg); ok {
			if m.current != msg.index {
				return m, nil
			}
			m.current = inspectorTab
			return m.updateTab(inspectorTab, selection)
		}
		return m.updateTab(msg.index, msg.message)
	}
	return m.updateTab(m.current, msg)
}

func (m model) switchTab(idx int) (tea.Model, tea.Cmd) {
	m.current = idx
	return m.updateTab(idx, tabActivatedMsg{})
}

// tabReply delivers asynchronous work to the view that started it.
type tabReply struct {
	index   int
	message tea.Msg
}

func (m model) updateTab(index int, msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.tabs[index].Update(msg)
	m.tabs[index] = next
	if cmd == nil {
		return m, nil
	}
	return m, func() tea.Msg { return tabReply{index: index, message: cmd()} }
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
		Background(appBg.GetBackground()).
		Foreground(subtle.GetForeground()).
		Width(gapW).
		Height(rowH).
		AlignVertical(lipgloss.Bottom).
		Render(strings.Repeat("─", gapW))
	return lipgloss.JoinHorizontal(lipgloss.Bottom, row, gap)
}

func (m model) renderContent() string {
	tabH := lipgloss.Height(m.renderTabBar())
	statusH := 1
	contentH := max(1, m.height-tabH-statusH)
	contentW := contentWidth(m.width)

	inner := ""
	if m.current >= 0 && m.current < len(m.tabs) {
		inner = m.tabs[m.current].View()
	}

	// MaxHeight and Height together ensure content never pushes tabs off screen.
	// Height sets explicit height, MaxHeight clips overflow.
	clipped := lipgloss.NewStyle().MaxHeight(contentH).Height(contentH).Render(inner)
	return lipgloss.NewStyle().Width(contentW).Background(colorBg1).Render(
		lipgloss.Place(
			contentW, contentH,
			lipgloss.Left, lipgloss.Top,
			clipped,
			lipgloss.WithWhitespaceBackground(colorBg1),
		),
	)
}

func (m model) renderStatusBar() string {
	left := styleStatusLeft.Render(" ◈ HexxlaDB ")
	mid := fmt.Sprintf(" %s · %s ", m.dbPath, m.metadata)
	keys := styleStatusRight.Render(" 1-5 tabs · Tab cycle · q quit ")

	midW := max(0, m.width-lipgloss.Width(left)-lipgloss.Width(keys))
	if midW == 0 {
		return lipgloss.JoinHorizontal(lipgloss.Top, left, keys)
	}
	mid = styleStatusMid.Width(midW).Render(truncStr(mid, midW-2))

	return lipgloss.JoinHorizontal(lipgloss.Top, left, mid, keys)
}
