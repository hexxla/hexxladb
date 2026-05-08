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

type dashboardView struct {
	noConsume
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
	cellCount := int(stats.LogicalCells)
	pageSize := v.db.PageSize()
	maxVal := v.db.MaxValueBytes()
	embedDim := v.db.EmbeddingDimension()

	var seamCount int
	var tags []hexxladb.TagCount
	_ = v.db.View(func(tx *hexxladb.Tx) error {
		_ = tx.AscendRange([]byte(index.SeamPrefix), index.SeamScanUpperBound(), func(_, _ []byte) bool {
			seamCount++
			return seamCount < 100000
		})
		var err error
		tags, err = tx.TagCounts(ctx)
		return err
	})
	w := contentWidth(v.width - 6)
	leftW, rightW := twoColumnWidths(w)

	// Reserve space for title(2), status(1), help(1) = 4 lines
	maxContentH := max(5, v.height-4)

	// ── title ───────────────────────────────────────────────────────────────
	title := viewTitle("◈ HexxlaDB Explorer", w)
	subtitle := Subtle.Render("  Spatial LLM Memory Database")

	// ── stat cards ──────────────────────────────────────────────────────────
	mvccEnabled := stats.CommitSeq > 0

	statCard := func(label, value string, clr lipgloss.Color) string {
		colW := max(20, (w-4)/4)
		return Box.BorderForeground(clr).Width(colW).Render(
			Dim.Render(label) + "\n" +
				lipgloss.NewStyle().Foreground(clr).Background(colorBg2).Bold(true).Render(value),
		)
	}

	embedStr := "none yet"
	embedClr := colorText2
	if embedDim > 0 {
		embedStr = fmt.Sprintf("%dd", embedDim)
		embedClr = colorPink
	}

	row1 := lipgloss.JoinHorizontal(lipgloss.Top,
		statCard("CELLS", fmt.Sprintf("%d", cellCount), colorCyan),
		" ",
		statCard("SEAMS", fmt.Sprintf("%d", seamCount), colorOrange),
		" ",
		statCard("COMMIT SEQ", fmt.Sprintf("%d", stats.CommitSeq), colorPurple),
		" ",
		statCard("MVCC", func() string {
			if mvccEnabled {
				return "enabled"
			}
			return "disabled"
		}(), colorGreen),
	)

	row2 := lipgloss.JoinHorizontal(lipgloss.Top,
		statCard("PAGE SIZE", fmt.Sprintf("%d B", pageSize), colorText1),
		" ",
		statCard("MAX VALUE", fmt.Sprintf("%d B", maxVal), colorText1),
		" ",
		statCard("EMBEDDINGS", embedStr, embedClr),
		" ",
		statCard("VERSIONED", fmt.Sprintf("%d", stats.VersionedRows), colorText2),
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
		bar := barGraphBg(t.Count, maxCount, 20, colorCyan, colorBg2)
		tagLines.WriteString(
			lipgloss.JoinHorizontal(lipgloss.Left,
				lipgloss.NewStyle().Width(22).Foreground(colorYellow).Background(colorBg2).Render(truncStr(t.Tag, 20)),
				bar,
				Dim.Render(fmt.Sprintf("  %d", t.Count)),
			) + "\n",
		)
	}
	if tagLines.Len() == 0 {
		tagLines.WriteString(Dim.Render("  no tags yet"))
	}

	tagsBox := Box.Width(leftW).Render(
		Section.Render("Top Tags") + "\n" + tagLines.String(),
	)

	// ── keybindings card ────────────────────────────────────────────────────
	bindings := []struct{ key, desc string }{
		{"Tab / Shift+Tab", "cycle tabs"},
		{"1-9", "jump to tab"},
		{"↑↓/jk", "navigate list"},
		{"Enter", "inspect cell"},
		{"/", "search cells (Cells tab)"},
		{"e", "cycle: lexical → semantic → hybrid"},
		{"c", "radial context pack (Inspector)"},
		{"f", "FOV context (Inspector)"},
		{"v", "toggle FOV overlay (Hex Grid)"},
		{"r", "refresh current tab"},
		{"q / Ctrl+C", "quit"},
	}
	var kbLines strings.Builder
	for _, b := range bindings {
		kbLines.WriteString(
			lipgloss.JoinHorizontal(lipgloss.Left,
				MetricLabel.Width(18).Render(b.key),
				Dim.Render(b.desc),
			) + "\n",
		)
	}
	kbBox := Box.BorderForeground(colorText2).Width(rightW).Render(
		Section.Render("Keybindings") + "\n" + kbLines.String(),
	)

	infoRow := lipgloss.JoinHorizontal(lipgloss.Top, tagsBox, " ", kbBox)

	// Build full content then clip to max height
	fullContent := lipgloss.JoinVertical(lipgloss.Left,
		title,
		subtitle,
		"",
		row1,
		"",
		row2,
		"",
		infoRow,
	)
	clippedContent := lipgloss.NewStyle().MaxHeight(maxContentH).Render(fullContent)

	return clippedContent
}
