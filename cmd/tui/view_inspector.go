package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/hexxla/hexxladb"
)

type inspectorView struct {
	noConsume
	db          *hexxladb.DB
	cell        hexxladb.Coord
	data        *hexxladb.CellView
	pack        *hexxladb.ContextPack
	fovCells    []hexxladb.CellRecord // FOV-filtered context results
	packLoading bool
	selected    bool
	cellLoading bool
	cellErr     error
	packErr     error
	request     uint64
	width       int
	height      int
	scroll      int
	contextMode bool
	lines       []string
}

func newInspectorView(db *hexxladb.DB) view {
	return &inspectorView{db: db}
}

func (v *inspectorView) SetSize(w, h int) {
	v.width, v.height = w, h
	v.reflow()
}

func (v *inspectorView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case inspectCellMsg:
		cmd := v.selectCell(msg.coord)
		return v, cmd
	case cellDetailMsg:
		if msg.request == v.request {
			v.cellLoading = false
			v.cellErr = msg.err
			if msg.err == nil {
				v.data = &msg.cell
			}
			v.reflow()
		}
	case contextLoadedMsg:
		if msg.request == v.request {
			v.packLoading = false
			v.packErr = msg.err
			v.pack = msg.pack
			v.fovCells = msg.visible
			v.reflow()
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "c", "f":
			if v.data != nil {
				cmd := v.loadContext(msg.String() == "f")
				return v, cmd
			}
		case "d":
			v.contextMode = false
			v.scroll = 0
			v.reflow()
		case "up", "k":
			v.scrollBy(-1)
		case "down", "j":
			v.scrollBy(1)
		case "pgup":
			v.scrollBy(-v.pageHeight())
		case "pgdown":
			v.scrollBy(v.pageHeight())
		case "g", "home":
			v.scroll = 0
		case "G", "end":
			v.scroll = v.maxScroll()
		case "r":
			if v.selected {
				cmd := v.selectCell(v.cell)
				return v, cmd
			}
		}
	}
	return v, nil
}

func (v *inspectorView) selectCell(coord hexxladb.Coord) tea.Cmd {
	v.cell = coord
	v.selected = true
	v.cellLoading = true
	v.cellErr = nil
	v.data = nil
	v.contextMode = false
	v.scroll = 0
	v.clearContext()
	v.reflow()
	db, request := v.db, v.request
	return func() tea.Msg {
		var cell hexxladb.CellView
		err := db.View(func(tx *hexxladb.Tx) error {
			var err error
			cell, err = tx.AssembleCellView(context.Background(), coord, nil, hexxladb.DefaultAssembleCellViewOpts())
			return err
		})
		return cellDetailMsg{request: request, cell: cell, err: err}
	}
}

func (v *inspectorView) clearContext() {
	v.request++
	v.pack = nil
	v.fovCells = nil
	v.packErr = nil
	v.packLoading = false
}

func (v *inspectorView) loadContext(fov bool) tea.Cmd {
	v.clearContext()
	v.packLoading = true
	v.contextMode = true
	v.scroll = 0
	v.reflow()
	db, coord, request := v.db, v.cell, v.request
	return func() tea.Msg {
		if fov {
			cells, err := loadContextFOV(db, coord)
			if cells == nil && err == nil {
				cells = []hexxladb.CellRecord{}
			}
			return contextLoadedMsg{request: request, visible: cells, err: err}
		}
		pack, err := loadContextPack(db, coord)
		return contextLoadedMsg{request: request, pack: &pack, err: err}
	}
}

func loadContextPack(db *hexxladb.DB, coord hexxladb.Coord) (hexxladb.ContextPack, error) {
	var pack hexxladb.ContextPack
	err := db.View(func(tx *hexxladb.Tx) error {
		var e error
		pack, e = tx.LoadContext(
			context.Background(),
			hexxladb.LoadContextConfig{
				Seeds:    []hexxladb.Coord{coord},
				MaxRing:  3,
				MaxCells: 128,
				Assembly: hexxladb.ContextAssemblyConfig{
					Assemble:         hexxladb.DefaultAssembleCellViewOpts(),
					IncludeSeams:     true,
					SeamRadius:       2,
					FilterSuperseded: true,
					Explain:          true,
				},
			},
		)
		return e
	})
	return pack, err
}

func (v *inspectorView) Help() []string {
	return []string{helpItem("Esc", "back"), helpItem("↑↓", "scroll"), helpItem("d", "details"),
		helpItem("c", "context"), helpItem("f", "FOV"), helpItem("r", "reload"), helpItem("PgUp/PgDn", "page")}
}

