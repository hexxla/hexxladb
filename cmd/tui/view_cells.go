package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/hexxla/hexxladb"
)

type cellsView struct {
	db         *hexxladb.DB
	cells      []hexxladb.CellView
	searchHits []searchResult
	cursor     int
	loading    bool
	searching  bool
	searched   bool
	input      string
	query      string
	searchMode string
	resultMode string
	started    bool
	request    uint64
	err        error
	width      int
	height     int
}

func newCellsView(db *hexxladb.DB) view {
	return &cellsView{db: db, searchMode: "lexical"}
}

func (v *cellsView) SetSize(w, h int) { v.width = w; v.height = h }
func (v *cellsView) Consuming() bool  { return v.searching }

func (v *cellsView) totalRows() int {
	if v.searched {
		return len(v.searchHits)
	}
	return len(v.cells)
}

func (v *cellsView) rowCell(i int) (cell hexxladb.CellView, score float64) {
	if v.searched {
		return v.searchHits[i].cell, v.searchHits[i].score
	}
	return v.cells[i], -1
}

func (v *cellsView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case tabActivatedMsg:
		if !v.started {
			cmd := v.browse()
			return v, cmd
		}
	case cellsLoadedMsg:
		if msg.request != v.request {
			return v, nil
		}
		v.err = msg.err
		v.cells = msg.browse
		v.searchHits = msg.hits
		v.loading = false
		v.cursor = 0
	case tea.KeyMsg:
		if v.searching {
			return v.handleSearchKeyMsg(msg)
		}
		return v.handleNavKeyMsg(msg)
	}
	return v, nil
}

// handleSearchKeyMsg processes key input while the search bar is active.
func (v *cellsView) handleSearchKeyMsg(msg tea.KeyMsg) (view, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		v.searching = false
	case tea.KeyEnter:
		cmd := v.executeSearch()
		return v, cmd
	case tea.KeyBackspace, tea.KeyDelete:
		_, size := utf8.DecodeLastRuneInString(v.input)
		v.input = v.input[:len(v.input)-size]
	default:
		if msg.Type == tea.KeyRunes {
			v.input += string(msg.Runes)
		} else if key := msg.String(); len(key) == 1 && key[0] >= 32 {
			v.input += key
		}
	}
	return v, nil
}

func (v *cellsView) executeSearch() tea.Cmd {
	v.searching = false
	v.query = strings.TrimSpace(v.input)
	if v.query == "" {
		return v.browse()
	}
	v.searched = true
	v.resultMode = v.searchMode
	v.beginLoad()
	db, query, mode, request := v.db, v.query, v.resultMode, v.request
	return func() tea.Msg {
		hits, err := searchCells(db, query, mode)
		return cellsLoadedMsg{request: request, hits: hits, err: err}
	}
}

// handleNavKeyMsg processes key input for navigation/actions outside search mode.
func (v *cellsView) handleNavKeyMsg(msg tea.KeyMsg) (view, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if v.cursor > 0 {
			v.cursor--
		}
	case "down", "j":
		if v.cursor < v.totalRows()-1 {
			v.cursor++
		}
	case "g":
		v.cursor = 0
	case "G":
		if v.totalRows() > 0 {
			v.cursor = v.totalRows() - 1
		}
	case "enter":
		if v.cursor < v.totalRows() {
			c, _ := v.rowCell(v.cursor)
			return v, func() tea.Msg { return inspectCellMsg{coord: c.Coord} }
		}
	case "/":
		v.searching = true
		v.input = ""
		return v, nil
	case "e":
		switch v.searchMode {
		case "lexical":
			v.searchMode = "embedding"
		case "embedding":
			v.searchMode = "hybrid"
		default:
			v.searchMode = "lexical"
		}
		return v, nil
	case "r":
		cmd := v.browse()
		return v, cmd
	}
	return v, nil
}

func (v *cellsView) beginLoad() {
	v.started = true
	v.loading = true
	v.request++
	v.cursor = 0
	v.cells = nil
	v.searchHits = nil
	v.err = nil
}

func (v *cellsView) browse() tea.Cmd {
	v.searched = false
	v.query = ""
	v.beginLoad()
	db, request := v.db, v.request
	return func() tea.Msg { return loadCells(db, request) }
}

func (v *cellsView) View() string {
	switch {
	case v.loading:
		return lipgloss.JoinVertical(lipgloss.Left, v.renderSearchBar(), styleLoading.Render("  ⟳  Loading cells…"))
	case v.totalRows() == 0:
		return v.renderEmptyState()
	default:
		return v.renderCellTable(v.totalRows(), v.searched)
	}
}

func (v *cellsView) renderEmptyState() string {
	text := "  No cells found in database."
	if v.searched {
		text = fmt.Sprintf("  No results for %q.", v.query)
	}
	state := styleDim.Render(text)
	if v.err != nil {
		state = v.renderError()
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		v.renderSearchBar(), state,
		styleHelp.Render("  "+helpItem("/", "new search")+"  "+helpItem("r", "browse cells")),
	)
}

func (v *cellsView) renderError() string {
	if v.searched {
		return styleBad.Render("  Search: " + terminalText(v.err.Error()))
	}
	return styleBad.Render("  Cells: " + terminalText(v.err.Error()))
}

