package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Terminal palette based on ollamon reference - uses standard terminal colors
// for better compatibility and semantic naming.
var (
	// Semantic style names following ollamon pattern
	AppBg       = lipgloss.NewStyle().Background(lipgloss.Color("235")).Foreground(lipgloss.Color("252"))
	Header      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("25")).Padding(0, 1)
	Subtle      = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	Title       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	Section     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
	Box         = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("61")).Padding(0, 1)
	Highlight   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("221"))
	Accent      = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	OK          = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	Warn        = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	Err         = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	Dim         = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	Filter      = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("166")).Padding(0, 1)
	LogLine     = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	Help        = lipgloss.NewStyle().Foreground(lipgloss.Color("251")).Background(lipgloss.Color("237")).Padding(0, 1)
	MetricLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("110"))

	// Legacy color names for backward compatibility (will phase out)
	colorPurple = lipgloss.Color("61")  // primary accent
	colorCyan   = lipgloss.Color("81")  // secondary accent / data
	colorGreen  = lipgloss.Color("42")  // success / included
	colorPink   = lipgloss.Color("213") // warning / superseded
	colorOrange = lipgloss.Color("214") // caution / seam
	colorRed    = lipgloss.Color("196") // error / evicted
	colorYellow = lipgloss.Color("229") // highlight / tags

	// Backgrounds
	colorBg0 = lipgloss.Color("235") // near-black base
	colorBg1 = lipgloss.Color("236") // panel bg
	colorBg2 = lipgloss.Color("237") // card / row bg
	colorBg3 = lipgloss.Color("238") // selected row bg

	// Text
	colorText0 = lipgloss.Color("252") // primary
	colorText1 = lipgloss.Color("245") // secondary
	colorText2 = lipgloss.Color("243") // dim / muted

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

	styleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPurple)

	styleBorderSubtle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorText2)

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
			Background(colorBg1).
			Bold(true)

	styleSectionHeader = lipgloss.NewStyle().
				Foreground(colorCyan).
				Background(colorBg1).
				Bold(true)

	styleDim = lipgloss.NewStyle().
			Foreground(colorText2).
			Background(colorBg1)

	styleTag = lipgloss.NewStyle().
			Foreground(colorYellow).
			Background(lipgloss.Color("#2A2600")).
			Padding(0, 1)

	styleBad = lipgloss.NewStyle().
			Foreground(colorRed).
			Background(colorBg1)

	// Help bar at bottom of content
	styleHelp = lipgloss.NewStyle().
			Foreground(colorText2).
			Background(colorBg1).
			Italic(true)

	styleHelpKey = lipgloss.NewStyle().
			Foreground(colorCyan).
			Background(colorBg1).
			Bold(true)

	// Card-interior text styles (colorBg2 background for use inside styleCard panels).
	styleCardDim = lipgloss.NewStyle().
			Foreground(colorText2).
			Background(colorBg2)

	styleCardHeader = lipgloss.NewStyle().
			Foreground(colorCyan).
			Background(colorBg2).
			Bold(true)

	styleCardKey = lipgloss.NewStyle().
			Foreground(colorPurple).
			Background(colorBg2).
			Bold(true)

	styleCardValue = lipgloss.NewStyle().
			Foreground(colorText0).
			Background(colorBg2)

	// Loading spinner text
	styleLoading = lipgloss.NewStyle().
			Foreground(colorPurple).
			Background(colorBg1).
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

// barGraph renders a colored bar of proportional width against colorBg1.
func barGraph(value, maxVal, maxWidth int, clr lipgloss.Color) string {
	return barGraphBg(value, maxVal, maxWidth, clr, colorBg1)
}

// barGraphBg renders a colored bar with an explicit background color.
func barGraphBg(value, maxVal, maxWidth int, clr, bg lipgloss.Color) string {
	if maxVal == 0 {
		return ""
	}
	w := value * maxWidth / maxVal
	if w < 1 && value > 0 {
		w = 1
	}
	return lipgloss.NewStyle().Foreground(clr).Background(bg).Render(repeatStr("█", w))
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

// contentWidth returns a safe width for content, with fallback to 80.
// Prevents division by zero on tiny terminals.
func contentWidth(w int) int {
	if w > 4 {
		return w
	}
	return 80
}

// twoColumnWidths splits total width into two columns.
// For < 100 chars: simple 50/50 split.
// For >= 100 chars: left = total/2 - 1, right = total - left (1-char gap).
func twoColumnWidths(total int) (left, right int) {
	if total < 100 {
		return total / 2, total - total/2
	}
	left = total/2 - 1
	right = total - left
	return left, right
}