func (v *inspectorView) View() string {
	title := styleAccent.Render("Inspect")
	if v.selected {
		title = styleAccent.Render(coordText(v.cell)) + styleDim.Render(" · Details")
		if v.contextMode {
			title = styleAccent.Render(coordText(v.cell)) + styleDim.Render(" · Context")
		}
	}
	end := min(v.scroll+v.pageHeight(), len(v.lines))
	if len(v.lines) > v.pageHeight() {
		title = fitRight(title, styleDim.Render(fmt.Sprintf("%d–%d / %d", v.scroll+1, end, len(v.lines))), v.width)
	}
	return title + "\n" + strings.Join(v.lines[v.scroll:end], "\n")
}

func (v *inspectorView) pageHeight() int { return max(1, v.height-1) }
func (v *inspectorView) maxScroll() int  { return max(0, len(v.lines)-v.pageHeight()) }

func (v *inspectorView) scrollBy(delta int) {
	v.scroll = min(max(0, v.scroll+delta), v.maxScroll())
}

func (v *inspectorView) reflow() {
	width := max(1, v.width)
	content := v.content()
	v.lines = strings.Split(ansi.Wrap(content, width, ""), "\n")
	v.scroll = min(v.scroll, v.maxScroll())
}

func (v *inspectorView) content() string {
	switch {
	case v.cellLoading:
		return styleDim.Render("Loading cell…")
	case v.cellErr != nil:
		return styleBad.Render("Cell error: " + terminalText(v.cellErr.Error()))
	case v.data == nil:
		return styleDim.Render("Press Enter on a cell in Cells or Hex Grid.")
	case v.contextMode:
		return v.contextContent()
	}
	cell := v.data
	meta := fmt.Sprintf("Confidence %.3f", cell.Provenance.Confidence)
	if cell.Provenance.SourceID != "" {
		meta += " · Source " + terminalText(cell.Provenance.SourceID)
	}
	lines := []string{styleDim.Render(meta)}
	if len(cell.Tags) > 0 {
		lines = append(lines, styleDim.Render("Tags ")+terminalText(strings.Join(cell.Tags, " · ")))
	}
	if cell.ClusterHint != nil {
		lines = append(lines, styleDim.Render("Cluster ")+coordText(*cell.ClusterHint))
	}
	lines = append(lines, "", terminalBody(cell.RawContent))
	for _, facet := range cell.Facets {
		lines = append(lines, "", styleAccent.Render(fmt.Sprintf("Facet %d", facet.ID)), terminalBody(facet.DerivedContent))
	}
	return strings.Join(lines, "\n")
}

func (v *inspectorView) contextContent() string {
	switch {
	case v.packLoading:
		return styleDim.Render("Loading context…")
	case v.packErr != nil:
		return styleBad.Render("Context error: " + terminalText(v.packErr.Error()))
	case v.pack != nil:
		return renderContextPack(*v.pack, v.width)
	}
	lines := []string{styleAccent.Render(fmt.Sprintf("FOV context · %d visible cells · radius 3", len(v.fovCells)))}
	for _, cell := range v.fovCells {
		coord, _ := hexxladb.Unpack(cell.Key)
		lines = append(lines, contextRow(coord, cell.Provenance.Confidence, cell.RawContent, v.width))
	}
	return strings.Join(lines, "\n")
}

func renderContextPack(pack hexxladb.ContextPack, width int) string {
	lines := []string{styleAccent.Render(fmt.Sprintf("Radial context · %d cells · %d seams", len(pack.Cells), len(pack.Seams)))}
	if pack.Stats.CandidatesScanned > 0 {
		stats := fmt.Sprintf("%d candidates · radius %d", pack.Stats.CandidatesScanned, pack.Stats.MaxRingUsed)
		if pack.Stats.ResultLimitReached {
			stats += " · result limit reached"
		}
		lines = append(lines, styleDim.Render(stats))
	}
	for _, cell := range pack.Cells {
		row := contextRow(cell.Coord, cell.Provenance.Confidence, cell.RawContent, width)
		if cell.SupersededFrom != nil {
			row += styleDim.Render(" · replaced " + coordText(*cell.SupersededFrom))
		}
		lines = append(lines, row)
	}
	if len(pack.Seams) > 0 {
		lines = append(lines, "", styleAccent.Render("Seams"))
		for _, seam := range pack.Seams {
			lines = append(lines, terminalText(seam.SeamType+" · "+seam.Reason))
		}
	}
	if len(pack.Explanations) > 0 {
		lines = append(lines, "", styleAccent.Render("Assembly decisions"))
		for _, decision := range pack.Explanations {
			row := fmt.Sprintf("%s · ring %d · %s", coordText(decision.Coord), decision.Ring, decision.Reason)
			if decision.SupersededBy != nil {
				row += " → " + coordText(*decision.SupersededBy)
			}
			lines = append(lines, styleDim.Render(row))
		}
	}
	return strings.Join(lines, "\n")
}

func contextRow(coord hexxladb.Coord, confidence float64, content string, width int) string {
	label := fmt.Sprintf("%s %.2f  ", coordText(coord), confidence)
	return styleDim.Render(label) + truncStr(content, width-len(label))
}