// renderCellTable renders the main cell table with headers, rows, and footer.
func (v *cellsView) renderCellTable(total int, isSearch bool) string {
	w := max(60, v.width-4)

	// ── visible window: reserve title(1)+scrollInfo(1)+searchbar(1-2)+help(1) = 4-5 lines
	extraLines := 4
	if v.searching {
		extraLines++
	}
	if v.err != nil {
		extraLines++
	}
	visH := max(3, v.height-extraLines)

	// Viewport: keep cursor visible, centered when possible
	start := max(0, v.cursor-visH/2)
	end := min(start+visH, total)
	if end-start < visH && start > 0 {
		start = max(0, end-visH)
	}

	// ── column widths
	coordW := 10
	tagsW := 20
	confW := 6
	contentW := max(10, w-coordW-tagsW-confW-10)

	var headers []string
	if isSearch {
		headers = []string{" ", "COORD", "CONTENT", "TAGS", "SCORE"}
	} else {
		headers = []string{" ", "COORD", "CONTENT", "TAGS", "CONF"}
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(colorText2)).
		Headers(headers...).
		Width(w).
		StyleFunc(v.cellTableStyleFunc(start, isSearch))

	for i := start; i < end; i++ {
		c, score := v.rowCell(i)
		marker := styleDim.Render("  ")
		if i == v.cursor {
			marker = lipgloss.NewStyle().Foreground(colorPurple).Background(colorBg3).Render(" ▶")
		}
		coord := fmt.Sprintf("(%d,%d)", c.Coord.Q, c.Coord.R)
		content := truncStr(c.RawContent, contentW)
		tags := truncStr(strings.Join(c.Tags, " "), tagsW)
		var last string
		if isSearch {
			last = fmt.Sprintf("%.2f", score)
		} else {
			last = fmt.Sprintf("%.2f", c.Provenance.Confidence)
		}
		t = t.Row(marker, coord, content, tags, last)
	}

	pct := 0
	if total > 0 {
		pct = (v.cursor + 1) * 100 / total
	}
	title := "◈ Cells"
	if isSearch {
		modeStr := searchModeLabel(v.resultMode)
		title = fmt.Sprintf("◈ Cells  ⌕ %q [%s] — %d results", v.query, modeStr, total)
	}
	scrollInfo := styleDim.Render(fmt.Sprintf("  %d/%d  (%d%%)", v.cursor+1, total, pct))

	help := helpItem("↑↓/jk", "navigate") + "  " +
		helpItem("g/G", "top/bottom") + "  " +
		helpItem("Enter", "inspect") + "  " +
		helpItem("/", "search") + "  " +
		helpItem("e", "cycle: lex→sem→hybrid") + "  " +
		helpItem("r", "browse cells")

	parts := []string{viewTitle(title, v.width), scrollInfo}
	if v.err != nil {
		parts = append(parts, v.renderError())
	}
	if bar := v.renderSearchBar(); bar != "" {
		parts = append(parts, bar)
	}
	tableRender := t.Render()
	// Clip table to viewport height to prevent overflow
	tableRender = lipgloss.NewStyle().MaxHeight(visH).Render(tableRender)
	parts = append(parts, tableRender, styleHelp.Render("  "+help))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// cellTableStyleFunc returns the StyleFunc for the cell table, parameterised by scroll start and mode.
func (v *cellsView) cellTableStyleFunc(start int, isSearch bool) func(row, col int) lipgloss.Style {
	return func(row, col int) lipgloss.Style {
		if row == table.HeaderRow {
			return lipgloss.NewStyle().Foreground(colorPurple).Background(colorBg1).Bold(true).Padding(0, 1)
		}
		actualIdx := start + row
		base := lipgloss.NewStyle().Padding(0, 1).Background(colorBg1)
		switch {
		case actualIdx == v.cursor:
			base = base.Background(colorBg3).Foreground(colorCyan).Bold(true)
		case row%2 == 0:
			base = base.Foreground(colorText1)
		default:
			base = base.Foreground(colorText0)
		}
		switch col {
		case 2: // content
			if actualIdx != v.cursor {
				return base.Foreground(colorText0)
			}
		case 3: // tags
			return base.Foreground(colorYellow)
		case 4: // conf/score
			if isSearch {
				return base.Foreground(colorPurple)
			}
			return base.Foreground(colorGreen)
		}
		return base
	}
}

func searchModeLabel(mode string) string {
	switch mode {
	case "embedding":
		return "semantic"
	case "hybrid":
		return "hybrid"
	default:
		return "lexical"
	}
}

func searchModeColor(mode string) lipgloss.Color {
	switch mode {
	case "embedding":
		return colorPink
	case "hybrid":
		return colorGreen
	default:
		return colorCyan
	}
}

func (v *cellsView) renderSearchBar() string {
	if v.searching {
		modeStr := searchModeLabel(v.searchMode)
		modeClr := searchModeColor(v.searchMode)
		return lipgloss.NewStyle().Foreground(colorCyan).Background(colorBg1).Render("  ⌕  ") +
			lipgloss.NewStyle().Foreground(modeClr).Background(colorBg1).Render("["+modeStr+"] ") +
			lipgloss.NewStyle().Foreground(colorText0).Background(colorBg1).Bold(true).Render(terminalText(v.input)) +
			lipgloss.NewStyle().Foreground(colorPurple).Background(colorBg1).Render("█") +
			styleDim.Render("  Enter=search  Esc=clear")
	}
	return ""
}
