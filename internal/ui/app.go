// Package ui is cuddle's Bubble Tea interface. It never runs commands or
// HTTP itself: it calls the interfaces in Deps inside tea.Cmds.
package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Tab is one of the four views.
type Tab int

// The views, in tab order.
const (
	TabToday Tab = iota
	TabTasks
	TabPRs
	TabAgents
)

var tabNames = []string{"Today", "Tasks", "PRs", "Agents"}

// Deps are the services the UI uses.
type Deps struct{}

// App is the root model.
type App struct {
	deps   Deps
	tab    Tab
	width  int
	height int
	flash  string
}

// NewApp returns the root model.
func NewApp(d Deps) *App {
	return &App{deps: d, width: 100, height: 30}
}

// Init starts nothing yet.
func (a *App) Init() tea.Cmd { return nil }

// Update handles messages.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return a, a.handleKey(msg.String())
	}
	return a, nil
}

func (a *App) handleKey(k string) tea.Cmd {
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "1", "2", "3", "4":
		a.tab = Tab(k[0] - '1')
		a.flash = ""
	}
	return nil
}

// View renders the full screen.
func (a *App) View() tea.View {
	v := tea.NewView(a.Render())
	v.AltScreen = true
	v.WindowTitle = "cuddle"
	return v
}

// Render returns the screen as a string.
func (a *App) Render() string {
	var b strings.Builder
	b.WriteString(a.tabBar())
	b.WriteString("\n")
	b.WriteString(sFaint.Render(strings.Repeat("─", a.width)))
	b.WriteString("\n")
	body := sFaint.Render("nothing here yet")
	lines := strings.Split(body, "\n")
	for len(lines) < a.height-4 {
		lines = append(lines, "")
	}
	b.WriteString(strings.Join(lines[:max(0, a.height-4)], "\n"))
	b.WriteString("\n")
	b.WriteString(a.flash)
	b.WriteString("\n")
	b.WriteString(sFaint.Render("1–4 views · q quit"))
	return b.String()
}

func (a *App) tabBar() string {
	var parts []string
	for i, n := range tabNames {
		label := fmt.Sprintf("%d %s", i+1, n)
		if Tab(i) == a.tab {
			parts = append(parts, sTabOn.Render(label))
		} else {
			parts = append(parts, sFaint.Render(label))
		}
	}
	left := strings.Join(parts, "   ")
	right := sBold.Render("cuddle")
	gap := a.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	return left + strings.Repeat(" ", max(1, gap)) + right
}
