package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
)

type inspectorView struct {
	db          *hexxladb.DB
	cell        hexxladb.Coord
	data        *hexxladb.CellView
	pack        *hexxladb.ContextPack
	packLoading bool
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

	case tea.KeyMsg:
		switch msg.String() {
		case "c":
			if v.data != nil && !v.packLoading {
				v.packLoading = true
				coord := v.cell
				db := v.db
				return v, tea.Tick(time.Millisecond, func(_ time.Time) tea.Msg {
					pack, err := loadContextPack(db, coord)
					return contextPackLoadedMsg{pack: pack, err: err}
				})
			}
		case "r":
			v.data = nil
			v.pack = nil
			v.packLoading = false
			return v, nil
		}
	}
	return v, nil
}

func loadContextPack(db *hexxladb.DB, coord hexxladb.Coord) (hexxladb.ContextPack, error) {
	var pack hexxladb.ContextPack
	err := db.View(func(tx *hexxladb.Tx) error {
		var e error
		pack, e = tx.LoadContextPack(
			context.Background(),
			coord, 3, 8192,
			hexxladb.ByteLenBudgeter{},
			hexxladb.LoadContextBudgetConfig{
				Assemble:          hexxladb.DefaultAssembleCellViewOpts(),
				MaxCandidateCells: 128,
				IncludeSeams:      true,
				SeamRadius:        2,
				FilterSuperseded:  true,
				Explain:           true,
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
			styleKey.Width(14).Render(label+":"),
			styleValue.Render(value),
		)
	}

	tagsStr := strings.Join(v.data.Tags, "  ")
	if tagsStr == "" {
		tagsStr = styleDim.Render("(none)")
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
			styleSectionHeader.Render("Cell") + "\n" +
				kv("Coord", fmt.Sprintf("(%d,%d)", v.data.Coord.Q, v.data.Coord.R)) + "\n" +
				kv("Confidence", fmt.Sprintf("%.3f", v.data.Provenance.Confidence)) + "\n" +
				kv("Source", truncStr(v.data.Provenance.SourceID, halfW-18)) + "\n" +
				"" + "\n" +
				styleDim.Render("Tags") + "\n" +
				tagsStr + "\n\n" +
				styleDim.Render("Content") + "\n" +
				styleValue.Render(v.data.RawContent),
		)

	// ── context pack ─────────────────────────────────────────────────────────
	var packPanel string
	switch {
	case v.packLoading:
		packPanel = styleBorderSubtle.Width(halfW).Padding(0, 2).Render(
			styleLoading.Render("⟳  Loading context pack…"),
		)
	case v.pack == nil:
		packPanel = styleBorderSubtle.Width(halfW).Padding(0, 2).Render(
			styleSectionHeader.Render("Context Pack") + "\n" +
				styleDim.Render("Press ") + styleHelpKey.Render("c") + styleDim.Render(" to assemble context pack\n") +
				styleDim.Render("(radius 3, 8KB budget, seam-aware)"),
		)
	default:
		packPanel = v.renderPack(halfW)
	}

	top := lipgloss.JoinHorizontal(lipgloss.Top, cellDetail, "  ", packPanel)

	// ── explain panel ─────────────────────────────────────────────────────────
	var explainPanel string
	if v.pack != nil && len(v.pack.Explanations) > 0 {
		explainPanel = v.renderExplain(w)
	}

	help := helpItem("c", "load context pack") + "  " + helpItem("r", "reset")

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

	sb.WriteString(styleSectionHeader.Render("Context Pack") + "\n")
	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Left,
		styleDim.Render("cells: "), styleGood.Render(fmt.Sprintf("%d", len(p.Cells))),
		styleDim.Render("  tokens: "), styleValue.Render(fmt.Sprintf("%d", p.TotalTokens)),
		styleDim.Render("  seams: "), styleWarn.Render(fmt.Sprintf("%d", len(p.Seams))),
	) + "\n")

	if p.Stats.CandidatesScanned > 0 {
		sb.WriteString(styleDim.Render(fmt.Sprintf(
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
		prefix := "  "
		clr := colorText0
		if c.SupersededFrom != nil {
			prefix = lipgloss.NewStyle().Foreground(colorPink).Render(" ↺")
			clr = colorCyan
		}
		bar := barGraph(int(c.Provenance.Confidence*100), int(maxConf*100), 8, colorGreen)
		line := lipgloss.JoinHorizontal(lipgloss.Left,
			prefix,
			lipgloss.NewStyle().Foreground(colorText2).Width(10).Render(
				fmt.Sprintf("(%d,%d)", c.Coord.Q, c.Coord.R),
			),
			bar,
			"  ",
			lipgloss.NewStyle().Foreground(clr).Render(truncStr(c.RawContent, w-32)),
		)
		if c.SupersededFrom != nil {
			line += "\n" + stylePink.Render(
				fmt.Sprintf("    ↳ superseded (%d,%d)", c.SupersededFrom.Q, c.SupersededFrom.R),
			)
		}
		sb.WriteString(line + "\n")
	}

	if len(p.Seams) > 0 {
		sb.WriteString("\n" + styleSectionHeader.Render("Seams in Pack") + "\n")
		for _, s := range p.Seams {
			sb.WriteString(styleWarn.Render(fmt.Sprintf("  ⋈ %s", s.SeamType)) +
				styleDim.Render(fmt.Sprintf("  %s", truncStr(s.Reason, w-24))) + "\n")
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

func (v *inspectorView) renderExplain(w int) string {
	var sb strings.Builder
	sb.WriteString(styleSectionHeader.Render("Assembly Decisions") + "\n")
	for _, ex := range v.pack.Explanations {
		var marker, clr string
		switch ex.Reason {
		case "included":
			marker = styleGood.Render("✓")
			clr = ""
		case "superseded":
			marker = stylePink.Render("↺")
			clr = ""
		default:
			marker = styleBad.Render("✗")
		}
		detail := styleDim.Render(fmt.Sprintf("ring=%-2d tok=%-4d", ex.Ring, ex.Tokens))
		reason := lipgloss.NewStyle().Foreground(func() lipgloss.Color {
			switch ex.Reason {
			case "included":
				return colorGreen
			case "superseded":
				return colorPink
			default:
				return colorRed
			}
		}()).Render(ex.Reason)
		_ = clr
		extra := ""
		if ex.SupersededBy != nil {
			extra = styleDim.Render(fmt.Sprintf(" → (%d,%d)", ex.SupersededBy.Q, ex.SupersededBy.R))
		}
		fmt.Fprintf(&sb, "  %s  (%d,%d) %s  %s%s\n",
			marker,
			ex.Coord.Q, ex.Coord.R,
			detail,
			reason,
			extra,
		)
	}
	return styleBorderSubtle.Width(w).Padding(0, 1).Render(sb.String())
}
