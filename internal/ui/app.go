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
	"github.com/slavafyi/cuddle/internal/rex"
	"github.com/slavafyi/cuddle/internal/run"
	"github.com/slavafyi/cuddle/internal/store"
	"github.com/slavafyi/cuddle/internal/workspace"
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
	reposEvery       = 24 * time.Hour
)

// Deps are the services the UI uses.
type Deps struct {
	Config    config.Config
	ClickUp   clickup.Client
	Rex       rex.Client
	Runner    run.Runner
	Cache     store.Cache
	State     store.State
	CachePath string
	StatePath string
	Now       func() time.Time
	// Watch starts the Rex event stream. Nil disables live events.
	Watch func(ctx context.Context) (<-chan rex.Event, error)
}

// App is the root model.
type App struct {
	deps   Deps
	tab    Tab
	width  int
	height int
	flash  string

	cache        store.Cache
	state        store.State
	loadingTasks bool
	syncErr      error

	tasks  *taskView
	today  *todayView
	agents *agentView

	agentInfo   map[string]agentInfo
	confirmText string
	confirmYes  func() tea.Cmd

	mode        mode
	picker      picker
	statuses    map[string][]clickup.Status
	input       string
	commentTask string
	start       *startPanel
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
	reposMsg struct {
		repos []workspace.Repo
		at    time.Time
	}
)

// NewApp returns the root model.
func NewApp(d Deps) *App {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Runner == nil {
		d.Runner = run.Exec{}
	}
	a := &App{deps: d, width: 100, height: 30, tasks: newTaskView(), today: newTodayView(), agents: newAgentView(),
		statuses: map[string][]clickup.Status{}, agentInfo: map[string]agentInfo{}}
	a.state = d.State
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
	l := a.lookup()
	a.tasks.rebuild(a.cache.Tasks, a.cache.Me.ID, now, l)
	a.today.rebuild(a.cache.Tasks, a.cache.Me.ID, now, l)
	a.agents.rebuild(a.state.Agents, a.agentInfo, now)
}

// Init loads tasks and starts the refresh timer.
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.refreshTasks(true), a.scanRepos(false), a.refreshAgents(), a.startWatch(), a.tick())
}

// scanRepos rebuilds the local repo list when it is older than a day, or
// always when force is set.
func (a *App) scanRepos(force bool) tea.Cmd {
	if !force && !a.cache.ReposAt.IsZero() && a.now().Sub(a.cache.ReposAt) < reposEvery {
		return nil
	}
	roots, now := a.deps.Config.RepoRoots, a.now()
	return func() tea.Msg { return reposMsg{repos: workspace.ScanRepos(roots), at: now} }
}

func (a *App) saveState() tea.Cmd {
	s, path := a.state, a.deps.StatePath
	if path == "" {
		return nil
	}
	s.Version = store.StateVersion
	return func() tea.Msg {
		if err := store.Save(path, s); err != nil {
			return errMsg{fmt.Errorf("save state: %w", err)}
		}
		return savedMsg{}
	}
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
		return a, a.handleKey(msg)
	case tickMsg:
		cmds := []tea.Cmd{a.tick(), a.refreshAgents()}
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
	case statusesMsg:
		if msg.err != nil {
			a.flash = sRed.Render(msg.err.Error())
			return a, nil
		}
		a.statuses[msg.listID] = msg.statuses
		a.showPicker(msg.taskID, msg.statuses)
	case writeMsg:
		return a, a.onWrite(msg)
	case launchedMsg:
		return a, a.onLaunched(msg)
	case enterMsg:
		return a, a.onEnter(msg)
	case agentsMsg:
		for b, i := range msg {
			a.agentInfo[b] = i
		}
		a.rebuild()
	case agentEventMsg:
		return a, a.onAgentEvent(msg)
	case watchMsg:
		if msg.err != nil {
			a.flash = sYellow.Render("no live agent updates: " + msg.err.Error())
			return a, nil
		}
		return a, waitEvent(msg.ch)
	case closedMsg:
		return a, a.onClosed(msg)
	case focusedMsg:
		return a, a.onEnter(enterMsg{taskID: msg.agent.TaskID, agent: &msg.agent, err: msg.err})
	case reposMsg:
		a.cache.Repos, a.cache.ReposAt = msg.repos, msg.at
		return a, a.saveCache()
	case errMsg:
		a.flash = sRed.Render(msg.err.Error())
	}
	return a, nil
}

