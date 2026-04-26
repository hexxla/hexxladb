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

	v1Len := len(index.CellPrefix) + index.PackedCoordKeyLen
	mvccLen := v1Len + index.VersionSuffixLen
	seenCells := map[[16]byte]struct{}{}
	var cellCount, seamCount int
	var tags []hexxladb.TagCount
	_ = v.db.View(func(tx *hexxladb.Tx) error {
		_ = tx.AscendRange([]byte(index.CellPrefix), []byte("cell0"), func(k, _ []byte) bool {
			if len(k) != v1Len && len(k) != mvccLen {
				return true
			}
			var key [16]byte
			copy(key[:], k[len(index.CellPrefix):len(index.CellPrefix)+16])
			if _, ok := seenCells[key]; !ok {
				seenCells[key] = struct{}{}
				cellCount++
			}
			return cellCount < 100000
		})
		_ = tx.AscendRange([]byte(index.SeamPrefix), index.SeamScanUpperBound(), func(_, _ []byte) bool {
			seamCount++
			return seamCount < 100000
		})
		var err error
		tags, err = tx.TagCounts(ctx)
		return err
	})

	w := max(40, v.width-6)
	colW := max(20, (w-4)/2)

	// ── title ───────────────────────────────────────────────────────────────
	title := viewTitle("◈ HexxlaDB Explorer", w)
	subtitle := styleDim.Render("  Spatial LLM Memory Database")

	// ── stat cards ──────────────────────────────────────────────────────────
	mvccStr := styleGood.Render("enabled")
	if stats.CommitSeq == 0 {
		mvccStr = styleDim.Render("disabled")
	}

	statCard := func(label, value string, clr lipgloss.Color) string {
		return styleCard.BorderForeground(clr).Width(colW).Render(
			styleDim.Render(label) + "\n" +
				lipgloss.NewStyle().Foreground(clr).Background(colorBg2).Bold(true).Render(value),
		)
	}

	row1 := lipgloss.JoinHorizontal(lipgloss.Top,
		statCard("CELLS", fmt.Sprintf("%d", cellCount), colorCyan),
		" ",
		statCard("SEAMS", fmt.Sprintf("%d", seamCount), colorOrange),
		" ",
		statCard("COMMIT SEQ", fmt.Sprintf("%d", stats.CommitSeq), colorPurple),
		" ",
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

	halfw := (w - 1) / 2
	tagsBox := styleCard.Width(halfw).Render(
		styleSectionHeader.Render("Top Tags") + "\n" + tagLines.String(),
	)

	// ── keybindings card ────────────────────────────────────────────────────
	bindings := []struct{ key, desc string }{
		{"Tab / Shift+Tab", "cycle tabs"},
		{"1-8", "jump to tab"},
		{"↑↓/jk", "navigate list"},
		{"Enter", "inspect cell"},
		{"/", "search cells (Cells tab)"},
		{"c", "context pack (Inspector)"},
		{"r", "refresh current tab"},
		{"q / Ctrl+C", "quit"},
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
	kbBox := styleCard.BorderForeground(colorText2).Width(w - halfw - 1).Render(
		styleSectionHeader.Render("Keybindings") + "\n" + kbLines.String(),
	)

	infoRow := lipgloss.JoinHorizontal(lipgloss.Top, tagsBox, " ", kbBox)

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		subtitle,
		"",
		row1,
		"",
		infoRow,
	)
}
