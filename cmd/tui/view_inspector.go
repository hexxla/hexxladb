package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
}

func newInspectorView(db *hexxladb.DB) view {
	return &inspectorView{db: db}
}

func (v *inspectorView) SetSize(w, _ int) { v.width = w }

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
		}
	case contextLoadedMsg:
		if msg.request == v.request {
			v.packLoading = false
			v.packErr = msg.err
			v.pack = msg.pack
			v.fovCells = msg.visible
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "c", "f":
			if v.data != nil {
				cmd := v.loadContext(msg.String() == "f")
				return v, cmd
			}
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
	v.clearContext()
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

func (v *inspectorView) View() string {
	w := max(40, v.width-6)
	halfW := w/2 - 2

	if v.data == nil {
		prompt := styleDim.Render("  Press Enter on a cell in Cells or Hex Grid to inspect it.")
		switch {
		case v.cellLoading:
			prompt = styleLoading.Render("  ⟳  Loading cell…")
		case v.cellErr != nil:
			prompt = styleBad.Render(fmt.Sprintf("  Cell (%d,%d): %v", v.cell.Q, v.cell.R, v.cellErr))
		}
		return lipgloss.JoinVertical(lipgloss.Left, viewTitle("◈ Cell Inspector", v.width), "", prompt)
	}

	// ── cell detail card ─────────────────────────────────────────────────────
	kv := func(label, value string) string {
		return lipgloss.JoinHorizontal(lipgloss.Left,
			styleCardKey.Width(14).Render(label+":"),
			styleCardValue.Render(value),
		)
	}

	tagsStr := strings.Join(v.data.Tags, "  ")
	if tagsStr == "" {
		tagsStr = styleCardDim.Render("(none)")
	} else {
		var parts []string
		for _, t := range v.data.Tags {
			parts = append(parts, styleTag.Render(terminalText(t)))
		}
		tagsStr = strings.Join(parts, " ")
	}

	cellDetail := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorCyan).
		Background(colorBg2).
		Padding(0, 2).
		Width(halfW).
		Render(
			styleCardHeader.Render("Cell") + "\n" +
				kv("Coord", fmt.Sprintf("(%d,%d)", v.data.Coord.Q, v.data.Coord.R)) + "\n" +
				kv("Confidence", fmt.Sprintf("%.3f", v.data.Provenance.Confidence)) + "\n" +
				kv("Source", truncStr(v.data.Provenance.SourceID, halfW-18)) + "\n" +
				"\n" +
				styleCardDim.Render("Tags") + "\n" +
				tagsStr + "\n\n" +
				styleCardDim.Render("Content") + "\n" +
				styleCardValue.Render(terminalBody(v.data.RawContent)) + v.renderEnrichment(),
		)

	// ── context pack ─────────────────────────────────────────────────────────────
	var packPanel string
	switch {
	case v.packLoading:
		packPanel = styleBorderSubtle.Background(colorBg2).Width(halfW).Padding(0, 2).Render(
			lipgloss.NewStyle().Foreground(colorPurple).Background(colorBg2).Bold(true).Italic(true).Render("⟳  Loading context…"),
		)
	case v.packErr != nil:
		packPanel = styleBad.Render(fmt.Sprintf("  Context error: %v", v.packErr))
	case v.pack == nil && v.fovCells == nil:
		packPanel = styleBorderSubtle.Background(colorBg2).Width(halfW).Padding(0, 2).Render(
			styleCardHeader.Render("Context Loading") + "\n" +
				styleCardDim.Render("Press ") + lipgloss.NewStyle().Foreground(colorCyan).Background(colorBg2).Bold(true).Render("c") + styleCardDim.Render(" radial context pack\n") +
				styleCardDim.Render("Press ") + lipgloss.NewStyle().Foreground(colorGreen).Background(colorBg2).Bold(true).Render("f") + styleCardDim.Render(" FOV-filtered context\n") +
				styleCardDim.Render("(radius 3, seam-aware, visibility-filtered)"),
		)
	case v.fovCells != nil:
		packPanel = v.renderFOV(halfW)
	default:
		packPanel = v.renderPack(halfW)
	}

	top := lipgloss.JoinHorizontal(lipgloss.Top, cellDetail, "  ", packPanel)

	// ── explain panel ─────────────────────────────────────────────────────────
	var explainPanel string
	if v.packErr == nil && v.pack != nil && len(v.pack.Explanations) > 0 {
		explainPanel = v.renderExplain(w)
	}

	help := helpItem("c", "radial context") + "  " + helpItem("f", "FOV context") + "  " + helpItem("r", "reset")

	parts := []string{
		viewTitle("◈ Cell Inspector", v.width),
		"",
		top,
	}
	if explainPanel != "" {
		parts = append(parts, "", explainPanel)
	}
	parts = append(parts, "", styleHelp.Render("  "+help))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (v *inspectorView) renderEnrichment() string {
	var details strings.Builder
	if v.data.ClusterHint != nil {
		fmt.Fprintf(&details, "\n\nCluster hint (%d,%d)", v.data.ClusterHint.Q, v.data.ClusterHint.R)
	}
	for _, facet := range v.data.Facets {
		fmt.Fprintf(&details, "\n\nFacet %d: %s", facet.ID, terminalBody(facet.DerivedContent))
	}
	return details.String()
}

func (v *inspectorView) renderPack(w int) string {
	p := v.pack
	var sb strings.Builder

	sb.WriteString(styleCardHeader.Render("Context Pack") + "\n")
	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Left,
		styleCardDim.Render("cells: "), lipgloss.NewStyle().Foreground(colorGreen).Background(colorBg2).Render(fmt.Sprintf("%d", len(p.Cells))),
		styleCardDim.Render("  seams: "), lipgloss.NewStyle().Foreground(colorOrange).Background(colorBg2).Render(fmt.Sprintf("%d", len(p.Seams))),
	) + "\n")

	if p.Stats.CandidatesScanned > 0 {
		sb.WriteString(styleCardDim.Render(fmt.Sprintf(
			"scanned %d, limit reached %t, max ring %d",
			p.Stats.CandidatesScanned, p.Stats.ResultLimitReached, p.Stats.MaxRingUsed,
		)) + "\n")
	}
	sb.WriteString("\n")

	maxConf := 0.0
	for _, c := range p.Cells {
		if c.Provenance.Confidence > maxConf {
			maxConf = c.Provenance.Confidence
		}
	}
	if maxConf == 0 {
		maxConf = 1
	}

	for _, c := range p.Cells {
		prefix := styleCardDim.Render("  ")
		clr := colorText0
		if c.SupersededFrom != nil {
			prefix = lipgloss.NewStyle().Foreground(colorPink).Background(colorBg2).Render(" ↺")
			clr = colorCyan
		}
		bar := barGraphBg(int(c.Provenance.Confidence*100), int(maxConf*100), 8, colorGreen, colorBg2)
		line := lipgloss.JoinHorizontal(lipgloss.Left,
			prefix,
			lipgloss.NewStyle().Foreground(colorText2).Background(colorBg2).Width(10).Render(
				fmt.Sprintf("(%d,%d)", c.Coord.Q, c.Coord.R),
			),
			bar,
			styleCardDim.Render("  "),
			lipgloss.NewStyle().Foreground(clr).Background(colorBg2).Render(truncStr(c.RawContent, w-32)),
		)
		if c.SupersededFrom != nil {
			line += "\n" + lipgloss.NewStyle().Foreground(colorPink).Background(colorBg2).Render(
				fmt.Sprintf("    ↳ superseded (%d,%d)", c.SupersededFrom.Q, c.SupersededFrom.R),
			)
		}
		sb.WriteString(line + "\n")
	}

	if len(p.Seams) > 0 {
		sb.WriteString("\n" + styleCardHeader.Render("Seams in Pack") + "\n")
		for _, s := range p.Seams {
			sb.WriteString(
				lipgloss.NewStyle().Foreground(colorOrange).Background(colorBg2).Render(fmt.Sprintf("  ⋈ %s", terminalText(s.SeamType))) +
					styleCardDim.Render(fmt.Sprintf("  %s", truncStr(s.Reason, w-24))) + "\n")
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPurple).
		Background(colorBg2).
		Padding(0, 1).
		Width(w).
		Render(sb.String())
}

