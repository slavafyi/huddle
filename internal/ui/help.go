package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// helpWidth is the help popup's width, border included.
const helpWidth = 70

type keyHelp struct{ key, what string }

var (
	taskKeys = []keyHelp{
		{"j/k ctrl+n/p", "move"},
		{"enter", "start / focus agent"},
		{"shift+enter", "start another agent"},
		{"s", "status"},
		{"c", "comment"},
		{"a", "assign me"},
		{"o / p", "open task / PR"},
	}
	panelKeys = []keyHelp{
		{"tab", "next field"},
		{"space / ←→", "change value"},
		{"↑↓ ctrl+n/p", "move in list"},
		{"enter", "launch"},
		{"esc", "close"},
	}
	anywhereKeys = []keyHelp{
		{"tab / 1–4", "switch view"},
		{"r", "refresh"},
		{"P", "switch profile"},
		{"`", "last profile"},
		{"?", "keys"},
		{"q", "quit"},
	}
)

// viewKeys returns the title and keys of the current view.
func (a *App) viewKeys() (string, []keyHelp) {
	switch a.tab {
	case TabTasks:
		return a.tasks.title, append(append([]keyHelp(nil), taskKeys...), keyHelp{"t", "filter"})
	case TabPRs:
		return "PRs", []keyHelp{{"j/k ctrl+n/p", "move"}, {"enter", "diff in a Rex tab"}, {"o", "open on GitHub"}}
	case TabAgents:
		return "Agents", []keyHelp{{"j/k ctrl+n/p", "move"}, {"enter", "focus tab"}, {"x", "close tab"}, {"X", "clean merged"}}
	}
	return "Today", taskKeys
}

func helpGroup(title string, keys []keyHelp) []string {
	lines := []string{sBold.Render(title)}
	for _, k := range keys {
		lines = append(lines, "  "+k.key+strings.Repeat(" ", max(1, 13-ansi.StringWidth(k.key)))+sFaint.Render(k.what))
	}
	return lines
}

// renderHelp draws the key popup: the current view on the left, the Start
// panel (for task views) and global keys on the right.
func (a *App) renderHelp() []string {
	title, keys := a.viewKeys()
	left := helpGroup(title, keys)
	var right []string
	if a.tab == TabToday || a.tab == TabTasks {
		right = append(helpGroup("Start panel", panelKeys), "")
	}
	anywhere := append([]keyHelp(nil), anywhereKeys...)
	anywhere[0].key = fmt.Sprintf("tab / 1–%d", len(a.tabs))
	right = append(right, helpGroup("Anywhere", anywhere)...)
	const col = 36
	var lines []string
	for i := 0; i < max(len(left), len(right)); i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		lines = append(lines, l+strings.Repeat(" ", max(1, col-ansi.StringWidth(l)))+r)
	}
	lines = append(lines, "", sFaint.Render("any key closes"))
	return box(lines, helpWidth)
}
