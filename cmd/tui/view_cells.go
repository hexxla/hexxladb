package main

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/hexxla/hexxladb"
)

type cellsView struct {
	lifetime   context.Context
	cancelLoad context.CancelFunc
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

func newCellsView(ctx context.Context, db *hexxladb.DB) *cellsView {
	return &cellsView{lifetime: ctx, db: db, searchMode: "lexical"}
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
		v.cancelLoad = nil
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
	ctx, cancel := v.beginLoad()
	db, query, mode, request := v.db, v.query, v.resultMode, v.request
	return func() tea.Msg {
		defer cancel()
		hits, err := searchCells(ctx, db, query, mode)
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

func (v *cellsView) beginLoad() (context.Context, context.CancelFunc) {
	if v.cancelLoad != nil {
		v.cancelLoad()
	}
	ctx, cancel := context.WithCancel(v.lifetime)
	v.cancelLoad = cancel
	v.started = true
	v.loading = true
	v.request++
	v.cursor = 0
	v.cells = nil
	v.searchHits = nil
	v.err = nil
	return ctx, cancel
}

func (v *cellsView) browse() tea.Cmd {
	v.searched = false
	v.query = ""
	ctx, cancel := v.beginLoad()
	db, request := v.db, v.request
	return func() tea.Msg {
		defer cancel()
		return loadCells(ctx, db, request)
	}
}

func (v *cellsView) Help() []string {
	if v.searching {
		return []string{helpItem("Enter", "search"), helpItem("Esc", "cancel")}
	}
	return []string{helpItem("/", "search"), helpItem("Enter", "inspect"),
		helpItem("↑↓/jk", "move"), helpItem("e", "mode"), helpItem("r", "browse"), helpItem("g/G", "first/last")}
}

func (v *cellsView) View() string {
	total := v.totalRows()
	title := styleDim.Render("Browse · " + searchModeLabel(v.searchMode))
	if v.searched {
		title = styleDim.Render(fmt.Sprintf("%s · %q", searchModeLabel(v.resultMode), v.query))
	}
	position := styleDim.Render(rowPosition(v.cursor, total))
	title = ansi.Truncate(title, max(0, v.width-ansi.StringWidth(position)-2), "…")
	lines := []string{fitRight(title, position, v.width)}
	if v.searching {
		prefix := "/ " + searchModeLabel(v.searchMode) + "  "
		text := terminalText(v.input)
		excess := ansi.StringWidth(text) - max(1, v.width-len(prefix)-1)
		if excess > 0 {
			text = ansi.TruncateLeft(text, excess+1, "…")
		}
		lines[0] = styleAccent.Render(prefix) + text + styleAccent.Render("▏")
	}

	switch {
	case v.loading:
		lines = append(lines, styleDim.Render("Loading cells…"))
	case v.err != nil:
		lines = append(lines, styleBad.Render("Search / browse: "+terminalText(v.err.Error())))
	case total == 0 && v.searched:
		lines = append(lines, styleDim.Render("No matching cells."))
	case total == 0:
		lines = append(lines, styleDim.Render("No cells in this database."))
	}
	if total > 0 {
		lines = append(lines, v.renderRows(total, v.height-len(lines)-1))
	}
	return strings.Join(lines, "\n")
}

func (v *cellsView) renderRows(total, room int) string {
	start, end := visibleRows(total, v.cursor, room)
	coordWidth, tagsWidth := len("COORD"), len("TAGS")
	for i := start; i < end; i++ {
		cell, _ := v.rowCell(i)
		coordWidth = max(coordWidth, len(coordText(cell.Coord)))
		tagsWidth = max(tagsWidth, ansi.StringWidth(terminalText(strings.Join(cell.Tags, " "))))
	}
	last := "CONF"
	if v.searched {
		last = "SCORE"
	}
	columns := []column{{label: "COORD", width: min(coordWidth, v.width/3)}, {label: "CONTENT"}}
	showTags := v.width >= 55
	if showTags {
		columns = append(columns, column{label: "TAGS", width: min(tagsWidth, 24, v.width/4)})
	}
	columns = append(columns, column{label: last, width: 6, right: true})
	table := compactTable{columns: columns, width: v.width}
	return table.render(total, v.cursor, room, func(i int) []string {
		cell, score := v.rowCell(i)
		values := []string{coordText(cell.Coord), cell.RawContent}
		if showTags {
			values = append(values, strings.Join(cell.Tags, " "))
		}
		if !v.searched {
			score = cell.Provenance.Confidence
		}
		return append(values, fmt.Sprintf("%.2f", score))
	})
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
