// TUI for HexxlaDB — interactive database explorer.
//
// Run: go run ./cmd/tui -path /path/to/database.db
package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/cmdconfig"
)

func main() {
	os.Exit(run())
}

func run() int {
	pathFlag := flag.String("path", "", "database file path (overrides HEXXLA_DB_PATH)")
	flag.Parse()

	dbPath := cmp.Or(*pathFlag, os.Getenv("HEXXLA_DB_PATH"))
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := tea.NewProgram(newModel(ctx, db, dbPath), tea.WithAltScreen(), tea.WithContext(ctx))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		return 1
	}
	return 0
}

var tabNames = [...]string{"Cells", "Hex Grid", "Inspector", "Seams", "Diff"}

const (
	inspectorTab = 2
	frameRows    = 2
	minWidth     = 24
)

type model struct {
	dbPath   string
	metadata string
	tabs     [len(tabNames)]view
	current  int
	returnTo int
	showHelp bool
	width    int
	height   int
}

func newModel(ctx context.Context, db *hexxladb.DB, dbPath string) model {
	return model{
		dbPath:   dbPath,
		metadata: fmt.Sprintf("%d-byte pages · vector dim %d", db.PageSize(), db.EmbeddingDimension()),
		tabs: [len(tabNames)]view{
			newCellsView(ctx, db),
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
		for _, t := range m.tabs {
			t.SetSize(m.width, max(0, m.height-frameRows))
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
			case "?":
				m.showHelp = !m.showHelp
				return m, nil
			case "esc":
				if m.showHelp {
					m.showHelp = false
					return m, nil
				}
				if m.current == inspectorTab {
					return m.switchTab(m.returnTo)
				}
			case "1", "2", "3", "4", "5":
				return m.switchTab(int(msg.String()[0] - '1'))
			case "tab":
				return m.switchTab((m.current + 1) % len(m.tabs))
			case "shift+tab":
				return m.switchTab((m.current - 1 + len(m.tabs)) % len(m.tabs))
			}
		}
		if m.showHelp {
			return m, nil
		}

	case tabReply:
		if selection, ok := msg.message.(inspectCellMsg); ok {
			if m.current != msg.index {
				return m, nil
			}
			m.returnTo = m.current
			m.current = inspectorTab
			return m.updateTab(inspectorTab, selection)
		}
		return m.updateTab(msg.index, msg.message)
	}
	return m.updateTab(m.current, msg)
}

func (m model) switchTab(idx int) (tea.Model, tea.Cmd) {
	m.current = idx
	m.showHelp = false
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
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.width < minWidth || m.height < 6 {
		return ansi.Truncate("Resize terminal · q quit", m.width, "")
	}

	body := m.tabs[m.current].View()
	if m.showHelp {
		body = m.renderHelp()
	}
	bodyHeight := m.height - frameRows
	body = clipLines(body, m.width, bodyHeight)
	pad := strings.Repeat("\n", bodyHeight-strings.Count(body, "\n")-1)
	return m.renderTabBar() + "\n" + body + pad + "\n" + m.renderFooter()
}

func (m model) renderTabBar() string {
	var tabs []string
	for i, name := range tabNames {
		if m.width < 68 && i == 1 {
			name = "Grid"
		}
		if m.width < 52 && i == inspectorTab {
			name = "Inspect"
		}
		if m.width < 38 && i != m.current {
			name = ""
		}
		label := fmt.Sprint(i + 1)
		if name != "" {
			label += " " + name
		}
		style := styleDim
		if i == m.current {
			style = styleActive
		}
		tabs = append(tabs, style.Render(label))
	}
	separator := "  "
	if m.width < 52 {
		separator = " "
	}
	bar := strings.Join(tabs, separator)
	if m.width >= 68 {
		bar = styleAccent.Render("HexxlaDB") + "   " + bar
	}
	return ansi.Truncate(bar, m.width, "")
}

func (m model) renderFooter() string {
	view := m.tabs[m.current]
	if view.Consuming() {
		return fitRight(helpLine(view.Help()...), m.fileLabel(), m.width)
	}
	controls := view.Help()
	if m.showHelp {
		controls = []string{helpItem("Esc", "close help")}
	}
	global := helpLine(helpItem("?", "help"), helpItem("q", "quit"))
	available := m.width - ansi.StringWidth(global)
	var kept []string
	for _, item := range controls {
		available -= ansi.StringWidth(item) + 2
		if available < 0 {
			break
		}
		kept = append(kept, item)
	}
	return fitRight(helpLine(append(kept, global)...), m.fileLabel(), m.width)
}

func (m model) fileLabel() string {
	return styleDim.Render(truncStr(filepath.Base(m.dbPath), 32))
}

func (m model) renderHelp() string {
	lines := []string{
		styleAccent.Render("Keyboard shortcuts"),
		helpLine(helpItem("1–5", "switch view"), helpItem("Tab / Shift+Tab", "cycle views"), helpItem("q", "quit")),
		styleDim.Render(tabNames[m.current]) + "  " + helpLine(m.tabs[m.current].Help()...),
		styleDim.Render(terminalText(m.dbPath) + " · " + m.metadata),
	}
	return ansi.Wrap(strings.Join(lines, "\n"), m.width, "")
}

func fitRight(left, right string, width int) string {
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 2 {
		return ansi.Truncate(left, width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}
