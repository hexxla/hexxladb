package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
)

type analyticsView struct {
	noConsume
	db     *hexxladb.DB
	data   *analyticsLoadedMsg
	loaded bool
	width  int
	height int
}

func newAnalyticsView(db *hexxladb.DB) view {
	return &analyticsView{db: db}
}

func (v *analyticsView) SetSize(w, h int) { v.width = w; v.height = h }

func (v *analyticsView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case analyticsLoadedMsg:
		v.data = &msg
		v.loaded = true
		return v, nil
	case tabActivatedMsg:
		if !v.loaded {
			cmd := v.loadCmd()
			return v, cmd
		}
		return v, nil
	case tea.KeyMsg:
		if msg.String() == "r" {
			v.loaded = false
			v.data = nil
			cmd := v.loadCmd()
			return v, cmd
		}
	}
	return v, nil
}

func (v *analyticsView) loadCmd() tea.Cmd {
	db := v.db
	return func() tea.Msg {
		ctx := context.Background()
		var d analyticsLoadedMsg
		var err error
		d.mvccStats, _ = db.StatsMVCC()
		d.cellCount = int(d.mvccStats.LogicalCells)
		_ = db.View(func(tx *hexxladb.Tx) error {
			d.tagCounts, err = tx.TagCounts(ctx)
			if err != nil {
				return err
			}
			d.tagPairs, err = tx.TagCooccurrences(ctx, 2)
			if err != nil {
				return err
			}
			center := hexxladb.Coord{}
			d.ringDensity, err = tx.RingDensityMap(ctx, center, 5)
			if err != nil {
				return err
			}
			_ = tx.AscendRange([]byte(index.SeamPrefix), index.SeamScanUpperBound(), func(_, _ []byte) bool {
				d.seamCount++
				return d.seamCount < 100000
			})
			return nil
		})
		return d
	}
}

func (v *analyticsView) View() string {
	if !v.loaded {
		return styleLoading.Render("  ⟳  Loading analytics…")
	}
	d := v.data
	w := contentWidth(v.width - 6)
	leftW, rightW := twoColumnWidths(w)

	// ── MVCC stats card ───────────────────────────────────────────────────────
	mvccCard := Box.BorderForeground(colorPurple).Width(leftW).Render(
		Section.Render("MVCC") + "\n" +
			kvLine("Commit Seq", fmt.Sprintf("%d", d.mvccStats.CommitSeq)) + "\n" +
			kvLine("Cells", fmt.Sprintf("%d", d.cellCount)) + "\n" +
			kvLine("Seams", fmt.Sprintf("%d", d.seamCount)),
	)

	// ── ring density ──────────────────────────────────────────────────────────
	var ringLines strings.Builder
	for _, rd := range d.ringDensity {
		pct := 0
		if rd.Total > 0 {
			pct = rd.Occupied * 100 / rd.Total
		}
		occupied := barGraphBg(rd.Occupied, rd.Total, 16, colorCyan, colorBg2)
		ringLines.WriteString(
			lipgloss.JoinHorizontal(lipgloss.Left,
				Dim.Render(fmt.Sprintf("  ring %d  ", rd.Ring)),
				occupied,
				barGraphBg(rd.Total-rd.Occupied, rd.Total, 16, colorBg3, colorBg2),
				Dim.Render(fmt.Sprintf("  %d/%d (%d%%)", rd.Occupied, rd.Total, pct)),
			) + "\n",
		)
	}
	ringCard := Box.BorderForeground(colorCyan).Width(rightW).Render(
		Section.Render("Ring Density (origin r=5)") + "\n" + ringLines.String())

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, mvccCard, "  ", ringCard)

	// ── tag counts bar chart ───────────────────────────────────────────────────
	var tagLines strings.Builder
	shown := d.tagCounts
	if len(shown) > 15 {
		shown = shown[:15]
	}
	maxCount := 1
	for _, t := range shown {
		if t.Count > maxCount {
			maxCount = t.Count
		}
	}
	tagColors := []lipgloss.Color{colorPurple, colorCyan, colorGreen, colorOrange, colorPink}
	for i, t := range shown {
		clr := tagColors[i%len(tagColors)]
		bar := barGraphBg(t.Count, maxCount, 24, clr, colorBg2)
		tagLines.WriteString(
			lipgloss.JoinHorizontal(lipgloss.Left,
				lipgloss.NewStyle().Width(24).Foreground(colorYellow).Background(colorBg2).Render(truncStr(t.Tag, 22)),
				bar,
				Dim.Render(fmt.Sprintf("  %d", t.Count)),
			) + "\n",
		)
	}
	tagCard := Box.BorderForeground(colorYellow).Width(leftW).Render(
		Section.Render("Tag Counts (top 15)") + "\n" + tagLines.String())

	// ── co-occurrences ─────────────────────────────────────────────────────────
	var pairLines strings.Builder
	pairShown := d.tagPairs
	if len(pairShown) > 10 {
		pairShown = pairShown[:10]
	}
	for _, p := range pairShown {
		pairLines.WriteString(
			lipgloss.JoinHorizontal(lipgloss.Left,
				styleTag.Render(truncStr(p.A, 12)),
				Dim.Render(" + "),
				styleTag.Render(truncStr(p.B, 12)),
				Dim.Render(fmt.Sprintf("  %d", p.Count)),
			) + "\n",
		)
	}
	if pairLines.Len() == 0 {
		pairLines.WriteString(Dim.Render("  (need ≥2 co-occurrences)"))
	}
	pairCard := Box.BorderForeground(colorOrange).Width(rightW).Render(
		Section.Render("Tag Co-occurrences (min 2)") + "\n" + pairLines.String())

	midRow := lipgloss.JoinHorizontal(lipgloss.Top, tagCard, "  ", pairCard)

	help := helpItem("r", "refresh")

	return lipgloss.JoinVertical(lipgloss.Left,
		viewTitle("◈ Analytics", v.width),
		"",
		topRow,
		"",
		midRow,
		"",
		Help.Render("  "+help),
	)
}

func kvLine(label, value string) string {
	return lipgloss.JoinHorizontal(lipgloss.Left,
		MetricLabel.Width(14).Render(label+":"),
		Dim.Render(value),
	)
}
