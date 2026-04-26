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

	case tea.KeyMsg:
		if v.searching {
			switch msg.Type {
			case tea.KeyEsc:
				v.searching = false
				v.query = ""
				v.searchHits = nil
				v.loading = true
				v.cells = nil
				db := v.db
				return v, func() tea.Msg { return cellsLoadedMsg{cells: loadCells(db, 200)} }
			case tea.KeyEnter:
				v.searching = false
				v.loading = true
				v.searchHits = nil
				q := v.query
				db := v.db
				return v, func() tea.Msg { return searchHitsLoadedMsg{hits: searchCells(db, q, 200)} }
			case tea.KeyBackspace, tea.KeyDelete:
				if v.query != "" {
					v.query = v.query[:len(v.query)-1]
				}
			default:
				if msg.Type == tea.KeyRunes {
					v.query += string(msg.Runes)
				}
			}
			return v, nil
		}
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
		case "r":
			v.loading = true
			v.cells = nil
			v.searchHits = nil
			v.query = ""
			v.searching = false
			db := v.db
			return v, func() tea.Msg { return cellsLoadedMsg{cells: loadCells(db, 200)} }
		}
	}

	if v.loading && v.totalRows() == 0 {
		db := v.db
		return v, func() tea.Msg { return cellsLoadedMsg{cells: loadCells(db, 200)} }
	}
	return v, nil
}

func (v *cellsView) View() string {
	if v.loading {
		return styleLoading.Render("  ⟳  Loading cells…")
	}

	total := v.totalRows()
	isSearch := v.searchHits != nil

	if total == 0 {
		if isSearch {
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

	// ── visible window: reserve title(1)+scrollbar(1)+searchbar(1-2)+help(1) = 5 lines
	extraLines := 4
	if v.searching || v.query != "" {
		extraLines = 5
	}
	visH := max(3, v.height-extraLines)
	start := 0
	if v.cursor >= visH {
		start = v.cursor - visH + 1
	}
	end := min(start+visH, total)

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
		marker := "  "
		if i == v.cursor {
			marker = lipgloss.NewStyle().Foreground(colorPurple).Background(colorBg1).Render(" ▶")
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
		title = fmt.Sprintf("◈ Cells  ⌕ %q  — %d results", v.query, total)
	}
	scrollInfo := styleDim.Render(fmt.Sprintf("  %d/%d  (%d%%)", v.cursor+1, total, pct))

	help := helpItem("↑↓/jk", "navigate") + "  " +
		helpItem("g/G", "top/bottom") + "  " +
		helpItem("Enter", "inspect") + "  " +
		helpItem("/", "search") + "  " +
		helpItem("r", "browse all")

	parts := []string{viewTitle(title, v.width), scrollInfo}
	if bar := v.renderSearchBar(); bar != "" {
		parts = append(parts, bar)
	}
	parts = append(parts, t.Render(), styleHelp.Render("  "+help))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (v *cellsView) renderSearchBar() string {
	if v.searching {
		return lipgloss.NewStyle().Foreground(colorCyan).Background(colorBg1).Render("  ⌕  ") +
			lipgloss.NewStyle().Foreground(colorText0).Background(colorBg1).Bold(true).Render(v.query) +
			lipgloss.NewStyle().Foreground(colorPurple).Background(colorBg1).Render("█") +
			"  " + styleDim.Render("Enter=search  Esc=clear")
	}
	if v.query != "" && v.searchHits != nil {
		return styleDim.Render("  ⌕  ") +
			lipgloss.NewStyle().Foreground(colorYellow).Background(colorBg1).Render(v.query) +
			"  " + styleDim.Render("/=new search  r=browse all")
	}
	return ""
}
