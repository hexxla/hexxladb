package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/hexxla/hexxladb"
)

type cellsView struct {
	db         *hexxladb.DB
	cells      []hexxladb.CellView
	searchHits []searchResult // non-nil means we are in search-results mode
	cursor     int
	loading    bool
	searching  bool   // search bar is open (typing)
	query      string // last executed query (shown when results are displayed)
	searchMode string // "lexical" or "embedding"
	searchErr  error  // error from last search
	width      int
	height     int
}

func newCellsView(db *hexxladb.DB) view {
	return &cellsView{db: db, loading: true}
}

func (v *cellsView) SetSize(w, h int) { v.width = w; v.height = h }
func (v *cellsView) Consuming() bool  { return v.searching }

// searchHitsLoadedMsg carries the results of a lexical search.
type searchHitsLoadedMsg struct{ hits []searchResult }

func (v *cellsView) totalRows() int {
	if v.searchHits != nil {
		return len(v.searchHits)
	}
	return len(v.cells)
}

func (v *cellsView) rowCell(i int) (cell hexxladb.CellView, score float64) {
	if v.searchHits != nil {
		return v.searchHits[i].cell, v.searchHits[i].score
	}
	return v.cells[i], -1
}

func (v *cellsView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case tabActivatedMsg:
		if v.loading && v.totalRows() == 0 {
			db := v.db
			return v, func() tea.Msg { return cellsLoadedMsg{cells: loadCells(db, 200)} }
		}
		return v, nil
	case cellsLoadedMsg:
		v.cells = msg.cells
		v.searchHits = nil
		v.loading = false
		v.cursor = 0
		return v, nil
	case searchHitsLoadedMsg:
		v.searchHits = msg.hits
		v.loading = false
		v.cursor = 0
		return v, nil
	case embeddingSearchHitsLoadedMsg:
		v.searchErr = msg.err
		if msg.err == nil {
			v.searchHits = msg.hits
		}
		v.loading = false
		v.cursor = 0
		return v, nil
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
		v.query = ""
		v.searchHits = nil
		v.searchErr = nil
		v.loading = true
		v.cells = nil
		db := v.db
		return v, func() tea.Msg { return cellsLoadedMsg{cells: loadCells(db, 200)} }
	case tea.KeyEnter:
		return v.executeSearch()
	case tea.KeyBackspace, tea.KeyDelete:
		if v.query != "" {
			v.query = v.query[:len(v.query)-1]
		}
	default:
		if msg.Type == tea.KeyRunes {
			v.query += string(msg.Runes)
		} else if s := msg.String(); len(s) == 1 && s[0] >= 32 {
			v.query += s
		}
	}
	return v, nil
}

// executeSearch submits the current query for lexical or embedding search.
func (v *cellsView) executeSearch() (view, tea.Cmd) {
	v.searching = false
	v.loading = true
	v.searchHits = nil
	v.searchErr = nil
	q := v.query
	db := v.db
	if v.searchMode == "embedding" {
		return v, func() tea.Msg { return searchByEmbedding(db, q, 200) }
	}
	return v, func() tea.Msg { return searchHitsLoadedMsg{hits: searchCells(db, q, 200)} }
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
		v.query = ""
		return v, nil
	case "e":
		if v.searchMode == "lexical" {
			v.searchMode = "embedding"
		} else {
			v.searchMode = "lexical"
		}
		return v, nil
	case "r":
		return v.resetToBrowse()
	}
	return v, nil
}

// resetToBrowse clears search state and reloads all cells.
func (v *cellsView) resetToBrowse() (view, tea.Cmd) {
	v.loading = true
	v.cells = nil
	v.searchHits = nil
	v.searchErr = nil
	v.query = ""
	v.searching = false
	db := v.db
	return v, func() tea.Msg { return cellsLoadedMsg{cells: loadCells(db, 200)} }
}

