package ui

import (
	"charm.land/lipgloss/v2"
)

// Only the 16 ANSI colors plus bold, faint and reverse are used, so the
// terminal theme decides how huddle looks in light and dark mode.
var (
	sBold    = lipgloss.NewStyle().Bold(true)
	sFaint   = lipgloss.NewStyle().Faint(true)
	sSel     = lipgloss.NewStyle().Reverse(true)
	sRed     = lipgloss.NewStyle().Foreground(lipgloss.Red)
	sGreen   = lipgloss.NewStyle().Foreground(lipgloss.Green)
	sYellow  = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	sBlue    = lipgloss.NewStyle().Foreground(lipgloss.Blue)
	sMagenta = lipgloss.NewStyle().Foreground(lipgloss.Magenta)
	sCyan    = lipgloss.NewStyle().Foreground(lipgloss.Cyan)
	sTabOn   = lipgloss.NewStyle().Foreground(lipgloss.Blue).Bold(true)
)

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