func (a *App) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	switch a.mode {
	case modeStatus:
		return a.pickerKey(k)
	case modeComment:
		return a.commentKey(msg)
	case modeStart:
		return a.startKey(msg)
	case modeConfirm:
		return a.confirmKey(k)
	}
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "1", "2", "3", "4":
		a.tab = Tab(k[0] - '1')
		a.flash = ""
		return nil
	case "r":
		a.flash = sFaint.Render("refreshing…")
		return tea.Batch(a.refreshTasks(true), a.refreshAgents())
	}
	switch a.tab {
	case TabTasks:
		return a.taskKey(k)
	case TabToday:
		return a.listKey(&a.today.list, k)
	case TabAgents:
		return a.agentKey(k)
	}
	return nil
}

// listKey moves the cursor in l, or runs a task action.
func (a *App) listKey(l *list, k string) tea.Cmd {
	switch k {
	case "j", "down":
		l.move(1)
	case "k", "up":
		l.move(-1)
	default:
		return a.taskAction(k)
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
	default:
		return a.taskAction(k)
	}
	return nil
}

// taskAction handles keys that act on the selected task, in any view that
// lists tasks.
func (a *App) taskAction(k string) tea.Cmd {
	t, ok := a.selectedTask()
	if !ok {
		return nil
	}
	switch k {
	case "o":
		if t.URL != "" {
			return a.runCmd(run.Cmd{Name: "open", Args: []string{t.URL}})
		}
	case "s":
		return a.openStatusPicker(t)
	case "c":
		a.mode, a.input, a.commentTask = modeComment, "", t.ID
	case "a":
		return a.toggleAssign(t)
	case "enter":
		return a.enterTask(t)
	case "shift+enter":
		a.openStart(t)
		return a.scanRepos(false)
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
	case TabToday:
		r, ok = a.today.list.selected()
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
	flash := a.flash
	if a.mode == modeConfirm {
		flash = sYellow.Render(a.confirmText) + sFaint.Render("  y yes · any other key no")
	}
	if a.mode == modeComment {
		t, _ := a.taskByID(a.commentTask)
		flash = sBold.Render("comment "+t.Key()+": ") + a.input + "▏" + sFaint.Render("  enter send · esc cancel")
	}
	b.WriteString(ansi.Truncate(flash, a.width, "…") + "\n")
	b.WriteString(ansi.Truncate(sFaint.Render(keys), a.width, "…"))
	switch a.mode {
	case modeStatus:
		return overlay(b.String(), a.renderPicker(), max(2, (a.width-44)/2), 2)
	case modeStart:
		return overlay(b.String(), a.renderStart(a.height-2), max(0, a.width-panelWidth), 2)
	}
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
	case TabToday:
		sub = a.today.subHeader(now)
		body = func(h int) string { return a.today.list.render(a.bodyWidth(), h) }
		if t, ok := a.selectedTask(); ok {
			detail = taskDetail(t, me, now)
		}
		keys = "j/k move · s status · c comment · a assign · o open · r refresh · 1–4 views · q quit"
	case TabTasks:
		sub = a.tasks.subHeader(a.cache.Tasks, me)
		body = func(h int) string { return a.tasks.list.render(a.bodyWidth(), h) }
		if t, ok := a.selectedTask(); ok {
			detail = taskDetail(t, me, now)
		}
		keys = "j/k move · s status · c comment · a assign · o open · t filter · r refresh · q quit"
	case TabAgents:
		sub = sFaint.Render(plural(len(a.state.Agents), "agent")+" · tabs started by cuddle, live from Rex") + "\n" +
			sFaint.Render("enter focus tab · x close tab (asks first)")
		body = func(h int) string { return a.agents.list.render(a.bodyWidth(), h) }
		if ag, ok := a.selectedAgent(); ok {
			detail = agentDetail(ag, a.agentInfo[ag.BlockID])
		}
		keys = "j/k move · enter focus · x close · r refresh · 1–4 views · q quit"
	default:
		sub = sFaint.Render(tabNames[a.tab])
		body = func(int) string { return sFaint.Render("coming soon") }
		keys = "1–4 views · q quit"
	}
	return
}

// bodyWidth is the list width, leaving room for the Start panel when open.
func (a *App) bodyWidth() int {
	if a.mode == modeStart {
		return max(20, a.width-panelWidth-1)
	}
	return a.width
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
	right := a.counters() + a.syncLabel() + "  " + sBold.Render("cuddle")
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
