package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/hnsw"
	"github.com/hexxla/hexxladb/internal/index"
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
			msg.err = fmt.Errorf("embeddings not enabled")
			return msg
		}

		// Check HNSW graph existence via storage adapter
		var hnswEnabled bool
		_ = db.View(func(tx *hexxladb.Tx) error {
			stor := &txHNSWStorage{tx: tx}
			_, hasMeta, err := stor.GetHNSWMeta()
			if err == nil && hasMeta {
				hnswEnabled = true
			}
			return nil
		})
		msg.hnswEnabled = hnswEnabled

		// Count embeddings by scanning embed/ keyspace
		var count int
		_ = db.View(func(tx *hexxladb.Tx) error {
			_ = tx.AscendRange([]byte(index.EmbedPrefix), index.EmbedKeyEnd(), func(_, _ []byte) bool {
				count++
				return count < 1000000 // reasonable upper bound
			})
			return nil
		})
		msg.embedCount = count

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

// txHNSWStorage is a minimal storage adapter for checking HNSW meta existence.
type txHNSWStorage struct {
	tx *hexxladb.Tx
}

func (s *txHNSWStorage) GetHNSWMeta() (hnsw.Meta, bool, error) {
	k := []byte(index.HNSWMetaKey)
	v, ok, err := s.tx.Get(k)
	if err != nil {
		return hnsw.Meta{}, false, err
	}
	if !ok {
		return hnsw.Meta{}, false, nil
	}
	meta, err := hnsw.DecodeMeta(v)
	if err != nil {
		return hnsw.Meta{}, false, err
	}
	return *meta, true, nil
}