func (v *cellsView) View() string {
	if v.loading {
		return styleLoading.Render("  ⟳  Loading cells…")
	}

	total := v.totalRows()
	isSearch := v.searchHits != nil

	if total == 0 {
		if isSearch {
			if v.searchErr != nil {
				return lipgloss.JoinVertical(lipgloss.Left,
					v.renderSearchBar(),
					"",
					styleBad.Render("  ✗  Search error: "+v.searchErr.Error()),
					"",
					styleHelp.Render("  "+helpItem("/", "new search")+"  "+helpItem("r", "browse all")),
				)
			}
			return lipgloss.JoinVertical(lipgloss.Left,
				v.renderSearchBar(),
				"",
				styleDim.Render("  No results for "+styleBad.Render(v.query)+"."),
				"",
				styleHelp.Render("  "+helpItem("/", "new search")+"  "+helpItem("r", "browse all")),
			)
		}
		return styleDim.Render("  No cells found in database.")
	}

	w := max(60, v.width-4)

	// ── visible window: reserve title(1)+scrollInfo(1)+searchbar(1-2)+help(1) = 4-5 lines
	extraLines := 4
	if v.searching || v.query != "" {
		extraLines = 5
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
	scoreW := 6 // used in search mode instead of conf
	confW := 6
	contentW := max(10, w-coordW-tagsW-confW-10)

	var headers []string
	if isSearch {
		headers = []string{" ", "COORD", "CONTENT", "TAGS", "SCORE"}
	} else {
		headers = []string{" ", "COORD", "CONTENT", "TAGS", "CONF"}
	}
	_ = scoreW

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(colorText2)).
		Headers(headers...).
		Width(w).
		StyleFunc(func(row, col int) lipgloss.Style {
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
		})

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
		modeStr := "lexical"
		if v.searchMode == "embedding" {
			modeStr = "semantic"
		}
		title = fmt.Sprintf("◈ Cells  ⌕ %q [%s] — %d results", v.query, modeStr, total)
	}
	scrollInfo := styleDim.Render(fmt.Sprintf("  %d/%d  (%d%%)", v.cursor+1, total, pct))

	help := helpItem("↑↓/jk", "navigate") + "  " +
		helpItem("g/G", "top/bottom") + "  " +
		helpItem("Enter", "inspect") + "  " +
		helpItem("/", "search") + "  " +
		helpItem("e", "toggle mode") + "  " +
		helpItem("r", "browse all")

	parts := []string{viewTitle(title, v.width), scrollInfo}
	if bar := v.renderSearchBar(); bar != "" {
		parts = append(parts, bar)
	}
	tableRender := t.Render()
	// Clip table to viewport height to prevent overflow
	tableRender = lipgloss.NewStyle().MaxHeight(visH).Render(tableRender)
	parts = append(parts, tableRender, styleHelp.Render("  "+help))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (v *cellsView) renderSearchBar() string {
	if v.searching {
		modeStr := "lexical"
		modeClr := colorCyan
		if v.searchMode == "embedding" {
			modeStr = "semantic"
			modeClr = colorPink
		}
		return lipgloss.NewStyle().Foreground(colorCyan).Background(colorBg1).Render("  ⌕  ") +
			lipgloss.NewStyle().Foreground(modeClr).Background(colorBg1).Render("["+modeStr+"] ") +
			lipgloss.NewStyle().Foreground(colorText0).Background(colorBg1).Bold(true).Render(v.query) +
			lipgloss.NewStyle().Foreground(colorPurple).Background(colorBg1).Render("█") +
			styleDim.Render("  Enter=search  Esc=clear")
	}
	if v.query != "" && v.searchHits != nil {
		modeStr := "lexical"
		modeClr := colorYellow
		if v.searchMode == "embedding" {
			modeStr = "semantic"
			modeClr = colorPink
		}
		return styleDim.Render("  ⎔  ") +
			lipgloss.NewStyle().Foreground(modeClr).Background(colorBg1).Render("["+modeStr+"] ") +
			lipgloss.NewStyle().Foreground(colorYellow).Background(colorBg1).Render(v.query) +
			styleDim.Render("  /=new search  r=browse all")
	}
	return ""
}
