package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

type inspectorView struct {
	noConsume
	db          *hexxladb.DB
	cell        hexxladb.Coord
	data        *hexxladb.CellView
	pack        *hexxladb.ContextPack
	fovCells    []record.CellRecord // FOV-filtered context results
	packLoading bool
	packMode    string // "radial" or "fov"
	width       int
	height      int
}

func newInspectorView(db *hexxladb.DB) view {
	return &inspectorView{db: db}
}

func (v *inspectorView) SetSize(w, h int) { v.width = w; v.height = h }

func (v *inspectorView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case inspectCellMsg:
		v.cell = msg.coord
		v.data = nil
		v.pack = nil
		v.packLoading = false
		return v, nil

	case contextPackLoadedMsg:
		v.packLoading = false
		if msg.err != nil {
			return v, nil
		}
		v.pack = &msg.pack
		return v, nil

	case fovContextLoadedMsg:
		v.packLoading = false
		if msg.err != nil {
			return v, nil
		}
		v.fovCells = msg.cells
		v.pack = nil
		v.packMode = "fov"
		return v, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "c":
			if v.data != nil && !v.packLoading {
				v.packLoading = true
				v.packMode = "radial"
				coord := v.cell
				db := v.db
				return v, func() tea.Msg {
					pack, err := loadContextPack(db, coord)
					return contextPackLoadedMsg{pack: pack, err: err}
				}
			}
		case "f":
			if v.data != nil && !v.packLoading {
				v.packLoading = true
				v.packMode = "fov"
				coord := v.cell
				db := v.db
				return v, func() tea.Msg {
					return loadContextFOV(db, coord)
				}
			}
		case "r":
			v.data = nil
			v.pack = nil
			v.fovCells = nil
			v.packLoading = false
			v.packMode = ""
			return v, nil
		}
	}
	return v, nil
}

func loadContextPack(db *hexxladb.DB, coord hexxladb.Coord) (hexxladb.ContextPack, error) {
	var pack hexxladb.ContextPack
	err := db.View(func(tx *hexxladb.Tx) error {
		var e error
		pack, e = tx.LoadContext(
			context.Background(),
			hexxladb.LoadContextConfig{
				Seeds:     []hexxladb.Coord{coord},
				MaxRing:   3,
				MaxTokens: 8192,
				Assembly: hexxladb.LoadContextBudgetConfig{
					Assemble:          hexxladb.DefaultAssembleCellViewOpts(),
					MaxCandidateCells: 128,
					IncludeSeams:      true,
					SeamRadius:        2,
					FilterSuperseded:  true,
					Explain:           true,
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

	// ── load cell data lazily ────────────────────────────────────────────────
	if v.data == nil {
		pk, err := lattice.Pack(v.cell)
		if err == nil {
			_ = v.db.View(func(tx *hexxladb.Tx) error {
				rec, ok, _ := tx.GetCell(pk)
				if ok {
					cv := hexxladb.CellView{
						Coord:      v.cell,
						RawContent: rec.RawContent,
						Tags:       rec.Tags,
						Provenance: rec.Provenance,
						Validity:   rec.Validity,
					}
					v.data = &cv
				}
				return nil
			})
		}
	}

	if v.data == nil {
		prompt := styleDim.Render("  Navigate to a cell in the Cells tab and press Enter to inspect it.")
		hint := styleDim.Render(fmt.Sprintf("  (currently looking at (%d,%d) — not found)", v.cell.Q, v.cell.R))
		return lipgloss.JoinVertical(lipgloss.Left,
			viewTitle("◈ Cell Inspector", v.width),
			"",
			prompt,
			hint,
		)
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
			parts = append(parts, styleTag.Render(t))
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
				styleCardValue.Render(v.data.RawContent),
		)

	// ── context pack ─────────────────────────────────────────────────────────────
	var packPanel string
	switch {
	case v.packLoading:
		packPanel = styleBorderSubtle.Background(colorBg2).Width(halfW).Padding(0, 2).Render(
			lipgloss.NewStyle().Foreground(colorPurple).Background(colorBg2).Bold(true).Italic(true).Render("⟳  Loading context…"),
		)
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
	if v.pack != nil && len(v.pack.Explanations) > 0 {
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

func (v *inspectorView) renderPack(w int) string {
	p := v.pack
	var sb strings.Builder

	sb.WriteString(styleCardHeader.Render("Context Pack") + "\n")
	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Left,
		styleCardDim.Render("cells: "), lipgloss.NewStyle().Foreground(colorGreen).Background(colorBg2).Render(fmt.Sprintf("%d", len(p.Cells))),
		styleCardDim.Render("  tokens: "), styleCardValue.Render(fmt.Sprintf("%d", p.TotalTokens)),
		styleCardDim.Render("  seams: "), lipgloss.NewStyle().Foreground(colorOrange).Background(colorBg2).Render(fmt.Sprintf("%d", len(p.Seams))),
	) + "\n")

	if p.Stats.CandidatesScanned > 0 {
		sb.WriteString(styleCardDim.Render(fmt.Sprintf(
			"scanned %d, evicted %d, max ring %d",
			p.Stats.CandidatesScanned, p.Stats.CellsEvicted, p.Stats.MaxRingUsed,
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
				lipgloss.NewStyle().Foreground(colorOrange).Background(colorBg2).Render(fmt.Sprintf("  ⋈ %s", s.SeamType)) +
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
		coord, _ := lattice.Unpack(c.Key)
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
		detail := styleCardDim.Render(fmt.Sprintf("ring=%-2d tok=%-4d", ex.Ring, ex.Tokens))
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
