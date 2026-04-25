package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hexxla/hexxladb"
)

type dashboardView struct {
	db     *hexxladb.DB
	width  int
	height int
}

func newDashboardView(db *hexxladb.DB) view {
	return &dashboardView{db: db}
}

func (v *dashboardView) SetSize(w, h int) { v.width = w; v.height = h }

func (v *dashboardView) Update(_ tea.Msg) (view, tea.Cmd) { return v, nil }

func (v *dashboardView) View() string {
	ctx := context.Background()

	stats, _ := v.db.StatsMVCC()

	var cellCount, seamCount int
	var tags []hexxladb.TagCount
	_ = v.db.View(func(tx *hexxladb.Tx) error {
		_ = tx.AscendRange(nil, []byte("cell\xff"), func(_, _ []byte) bool {
			cellCount++
			return cellCount < 10000
		})
		_ = tx.AscendRange(nil, []byte("seam\xff"), func(_, _ []byte) bool {
			seamCount++
			return seamCount < 10000
		})
		var err error
		tags, err = tx.TagCounts(ctx)
		return err
	})

	w := max(40, v.width-6)
	colW := max(20, (w-4)/2)

	// ── title ───────────────────────────────────────────────────────────────
	title := lipgloss.NewStyle().
		Foreground(colorPurple).
		Bold(true).
		Render("◈ HexxlaDB Explorer")
	subtitle := styleDim.Render("  Spatial LLM Memory Database")

	// ── stat cards ──────────────────────────────────────────────────────────
	mvccStr := styleGood.Render("enabled")
	if stats.CommitSeq == 0 {
		mvccStr = styleDim.Render("disabled")
	}

	statCard := func(label, value string, clr lipgloss.Color) string {
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clr).
			Background(colorBg2).
			Padding(0, 2).
			Width(colW).
			Render(
				styleDim.Render(label) + "\n" +
					lipgloss.NewStyle().Foreground(clr).Bold(true).Render(value),
			)
	}

	row1 := lipgloss.JoinHorizontal(lipgloss.Top,
		statCard("CELLS", fmt.Sprintf("%d", cellCount), colorCyan),
		"  ",
		statCard("SEAMS", fmt.Sprintf("%d", seamCount), colorOrange),
		"  ",
		statCard("COMMIT SEQ", fmt.Sprintf("%d", stats.CommitSeq), colorPurple),
		"  ",
		statCard("MVCC", mvccStr, colorGreen),
	)

	// ── top tags ────────────────────────────────────────────────────────────
	var tagLines strings.Builder
	maxCount := 1
	shown := tags
	if len(shown) > 12 {
		shown = shown[:12]
	}
	for _, t := range shown {
		if t.Count > maxCount {
			maxCount = t.Count
		}
	}
	for _, t := range shown {
		bar := barGraph(t.Count, maxCount, 20, colorCyan)
		tagLines.WriteString(
			lipgloss.JoinHorizontal(lipgloss.Left,
				lipgloss.NewStyle().Width(22).Foreground(colorYellow).Render(truncStr(t.Tag, 20)),
				bar,
				styleDim.Render(fmt.Sprintf("  %d", t.Count)),
			) + "\n",
		)
	}
	if tagLines.Len() == 0 {
		tagLines.WriteString(styleDim.Render("  no tags yet"))
	}

	tagsBox := styleBorder.Width(w/2-2).Padding(0, 1).Render(
		styleSectionHeader.Render("Top Tags") + "\n" + tagLines.String(),
	)

	// ── keybindings card ────────────────────────────────────────────────────
	bindings := []struct{ key, desc string }{
		{"Tab / ←→", "cycle tabs"},
		{"1-6", "jump to tab"},
		{"↑↓", "navigate list"},
		{"Enter", "inspect cell"},
		{"c", "load context pack (Inspector)"},
		{"r", "refresh"},
		{"q / Esc", "quit"},
	}
	var kbLines strings.Builder
	for _, b := range bindings {
		kbLines.WriteString(
			lipgloss.JoinHorizontal(lipgloss.Left,
				styleKey.Width(18).Render(b.key),
				styleDim.Render(b.desc),
			) + "\n",
		)
	}
	kbBox := styleBorderSubtle.Width(w/2-2).Padding(0, 1).Render(
		styleSectionHeader.Render("Keybindings") + "\n" + kbLines.String(),
	)

	infoRow := lipgloss.JoinHorizontal(lipgloss.Top, tagsBox, "  ", kbBox)

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		subtitle,
		"",
		row1,
		"",
		infoRow,
	)
}
