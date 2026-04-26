package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Neon-on-dark palette — all colors optimised for dark terminals.
var (
	// Core accent colors
	colorPurple = lipgloss.Color("#9D50FF") // primary accent
	colorCyan   = lipgloss.Color("#00D7FF") // secondary accent / data
	colorGreen  = lipgloss.Color("#00FF9C") // success / included
	colorPink   = lipgloss.Color("#FF6ACB") // warning / superseded
	colorOrange = lipgloss.Color("#FF9E4F") // caution / seam
	colorRed    = lipgloss.Color("#FF4F6A") // error / evicted
	colorYellow = lipgloss.Color("#EDFF82") // highlight / tags

	// Backgrounds
	colorBg0 = lipgloss.Color("#0D0D14") // near-black base
	colorBg1 = lipgloss.Color("#13131E") // panel bg
	colorBg2 = lipgloss.Color("#1C1C2E") // card / row bg
	colorBg3 = lipgloss.Color("#252540") // selected row bg

	// Text
	colorText0 = lipgloss.Color("#FAFAFA") // primary
	colorText1 = lipgloss.Color("#B0B8D0") // secondary
	colorText2 = lipgloss.Color("#5A6080") // dim / muted

	// Tab border — active tab has open bottom to merge with content
	activeTabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      " ",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "┘",
		BottomRight: "└",
	}
	inactiveTabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      "─",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "┴",
		BottomRight: "┴",
	}

	styleTabActive = lipgloss.NewStyle().
			Border(activeTabBorder, true).
			BorderForeground(colorPurple).
			Foreground(colorText0).
			Bold(true).
			Padding(0, 2)

	styleTabInactive = lipgloss.NewStyle().
				Border(inactiveTabBorder, true).
				BorderForeground(colorText2).
				Foreground(colorText1).
				Padding(0, 2)

	styleTabGap = lipgloss.NewStyle().
			BorderBottom(true).
			BorderStyle(lipgloss.Border{Bottom: "─"}).
			BorderForeground(colorText2).
			Background(colorBg0)

	styleContent = lipgloss.NewStyle().
			Background(colorBg1).
			Foreground(colorText0).
			Padding(0, 2)

	styleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPurple)

	styleBorderSubtle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorText2)

	styleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorText2).
			Background(colorBg2).
			Padding(0, 2)

	// Status bar segments
	styleStatusLeft = lipgloss.NewStyle().
			Background(colorPurple).
			Foreground(colorBg0).
			Bold(true).
			Padding(0, 1)

	styleStatusMid = lipgloss.NewStyle().
			Background(colorBg2).
			Foreground(colorText1).
			Padding(0, 1)

	styleStatusRight = lipgloss.NewStyle().
				Background(lipgloss.Color("#1A1040")).
				Foreground(colorText2).
				Padding(0, 1)

	// Section headers inside views
	styleViewTitle = lipgloss.NewStyle().
			Foreground(colorPurple).
			Bold(true)

	styleSectionHeader = lipgloss.NewStyle().
				Foreground(colorCyan).
				Bold(true)

	// Labels / keys
	styleKey = lipgloss.NewStyle().
			Foreground(colorPurple).
			Bold(true)

	styleValue = lipgloss.NewStyle().
			Foreground(colorText0)

	styleDim = lipgloss.NewStyle().
			Foreground(colorText2)

	styleTag = lipgloss.NewStyle().
			Foreground(colorYellow).
			Background(lipgloss.Color("#2A2600")).
			Padding(0, 1)

	styleGood = lipgloss.NewStyle().
			Foreground(colorGreen)

	styleWarn = lipgloss.NewStyle().
			Foreground(colorOrange)

	styleBad = lipgloss.NewStyle().
			Foreground(colorRed)

	stylePink = lipgloss.NewStyle().
			Foreground(colorPink)

	// Help bar at bottom of content
	styleHelp = lipgloss.NewStyle().
			Foreground(colorText2).
			Italic(true)

	styleHelpKey = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true)

	// Loading spinner text
	styleLoading = lipgloss.NewStyle().
			Foreground(colorPurple).
			Bold(true).
			Italic(true)
)

// helpItem renders a key + description pair for the help bar.
func helpItem(key, desc string) string {
	return styleHelpKey.Render(key) + styleDim.Render(" "+desc)
}

// viewTitle renders a section title with a dim underline rule, sized to width w.
func viewTitle(title string, w int) string {
	t := styleViewTitle.Render(title)
	rule := styleDim.Render(strings.Repeat("─", max(0, w-lipgloss.Width(t)-1)))
	return lipgloss.JoinHorizontal(lipgloss.Top, t, " ", rule)
}

// clampLines hard-caps s to at most maxLines rendered lines, truncating the rest.
// This prevents inner view content from overflowing the fixed-height content panel.
func clampLines(s string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.SplitN(s, "\n", maxLines+1)
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n")
}

// truncStr truncates a string to maxLen with ellipsis.
func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}

// barGraph renders a colored bar of proportional width.
func barGraph(value, maxVal, maxWidth int, clr lipgloss.Color) string {
	if maxVal == 0 {
		return ""
	}
	w := value * maxWidth / maxVal
	if w < 1 && value > 0 {
		w = 1
	}
	return lipgloss.NewStyle().Foreground(clr).Render(repeatStr("█", w))
}

func repeatStr(s string, n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}
