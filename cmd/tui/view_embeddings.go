package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/inspect"
)

type embeddingsView struct {
	noConsume
	db     *hexxladb.DB
	data   *embeddingsLoadedMsg
	loaded bool
	width  int
	height int
}

func newEmbeddingsView(db *hexxladb.DB) view {
	return &embeddingsView{db: db}
}

func (v *embeddingsView) SetSize(w, h int) { v.width = w; v.height = h }

func (v *embeddingsView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case embeddingsLoadedMsg:
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

func (v *embeddingsView) loadCmd() tea.Cmd {
	db := v.db
	return func() tea.Msg {
		var msg embeddingsLoadedMsg
		msg.dimension = db.EmbeddingDimension()
		msg.metric = db.EmbeddingMetric()
		if msg.dimension == 0 {
			return msg // not an error — just not configured
		}

		msg.err = db.View(func(tx *hexxladb.Tx) error {
			var err error
			msg.embedCount, msg.hnswEnabled, err = inspect.Embeddings(tx, 1000000)
			return err
		})

		return msg
	}
}

func (v *embeddingsView) View() string {
	if !v.loaded {
		return styleLoading.Render("  ⟳  Loading embeddings…")
	}
	if v.data.err != nil {
		return styleBad.Render(fmt.Sprintf("  ✗  Embeddings error: %v", v.data.err))
	}

	d := v.data
	w := contentWidth(v.width - 6)

	if d.dimension == 0 {
		emptyPanel := Box.BorderForeground(colorText2).Width(w).Render(
			Section.Render("No Embeddings Stored") + "\n\n" +
				Dim.Render("No vectors have been stored in this database yet.") + "\n" +
				Dim.Render("The dimension will be auto-detected from the first PutEmbedding call."))

		help := helpItem("r", "refresh")
		return lipgloss.JoinVertical(lipgloss.Left,
			viewTitle("◈ Embeddings", v.width),
			"",
			emptyPanel,
			"",
			Help.Render("  "+help),
		)
	}

	colW := max(20, (w-4)/4)

	// Reserve space for title(1), help(1) = 2 lines
	maxContentH := max(3, v.height-2)

	// ── stat cards ──────────────────────────────────────────────────────────
	statCard := func(label, value string, clr lipgloss.Color) string {
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clr).
			Background(colorBg2).
			Padding(0, 2).
			Width(colW).
			Render(styleCardDim.Render(label) + "\n" +
				lipgloss.NewStyle().Foreground(clr).Background(colorBg2).Bold(true).Render(value))
	}

	metricStr := "unknown"
	switch d.metric {
	case hexxladb.DistanceCosine:
		metricStr = "cosine"
	case hexxladb.DistanceDotProduct:
		metricStr = "dot"
	case hexxladb.DistanceL2:
		metricStr = "L2"
	}

	row1 := lipgloss.JoinHorizontal(lipgloss.Top,
		statCard("DIMENSION", fmt.Sprintf("%dd", d.dimension), colorPink),
		" ",
		statCard("COUNT", fmt.Sprintf("%d", d.embedCount), colorCyan),
		" ",
		statCard("METRIC", metricStr, colorPurple),
		" ",
		statCard("HNSW GRAPH", func() string {
			if d.hnswEnabled {
				return "enabled"
			}
			return "disabled"
		}(), func() lipgloss.Color {
			if d.hnswEnabled {
				return colorGreen
			}
			return colorText2
		}()),
	)

	// ── info panel ───────────────────────────────────────────────────────────
	infoClr := colorGreen
	if !d.hnswEnabled {
		infoClr = colorOrange
	}
	infoText := "HNSW graph is built automatically after ~50 embeddings for fast ANN search."
	if !d.hnswEnabled {
		infoText = "HNSW graph not yet built (requires ~50+ embeddings). Search uses flat scan."
	}
	infoPanel := Box.BorderForeground(infoClr).Width(w).Render(
		Section.Render("Index Status") + "\n" +
			Dim.Render(infoText))

	help := helpItem("r", "refresh")

	// Build full content then clip to max height
	fullContent := lipgloss.JoinVertical(lipgloss.Left,
		viewTitle("◈ Embeddings", v.width),
		"",
		row1,
		"",
		infoPanel,
		"",
		Help.Render("  "+help),
	)
	clippedContent := lipgloss.NewStyle().MaxHeight(maxContentH).Render(fullContent)

	return clippedContent
}
