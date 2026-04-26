package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hexxla/hexxladb"
)

type healthView struct {
	noConsume
	db     *hexxladb.DB
	report *hexxladb.HealthReport
	err    error
	loaded bool
	width  int
	height int
}

func newHealthView(db *hexxladb.DB) view {
	return &healthView{db: db}
}

func (v *healthView) SetSize(w, h int) { v.width = w; v.height = h }

func (v *healthView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case healthLoadedMsg:
		v.report = msg.report
		v.err = msg.err
		v.loaded = true
		return v, nil
	case tabActivatedMsg:
		v.loaded = false
		v.report = nil
		v.err = nil
		cmd := v.loadCmd()
		return v, cmd
	case tea.KeyMsg:
		if msg.String() == "r" {
			v.loaded = false
			v.report = nil
			v.err = nil
			cmd := v.loadCmd()
			return v, cmd
		}
	}
	return v, nil
}

func (v *healthView) loadCmd() tea.Cmd {
	db := v.db
	return func() tea.Msg {
		cfg := hexxladb.DefaultHealthCheckConfig()
		report, err := db.HealthCheck(context.Background(), cfg)
		return healthLoadedMsg{report: &report, err: err}
	}
}

func (v *healthView) View() string {
	if !v.loaded {
		return styleLoading.Render("  ⟳  Running health check…")
	}
	if v.err != nil {
		return styleBad.Render(fmt.Sprintf("  ✗  Health check error: %v", v.err))
	}

	r := v.report
	w := max(40, v.width-6)
	colW := max(20, (w-4)/2)

	// ── summary cards ────────────────────────────────────────────────────────
	overallOK := r.TagIndexErrors == 0 && r.SourceIndexErrors == 0 && len(r.OrphanedSeams) == 0
	statusStr, statusClr := styleGood.Render("● HEALTHY"), colorGreen
	if !overallOK {
		statusStr, statusClr = styleWarn.Render("● ISSUES FOUND"), colorOrange
	}
	_ = statusClr

	statCard := func(label, value string, clr lipgloss.Color) string {
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clr).
			Background(colorBg2).
			Padding(0, 2).
			Width(colW/2 - 1).
			Render(styleDim.Render(label) + "\n" +
				lipgloss.NewStyle().Foreground(clr).Background(colorBg2).Bold(true).Render(value))
	}

	row1 := lipgloss.JoinHorizontal(lipgloss.Top,
		statCard("CELLS", fmt.Sprintf("%d", r.CellCount), colorCyan),
		" ",
		statCard("SEAMS", fmt.Sprintf("%d", r.SeamCount), colorOrange),
		" ",
		statCard("RESOLVED", fmt.Sprintf("%d", r.SeamsResolved), colorGreen),
		" ",
		statCard("UNRESOLVED", fmt.Sprintf("%d", r.SeamsUnresolved), func() lipgloss.Color {
			if r.SeamsUnresolved > 0 {
				return colorOrange
			}
			return colorGreen
		}()),
	)

	errClr := colorGreen
	if r.TagIndexErrors+r.SourceIndexErrors > 0 {
		errClr = colorRed
	}
	orphClr := colorGreen
	if len(r.OrphanedSeams) > 0 {
		orphClr = colorOrange
	}
	row2 := lipgloss.JoinHorizontal(lipgloss.Top,
		statCard("TAG ERRORS", fmt.Sprintf("%d", r.TagIndexErrors), errClr),
		" ",
		statCard("SRC ERRORS", fmt.Sprintf("%d", r.SourceIndexErrors), errClr),
		" ",
		statCard("ORPHANED", fmt.Sprintf("%d", len(r.OrphanedSeams)), orphClr),
		" ",
		statCard("MVCC SEQ", fmt.Sprintf("%d", r.MVCCStats.CommitSeq), colorPurple),
	)

	// ── MVCC detail ──────────────────────────────────────────────────────────
	mvccDetail := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPurple).
		Background(colorBg2).
		Padding(0, 2).
		Width(colW).
		Render(
			styleSectionHeader.Render("MVCC Stats") + "\n" +
				kvLine("Commit Seq", fmt.Sprintf("%d", r.MVCCStats.CommitSeq)) + "\n" +
				kvLine("Versioned Rows", fmt.Sprintf("%d", r.MVCCStats.VersionedRows)) + "\n" +
				kvLine("Logical Cells", fmt.Sprintf("%d", r.MVCCStats.LogicalCells)),
		)

	// ── warnings panel ───────────────────────────────────────────────────────
	var warnLines strings.Builder
	shown := r.Warnings
	if len(shown) > 20 {
		shown = shown[:20]
	}
	if len(shown) == 0 {
		warnLines.WriteString(styleGood.Render("  ✓  No warnings"))
	}
	for _, w := range shown {
		warnLines.WriteString(styleWarn.Render("  ⚠  ") + styleDim.Render(truncStr(w, v.width-10)) + "\n")
	}
	if len(r.Warnings) > 20 {
		warnLines.WriteString(styleDim.Render(fmt.Sprintf("  … and %d more", len(r.Warnings)-20)) + "\n")
	}
	warnPanel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorOrange).
		Background(colorBg2).
		Padding(0, 1).
		Width(colW).
		Render(styleSectionHeader.Render("Warnings") + "\n" + warnLines.String())

	midRow := lipgloss.JoinHorizontal(lipgloss.Top, mvccDetail, "  ", warnPanel)

	help := helpItem("r", "re-run health check")

	return lipgloss.JoinVertical(lipgloss.Left,
		viewTitle("◈ Health Check", v.width),
		"  "+statusStr,
		"",
		row1,
		"",
		row2,
		"",
		midRow,
		"",
		styleHelp.Render("  "+help),
	)
}