func (v *inspectorView) renderFOV(w int) string {
	cells := v.fovCells
	var sb strings.Builder

	sb.WriteString(styleCardHeader.Render("FOV Context (visibility-filtered)") + "\n")
	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Left,
		styleCardDim.Render("visible cells: "), lipgloss.NewStyle().Foreground(colorGreen).Background(colorBg2).Render(fmt.Sprintf("%d", len(cells))),
		styleCardDim.Render("  center: "), styleCardValue.Render(fmt.Sprintf("(%d,%d)", v.cell.Q, v.cell.R)),
		styleCardDim.Render("  radius: "), styleCardValue.Render("3"),
	) + "\n")
	sb.WriteString(styleCardDim.Render("empty cells block line-of-sight") + "\n\n")

	maxConf := 0.0
	for _, c := range cells {
		if c.Provenance.Confidence > maxConf {
			maxConf = c.Provenance.Confidence
		}
	}
	if maxConf == 0 {
		maxConf = 1
	}

	for _, c := range cells {
		coord, _ := hexxladb.Unpack(c.Key)
		dist := v.cell.Distance(coord)
		bar := barGraphBg(int(c.Provenance.Confidence*100), int(maxConf*100), 8, colorGreen, colorBg2)
		line := lipgloss.JoinHorizontal(lipgloss.Left,
			lipgloss.NewStyle().Foreground(colorText2).Background(colorBg2).Width(10).Render(
				fmt.Sprintf("(%d,%d)", coord.Q, coord.R),
			),
			lipgloss.NewStyle().Foreground(colorPurple).Background(colorBg2).Width(4).Render(
				fmt.Sprintf("d%d", dist),
			),
			bar,
			styleCardDim.Render("  "),
			lipgloss.NewStyle().Foreground(colorText0).Background(colorBg2).Render(truncStr(c.RawContent, w-36)),
		)
		sb.WriteString(line + "\n")
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorGreen).
		Background(colorBg2).
		Padding(0, 1).
		Width(w).
		Render(sb.String())
}

