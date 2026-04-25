package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hexxla/hexxladb"
)

type analyticsView struct {
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
	case tea.KeyMsg:
		if msg.String() == "r" {
			v.loaded = false
			v.data = nil
			cmd := v.loadCmd()
			return v, cmd
		}
	}
	if !v.loaded {
		cmd := v.loadCmd()
		return v, cmd
	}
	return v, nil
}

func (v *analyticsView) loadCmd() tea.Cmd {
	db := v.db
	return tea.Tick(time.Millisecond, func(_ time.Time) tea.Msg {
		ctx := context.Background()
		var d analyticsLoadedMsg
		var err error
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
			_ = tx.AscendRange(nil, []byte("cell\xff"), func(_, _ []byte) bool {
				d.cellCount++
				return d.cellCount < 100000
			})
			_ = tx.AscendRange(nil, []byte("seam\xff"), func(_, _ []byte) bool {
				d.seamCount++
				return d.seamCount < 100000
			})
			return nil
		})
		d.mvccStats, _ = db.StatsMVCC()
		return d
	})
}

func (v *analyticsView) View() string {
	if !v.loaded {
		return styleLoading.Render("  ⟳  Loading analytics…")
	}
	d := v.data
	w := max(40, v.width-6)
	colW := max(20, (w-4)/2)

	// ── MVCC stats card ───────────────────────────────────────────────────────
	mvccCard := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPurple).
		Background(colorBg2).
		Padding(0, 2).
		Width(colW).
		Render(
			styleSectionHeader.Render("MVCC") + "\n" +
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
		occupied := barGraph(rd.Occupied, rd.Total, 16, colorCyan)
		empty := barGraph(rd.Total-rd.Occupied, rd.Total, 16, colorText2)
		_ = empty
		ringLines.WriteString(
			lipgloss.JoinHorizontal(lipgloss.Left,
				styleDim.Render(fmt.Sprintf("  ring %d  ", rd.Ring)),
				occupied,
				barGraph(rd.Total-rd.Occupied, rd.Total, 16, colorBg3),
				styleDim.Render(fmt.Sprintf("  %d/%d (%d%%)", rd.Occupied, rd.Total, pct)),
			) + "\n",
		)
	}
	ringCard := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorCyan).
		Background(colorBg2).
		Padding(0, 1).
		Width(colW).
		Render(styleSectionHeader.Render("Ring Density (origin r=5)") + "\n" + ringLines.String())

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
		bar := barGraph(t.Count, maxCount, 24, clr)
		tagLines.WriteString(
			lipgloss.JoinHorizontal(lipgloss.Left,
				lipgloss.NewStyle().Width(24).Foreground(colorYellow).Render(truncStr(t.Tag, 22)),
				bar,
				styleDim.Render(fmt.Sprintf("  %d", t.Count)),
			) + "\n",
		)
	}
	tagCard := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorYellow).
		Background(colorBg2).
		Padding(0, 2).
		Width(colW).
		Render(styleSectionHeader.Render("Tag Counts (top 15)") + "\n" + tagLines.String())

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
				styleDim.Render(" + "),
				styleTag.Render(truncStr(p.B, 12)),
				styleDim.Render(fmt.Sprintf("  %d", p.Count)),
			) + "\n",
		)
	}
	if pairLines.Len() == 0 {
		pairLines.WriteString(styleDim.Render("  (need ≥2 co-occurrences)"))
	}
	pairCard := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorOrange).
		Background(colorBg2).
		Padding(0, 2).
		Width(colW).
		Render(styleSectionHeader.Render("Tag Co-occurrences (min 2)") + "\n" + pairLines.String())

	midRow := lipgloss.JoinHorizontal(lipgloss.Top, tagCard, "  ", pairCard)

	help := helpItem("r", "refresh")

	return lipgloss.JoinVertical(lipgloss.Left,
		styleViewTitle.Render("◈ Analytics"),
		"",
		topRow,
		"",
		midRow,
		"",
		styleHelp.Render("  "+help),
	)
}

func kvLine(label, value string) string {
	return lipgloss.JoinHorizontal(lipgloss.Left,
		styleKey.Width(14).Render(label+":"),
		styleValue.Render(value),
	)
}
