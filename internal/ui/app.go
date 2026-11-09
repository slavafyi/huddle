// Package ui is cuddle's Bubble Tea interface. It never runs commands or
// HTTP itself: it calls the interfaces in Deps inside tea.Cmds.
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/slavafyi/cuddle/internal/clickup"
	"github.com/slavafyi/cuddle/internal/config"
	"github.com/slavafyi/cuddle/internal/run"
	"github.com/slavafyi/cuddle/internal/store"
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

// Refresh timing (SPEC §4.7).
const (
	tickEvery        = 15 * time.Second
	tasksEvery       = 3 * time.Minute
	tasksFullEvery   = 30 * time.Minute
	tasksSinceMargin = time.Minute
)

// Deps are the services the UI uses.
type Deps struct {
	Config    config.Config
	ClickUp   clickup.Client
	Runner    run.Runner
	Cache     store.Cache
	CachePath string
	StatePath string
	Now       func() time.Time
}

// App is the root model.
type App struct {
	deps   Deps
	tab    Tab
	width  int
	height int
	flash  string

	cache        store.Cache
	loadingTasks bool
	syncErr      error

	tasks *taskView
}

// Messages.
type (
	tasksMsg struct {
		me    clickup.User
		tasks []clickup.Task
		full  bool
		at    time.Time
		err   error
	}
	tickMsg  time.Time
	errMsg   struct{ err error }
	savedMsg struct{}
)

// NewApp returns the root model.
func NewApp(d Deps) *App {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Runner == nil {
		d.Runner = run.Exec{}
	}
	a := &App{deps: d, width: 100, height: 30, tasks: newTaskView()}
	a.applyCache(d.Cache)
	return a
}

func (a *App) now() time.Time { return a.deps.Now() }

// applyCache replaces the in-memory data with c and rebuilds the views.
func (a *App) applyCache(c store.Cache) {
	a.cache = c
	a.rebuild()
}

func (a *App) rebuild() {
	now := a.now()
	a.tasks.rebuild(a.cache.Tasks, a.cache.Me.ID, now, agentLookup{})
}

// Init loads tasks and starts the refresh timer.
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.refreshTasks(true), a.tick())
}

func (a *App) tick() tea.Cmd {
	return tea.Tick(tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// tasksDue reports whether tasks should refresh, and whether fully.
func (a *App) tasksDue() (full, due bool) {
	now := a.now()
	if a.cache.TasksFullAt.IsZero() || now.Sub(a.cache.TasksFullAt) >= tasksFullEvery {
		return true, true
	}
	return false, now.Sub(a.cache.TasksSyncedAt) >= tasksEvery
}

// refreshTasks fetches tasks in the background. A non-full refresh only asks
// for tasks updated since the last sync (minus a margin).
func (a *App) refreshTasks(full bool) tea.Cmd {
	if a.loadingTasks || a.deps.ClickUp == nil {
		return nil
	}
	a.loadingTasks = true
	cu, me, now := a.deps.ClickUp, a.cache.Me, a.now()
	since := time.Time{}
	if !full && !a.cache.TasksSyncedAt.IsZero() {
		since = a.cache.TasksSyncedAt.Add(-tasksSinceMargin)
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if me.ID == 0 {
			u, err := cu.Me(ctx)
			if err != nil {
				return tasksMsg{err: err}
			}
			me = u
		}
		ts, err := cu.MyTasks(ctx, me.ID, since, now)
		return tasksMsg{me: me, tasks: ts, full: since.IsZero(), at: now, err: err}
	}
}

func (a *App) saveCache() tea.Cmd {
	c, path := a.cache, a.deps.CachePath
	if path == "" {
		return nil
	}
	c.Version = store.CacheVersion
	return func() tea.Msg {
		if err := store.Save(path, c); err != nil {
			return errMsg{fmt.Errorf("save cache: %w", err)}
		}
		return savedMsg{}
	}
}

// Update handles messages.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return a, a.handleKey(msg.String())
	case tickMsg:
		cmds := []tea.Cmd{a.tick()}
		if full, due := a.tasksDue(); due {
			cmds = append(cmds, a.refreshTasks(full))
		}
		return a, tea.Batch(cmds...)
	case tasksMsg:
		a.loadingTasks = false
		if msg.err != nil {
			a.syncErr = msg.err
			return a, nil
		}
		a.syncErr = nil
		a.cache.Me = msg.me
		a.cache.Tasks = clickup.Merge(a.cache.Tasks, msg.tasks, msg.full, msg.me.ID, msg.at)
		a.cache.TasksSyncedAt = msg.at
		if msg.full {
			a.cache.TasksFullAt = msg.at
		}
		a.rebuild()
		return a, a.saveCache()
	case errMsg:
		a.flash = sRed.Render(msg.err.Error())
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
		return nil
	case "r":
		a.flash = sFaint.Render("refreshing…")
		return a.refreshTasks(true)
	}
	switch a.tab {
	case TabTasks:
		return a.taskKey(k)
	}
	return nil
}

