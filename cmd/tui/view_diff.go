package main

import (
	"context"
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/hexxla/hexxladb"
)

type diffView struct {
	noConsume
	db      *hexxladb.DB
	diff    *hexxladb.SnapshotDiff
	err     error
	loaded  bool
	loading bool
	cursor  int
	width   int
	height  int
}

func newDiffView(db *hexxladb.DB) view {
	return &diffView{db: db}
}

func (v *diffView) SetSize(w, h int) { v.width = w; v.height = h }

func (v *diffView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case snapshotDiffMsg:
		v.diff = msg.diff
		v.err = msg.err
		v.loaded = true
		v.loading = false
		v.cursor = 0
		return v, nil
	case tabActivatedMsg:
		if !v.loaded && !v.loading {
			cmd := v.loadCmd()
			return v, cmd
		}
		return v, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if v.cursor > 0 {
				v.cursor--
			}
		case "down", "j":
			if v.diff != nil && v.cursor < len(v.diff.Cells)-1 {
				v.cursor++
			}
		case "r":
			if v.loading {
				return v, nil
			}
			v.loaded = false
			v.diff = nil
			v.err = nil
			cmd := v.loadCmd()
			return v, cmd
		}
	}
	return v, nil
}

func (v *diffView) loadCmd() tea.Cmd {
	v.loading = true
	db := v.db
	return func() tea.Msg {
		stats, err := db.StatsMVCC()
		if err != nil {
			return snapshotDiffMsg{err: err}
		}
		fromSeq := stats.CommitSeq - min(stats.CommitSeq, 10)
		diff, err := db.SnapshotDiff(context.Background(), fromSeq, stats.CommitSeq, hexxladb.SnapshotDiffConfig{})
		return snapshotDiffMsg{diff: &diff, err: err}
	}
}

func (v *diffView) View() string {
	if !v.loaded {
		return styleLoading.Render("  ⟳  Computing snapshot diff…")
	}

	w := max(40, v.width-6)

	if v.err != nil {
		if errors.Is(v.err, hexxladb.ErrMVCCRequired) {
			return lipgloss.JoinVertical(lipgloss.Left,
				viewTitle("◈ Snapshot Diff", v.width),
				"",
				styleDim.Render("  Database not opened with MVCC enabled — diff requires EnableMVCC: true."),
				"",
				styleHelp.Render("  "+helpItem("r", "retry")),
			)
		}
		return styleBad.Render(fmt.Sprintf("  ✗  Diff error: %v", v.err))
	}

	d := v.diff
	if d == nil || (len(d.Cells) == 0 && len(d.Seams) == 0) {
		return lipgloss.JoinVertical(lipgloss.Left,
			viewTitle("◈ Snapshot Diff", v.width),
			"",
			styleDim.Render("  No changes detected in last 10 commits."),
			"",
			styleHelp.Render("  "+helpItem("r", "refresh")),
		)
	}

	colW := max(14, (w-6)/2)
	statCard := func(label, val string, clr lipgloss.Color) string {
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clr).
			Background(colorBg2).
			Padding(0, 2).
			Width(colW).
			Render(styleCardDim.Render(label) + "\n" +
				lipgloss.NewStyle().Foreground(clr).Background(colorBg2).Bold(true).Render(val))
	}

	summaryRow := lipgloss.JoinHorizontal(lipgloss.Top,
		statCard("CELL CHANGES", fmt.Sprintf("%d", len(d.Cells)), colorGreen),
		" ",
		statCard("SEAM CHANGES", fmt.Sprintf("%d", len(d.Seams)), colorOrange),
	)

	// ── cell diff table ───────────────────────────────────────────────────────
	visH := max(3, v.height-14)
	start := 0
	if v.cursor >= visH {
		start = v.cursor - visH + 1
	}
	end := min(start+visH, len(d.Cells))

	contentW := max(20, w-32)
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(colorText2)).
		Headers("", "OP", "COORD", "CONTENT").
		Width(w).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return lipgloss.NewStyle().Foreground(colorPurple).Background(colorBg1).Bold(true).Padding(0, 1)
			}
			actualIdx := start + row
			base := lipgloss.NewStyle().Padding(0, 1).Background(colorBg1)
			if actualIdx == v.cursor {
				base = base.Background(colorBg3).Bold(true)
			}
			if actualIdx < len(d.Cells) && d.Cells[actualIdx].Op == hexxladb.DiffOpDelete {
				return base.Foreground(colorOrange)
			}
			return base
		})

	for i := start; i < end; i++ {
		c := d.Cells[i]
		marker := styleDim.Render("  ")
		if i == v.cursor {
			marker = lipgloss.NewStyle().Foreground(colorPurple).Background(colorBg3).Render(" ▶")
		}
		coord := c.Coord
		opStr := string(c.Op)
		content := truncStr(c.Record.RawContent, contentW)
		t = t.Row(marker, opStr, fmt.Sprintf("(%d,%d)", coord.Q, coord.R), content)
	}

	pct := 0
	if len(d.Cells) > 0 {
		pct = (v.cursor + 1) * 100 / len(d.Cells)
	}
	scrollInfo := styleDim.Render(fmt.Sprintf("  %d/%d  (%d%%)", min(v.cursor+1, len(d.Cells)), len(d.Cells), pct))

	help := helpItem("↑↓/jk", "navigate") + "  " + helpItem("r", "refresh")

	return lipgloss.JoinVertical(lipgloss.Left,
		viewTitle("◈ Snapshot Diff  (last 10 commits)", v.width),
		"",
		summaryRow,
		"",
		scrollInfo,
		t.Render(),
		"",
		styleHelp.Render("  "+help),
	)
}
