package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Only the 16 ANSI colors plus bold, faint and reverse are used, so the
// terminal theme decides how huddle looks in light and dark mode.
var (
	sBold    = lipgloss.NewStyle().Bold(true)
	sFaint   = lipgloss.NewStyle().Faint(true)
	sRed     = lipgloss.NewStyle().Foreground(lipgloss.Red)
	sGreen   = lipgloss.NewStyle().Foreground(lipgloss.Green)
	sYellow  = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	sBlue    = lipgloss.NewStyle().Foreground(lipgloss.Blue)
	sMagenta = lipgloss.NewStyle().Foreground(lipgloss.Magenta)
	sCyan    = lipgloss.NewStyle().Foreground(lipgloss.Cyan)
	sTabOn   = sBold // not a color: blue is grey in some themes
)

// selBG is the selected row's background: ANSI 8 (bright black).
const selBG = "\x1b[100m"

// selected draws s as the selected row, the same in every list, picker and
// panel: a grey background across width cells. Colors stay; faint text
// turns normal so it stays readable on grey (SPEC §4.8).
func selected(s string, width int) string {
	s = strings.ReplaceAll(s, "\x1b[2m", "")
	s = strings.ReplaceAll(s, "\x1b[m", "\x1b[m"+selBG)
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+selBG)
	if n := width - ansi.StringWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return selBG + s + "\x1b[m"
}

// statusStyle returns the style for a status name. Names it doesn't know
// are green when done, else faint.
func statusStyle(status string, done bool) lipgloss.Style {
	switch status {
	case "to start":
		return sGreen
	case "in progress":
		return sBlue
	case "to review":
		return sYellow
	case "feedbacks":
		return sRed
	case "to publish":
		return sCyan
	case "published":
		return sBold
	case "Closed", "closed", "complete":
		return sGreen
	}
	if done {
		return sGreen
	}
	return sFaint
}