func (a *App) taskKey(k string) tea.Cmd {
	l := &a.tasks.list
	switch k {
	case "j", "down":
		l.move(1)
	case "k", "up":
		l.move(-1)
	case "t":
		a.tasks.filter = (a.tasks.filter + 1) % 3
		a.rebuild()
	case "o":
		if t, ok := a.selectedTask(); ok && t.URL != "" {
			return a.runCmd(run.Cmd{Name: "open", Args: []string{t.URL}})
		}
	}
	return nil
}

// selectedTask returns the task under the cursor in the current view.
func (a *App) selectedTask() (clickup.Task, bool) {
	var r row
	var ok bool
	switch a.tab {
	case TabTasks:
		r, ok = a.tasks.list.selected()
	}
	if !ok {
		return clickup.Task{}, false
	}
	return a.taskByID(r.key)
}

func (a *App) taskByID(id string) (clickup.Task, bool) {
	for _, t := range a.cache.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return clickup.Task{}, false
}

// runCmd runs an external command in the background and reports failures.
func (a *App) runCmd(c run.Cmd) tea.Cmd {
	r := a.deps.Runner
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := r.Run(ctx, c); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

// View renders the full screen.
func (a *App) View() tea.View {
	v := tea.NewView(a.Render())
	v.AltScreen = true
	v.WindowTitle = "cuddle"
	return v
}

const detailLines = 4

// Render returns the screen as a string.
func (a *App) Render() string {
	sub, body, detail, keys := a.viewParts()
	subLines := strings.Split(sub, "\n")
	bodyH := a.height - 2 - len(subLines) - 1 - (detailLines + 1) - 2
	var b strings.Builder
	b.WriteString(a.tabBar() + "\n")
	b.WriteString(sFaint.Render(strings.Repeat("─", a.width)) + "\n")
	for _, l := range subLines {
		b.WriteString(ansi.Truncate(l, a.width, "…") + "\n")
	}
	b.WriteString("\n")
	b.WriteString(pad(body(max(1, bodyH)), bodyH))
	b.WriteString(sFaint.Render(strings.Repeat("─", a.width)) + "\n")
	for i := 0; i < detailLines; i++ {
		line := ""
		if i < len(detail) {
			line = detail[i]
		}
		b.WriteString(ansi.Truncate(line, a.width, "…") + "\n")
	}
	b.WriteString(ansi.Truncate(a.flash, a.width, "…") + "\n")
	b.WriteString(ansi.Truncate(sFaint.Render(keys), a.width, "…"))
	return b.String()
}

// pad returns s with exactly h lines, each ending in a newline.
func pad(s string, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:max(0, h)]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	if h <= 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) viewParts() (sub string, body func(h int) string, detail []string, keys string) {
	me, now := a.cache.Me.ID, a.now()
	switch a.tab {
	case TabTasks:
		sub = a.tasks.subHeader(a.cache.Tasks, me)
		body = func(h int) string { return a.tasks.list.render(a.width, h) }
		if t, ok := a.selectedTask(); ok {
			detail = taskDetail(t, me, now)
		}
		keys = "j/k move · t filter · o open in ClickUp · r refresh · 1–4 views · q quit"
	default:
		sub = sFaint.Render(tabNames[a.tab])
		body = func(int) string { return sFaint.Render("coming soon") }
		keys = "1–4 views · q quit"
	}
	return
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
	right := a.syncLabel() + "  " + sBold.Render("cuddle")
	gap := a.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	return left + strings.Repeat(" ", max(1, gap)) + right
}

func (a *App) syncLabel() string {
	at := a.cache.TasksSyncedAt
	switch {
	case a.syncErr != nil && !at.IsZero():
		return sRed.Render("offline · cached " + ago(a.now().Sub(at)) + " ago")
	case a.syncErr != nil:
		return sRed.Render("offline")
	case at.IsZero():
		return sFaint.Render("loading…")
	}
	return sFaint.Render("synced " + ago(a.now().Sub(at)) + " ago")
}

// agentLookup marks tasks that have an agent tab. Filled in by later phases.
type agentLookup struct{}

func (agentLookup) badge(string) string { return "" }