func (v *inspectorView) renderExplain(w int) string {
	var sb strings.Builder
	sb.WriteString(styleCardHeader.Render("Assembly Decisions") + "\n")
	for _, ex := range v.pack.Explanations {
		var marker string
		switch ex.Reason {
		case "included":
			marker = lipgloss.NewStyle().Foreground(colorGreen).Background(colorBg2).Render("✓")
		case "superseded":
			marker = lipgloss.NewStyle().Foreground(colorPink).Background(colorBg2).Render("↺")
		default:
			marker = lipgloss.NewStyle().Foreground(colorRed).Background(colorBg2).Render("✗")
		}
		detail := styleCardDim.Render(fmt.Sprintf("ring=%-2d", ex.Ring))
		reason := lipgloss.NewStyle().Foreground(func() lipgloss.Color {
			switch ex.Reason {
			case "included":
				return colorGreen
			case "superseded":
				return colorPink
			default:
				return colorRed
			}
		}()).Background(colorBg2).Render(ex.Reason)
		extra := ""
		if ex.SupersededBy != nil {
			extra = styleCardDim.Render(fmt.Sprintf(" → (%d,%d)", ex.SupersededBy.Q, ex.SupersededBy.R))
		}
		fmt.Fprintf(&sb, "  %s  (%d,%d) %s  %s%s\n",
			marker,
			ex.Coord.Q, ex.Coord.R,
			detail,
			reason,
			extra,
		)
	}
	return styleBorderSubtle.Background(colorBg2).Width(w).Padding(0, 1).Render(sb.String())
}
