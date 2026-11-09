package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// box draws lines inside a faint border, width cells wide in total.
func box(lines []string, width int) []string {
	inner := width - 4
	out := []string{sFaint.Render("┌" + strings.Repeat("─", width-2) + "┐")}
	for _, l := range lines {
		l = ansi.Truncate(l, inner, "…")
		out = append(out, sFaint.Render("│ ")+l+strings.Repeat(" ", inner-ansi.StringWidth(l))+sFaint.Render(" │"))
	}
	return append(out, sFaint.Render("└"+strings.Repeat("─", width-2)+"┘"))
}

// overlay draws top over base, with its top-left corner at column x, row y.
func overlay(base string, top []string, x, y int) string {
	lines := strings.Split(base, "\n")
	for i, t := range top {
		r := y + i
		for len(lines) <= r {
			lines = append(lines, "")
		}
		l := lines[r]
		if w := ansi.StringWidth(l); w < x {
			l += strings.Repeat(" ", x-w)
		}
		left := ansi.Truncate(l, x, "")
		right := ansi.TruncateLeft(l, x+ansi.StringWidth(t), "")
		lines[r] = left + t + right
	}
	return strings.Join(lines, "\n")
}
