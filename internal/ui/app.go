// Package ui is huddle's Bubble Tea interface. It never runs commands or
// HTTP itself: it calls the interfaces in Deps inside tea.Cmds.
package ui

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/slavafyi/huddle/internal/config"
	"github.com/slavafyi/huddle/internal/github"
	"github.com/slavafyi/huddle/internal/rex"
	"github.com/slavafyi/huddle/internal/run"
	"github.com/slavafyi/huddle/internal/store"
	"github.com/slavafyi/huddle/internal/task"
	"github.com/slavafyi/huddle/internal/workspace"
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
	Sources   []task.Source // where tasks come from
	GitHub    github.Client
	Rex       rex.Client
	Runner    run.Runner
	Cache     store.Cache
	State     store.State
	CachePath string
	StatePath string
	Now       func() time.Time
	// HomeSession is the Rex session huddle runs in, for opening diffs.
	HomeSession string
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
	rules        map[string]task.Rules // by source name
	loadingTasks bool
	syncErr      error

	tasks  *taskView
	today  *todayView
	agents *agentView
	prs    *prView

	loadingPRs    bool
	prErr         error
	loadingAgents bool
	refreshAgain  bool

	ops       map[uint64]*op
	opID      uint64
	completed uint64
	launching map[string]bool
	lastStart map[string]*startPanel // by task ID, while that launch runs

	writer  *store.Writer
	saveGen uint64

	agentInfo   map[string]agentInfo
	confirmText string
	confirmYes  func() tea.Cmd

	mode        mode
	picker      picker
	statuses    map[string][]task.Status // by task.Ref(source, list ID)
	input       string
	commentTask string // task.Ref
	start       *startPanel
}

// Messages.
type (
	tasksMsg struct {
		results []fetched // one per source
		full    bool
		at      time.Time
		seq     uint64 // App.completed when the refresh started
	}
	tickMsg  time.Time
	errMsg   struct{ err error }
	savedMsg struct{}
	reposMsg struct {
		repos []workspace.Repo
		at    time.Time
	}
)

// fetched is one source's refresh result.
type fetched struct {
	source string
	me     int64
	tasks  []task.Task
	err    error
}

// NewApp returns the root model.
func NewApp(d Deps) *App {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Runner == nil {
		d.Runner = run.Exec{}
	}
	a := &App{deps: d, width: 100, height: 30, tasks: newTaskView(), today: newTodayView(), agents: newAgentView(), prs: newPRView(),
		statuses: map[string][]task.Status{}, rules: map[string]task.Rules{}, agentInfo: map[string]agentInfo{},
		ops: map[uint64]*op{}, launching: map[string]bool{}, lastStart: map[string]*startPanel{}, writer: &store.Writer{}}
	for _, s := range d.Sources {
		a.rules[s.Name()] = s.Rules()
	}
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
	a.tasks.rebuild(a.cache.Tasks, now, l)
	a.today.rebuild(a.cache.Tasks, a.rules, now, l)
	a.agents.rebuild(a.state.Agents, a.agentInfo, now)
	a.prs.rebuild(a.cache.PRs, a.cache.Tasks, now)
}

// Init loads tasks and starts the refresh timer.
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.refreshTasks(true), a.refreshPRs(), a.scanRepos(false), a.refreshAgents(), a.startWatch(), a.tick())
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

// refreshTasks fetches tasks from every source in the background. A
// non-full refresh only asks for tasks updated since the last sync (minus a
// margin).
func (a *App) refreshTasks(full bool) tea.Cmd {
	if a.loadingTasks || len(a.deps.Sources) == 0 {
		return nil
	}
	a.loadingTasks = true
	srcs, me, now, seq := a.deps.Sources, maps.Clone(a.cache.Me), a.now(), a.completed
	since := time.Time{}
	if !full && !a.cache.TasksSyncedAt.IsZero() {
		since = a.cache.TasksSyncedAt.Add(-tasksSinceMargin)
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		msg := tasksMsg{full: since.IsZero(), at: now, seq: seq}
		for _, s := range srcs {
			r := fetched{source: s.Name(), me: me[s.Name()]}
			if r.me == 0 {
				r.me, r.err = s.Me(ctx)
			}
			if r.err == nil {
				r.tasks, r.err = s.Fetch(ctx, r.me, since, now)
			}
			msg.results = append(msg.results, r)
		}
		return msg
	}
}

// source returns the source with the given name, or nil.
func (a *App) source(name string) task.Source {
	for _, s := range a.deps.Sources {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

// Update handles messages.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return a, a.handleKey(msg)
	case tea.PasteMsg:
		a.paste(strings.Join(strings.Fields(msg.Content), " "))
	case tickMsg:
		cmds := []tea.Cmd{a.tick(), a.refreshAgents()}
		if a.now().Sub(a.cache.PRsAt) >= prsEvery {
			cmds = append(cmds, a.refreshPRs())
		}
		if full, due := a.tasksDue(); due {
			cmds = append(cmds, a.refreshTasks(full))
		}
		return a, tea.Batch(cmds...)
	case tasksMsg:
		a.loadingTasks = false
		var again tea.Cmd // a write finished while this refresh ran
		if a.refreshAgain {
			a.refreshAgain = false
			again = a.refreshTasks(false)
		}
		// A failed source keeps its cached tasks; the others still merge.
		// Sync times move only when every source succeeded.
		a.syncErr = nil
		merged := false
		for _, r := range msg.results {
			if r.err != nil {
				if a.syncErr == nil {
					a.syncErr = r.err
				}
				continue
			}
			if a.cache.Me == nil {
				a.cache.Me = map[string]int64{}
			}
			a.cache.Me[r.source] = r.me
			a.cache.Tasks = task.Merge(a.cache.Tasks, r.tasks, r.source, msg.full, msg.at)
			merged = true
		}
		if !merged {
			return a, again
		}
		if a.syncErr == nil {
			a.cache.TasksSyncedAt = msg.at
			if msg.full {
				a.cache.TasksFullAt = msg.at
			}
		}
		a.reapplyOps(msg.seq)
		a.rebuild()
		return a, tea.Batch(a.saveCache(), again)
	case statusesMsg:
		if msg.err != nil {
			a.flash = sRed.Render(msg.err.Error())
			return a, nil
		}
		a.statuses[msg.list] = msg.statuses
		a.showPicker(msg.ref, msg.statuses)
	case writeMsg:
		return a, a.onWrite(msg)
	case openedMsg:
		return a, a.onOpened(msg)
	case appliedMsg:
		return a, a.onApplied(msg)
	case enterMsg:
		return a, a.onEnter(msg)
	case agentsMsg:
		a.loadingAgents = false
		for b, i := range msg.info {
			a.agentInfo[b] = i
		}
		if msg.err != nil {
			a.flash = sYellow.Render("Rex: " + msg.err.Error())
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
	case prsMsg:
		return a, a.onPRs(msg)
	case cleanedMsg:
		return a, a.onCleaned(msg)
	case focusedMsg:
		return a, a.onEnter(enterMsg{ref: msg.agent.TaskRef(), agent: &msg.agent, err: msg.err})
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
	case modeHelp:
		a.mode = modeNormal // any key closes help
		return nil
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
	case "?":
		a.mode = modeHelp
		return nil
	case "q", "ctrl+c":
		return tea.Quit
	case "1", "2", "3", "4":
		a.tab = Tab(k[0] - '1')
		a.flash = ""
		return nil
	case "tab", "shift+tab":
		d := 1
		if k == "shift+tab" {
			d = -1
		}
		a.tab = Tab(cycle(int(a.tab), d, len(tabNames)))
		a.flash = ""
		return nil
	case "r":
		a.flash = sFaint.Render("refreshing…")
		return tea.Batch(a.refreshTasks(true), a.refreshPRs(), a.refreshAgents())
	}
	switch a.tab {
	case TabTasks:
		return a.taskKey(k)
	case TabToday:
		return a.listKey(&a.today.list, k)
	case TabAgents:
		if k == "X" {
			return a.cleanupMerged()
		}
		return a.agentKey(k)
	case TabPRs:
		return a.prKey(k)
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
	// Acting on a row pins the cursor to it, so it follows the task when
	// the task moves to another group.
	switch a.tab {
	case TabTasks:
		a.tasks.list.touched = true
	case TabToday:
		a.today.list.touched = true
	}
	src := a.source(t.Source)
	switch k {
	case "s", "c", "a", "enter", "shift+enter":
		if src == nil {
			a.flash = sYellow.Render(t.Key + ": " + t.Source + " is not set up")
			return nil
		}
	}
	switch k {
	case "o":
		return a.openURL(t.URL)
	case "p":
		if p, ok := a.taskPR(t); ok {
			return a.openURL(p.URL)
		}
		a.flash = sFaint.Render("no PR for " + t.Key + " · branch names with the task ID link them")
	case "s":
		return a.openStatusPicker(t, src)
	case "c":
		a.mode, a.input, a.commentTask = modeComment, "", t.Ref()
	case "a":
		return a.toggleAssign(t, src)
	case "enter", "shift+enter":
		if a.launching[t.Ref()] {
			a.flash = sYellow.Render("already starting " + t.Key + "…")
			return nil
		}
		if k == "enter" {
			return a.enterTask(t)
		}
		a.openStart(t)
		return a.scanRepos(false)
	}
	return nil
}

// selectedTask returns the task under the cursor in the current view.
func (a *App) selectedTask() (task.Task, bool) {
	var r row
	var ok bool
	switch a.tab {
	case TabTasks:
		r, ok = a.tasks.list.selected()
	case TabToday:
		r, ok = a.today.list.selected()
	}
	if !ok {
		return task.Task{}, false
	}
	return a.taskByRef(r.key)
}

// taskByRef finds a task by its task.Ref.
func (a *App) taskByRef(ref string) (task.Task, bool) {
	for _, t := range a.cache.Tasks {
		if t.Ref() == ref {
			return t, true
		}
	}
	return task.Task{}, false
}

// openURL opens a link in the browser.
func (a *App) openURL(url string) tea.Cmd {
	if url == "" {
		return nil
	}
	return a.runCmd(run.Cmd{Name: "open", Args: []string{url}})
}

func runCmd(name string, args ...string) run.Cmd { return run.Cmd{Name: name, Args: args} }

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
	v.WindowTitle = "Huddle"
	return v
}

const detailLines = 4

// Render returns the screen as a string.
func (a *App) Render() string {
	sub, body, detail := a.viewParts()
	subLines := strings.Split(sub, "\n")
	bodyH := a.height - 2 - len(subLines) - 1 - (detailLines + 1) - 1
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
		t, _ := a.taskByRef(a.commentTask)
		flash = sBold.Render("comment "+t.Key+": ") + a.input + "▏" + sFaint.Render("  enter send · esc cancel")
	}
	// Last line: messages on the left, the help key on the right.
	help := sFaint.Render("? keys")
	flash = ansi.Truncate(flash, max(0, a.width-ansi.StringWidth(help)-2), "…")
	b.WriteString(flash + strings.Repeat(" ", max(1, a.width-ansi.StringWidth(flash)-ansi.StringWidth(help))) + help)
	switch a.mode {
	case modeHelp:
		lines := a.renderHelp()
		return overlay(b.String(), lines, max(0, (a.width-helpWidth)/2), max(1, (a.height-len(lines))/2))
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

func (a *App) viewParts() (sub string, body func(h int) string, detail []string) {
	now := a.now()
	switch a.tab {
	case TabToday:
		sub = a.today.subHeader(now)
		body = func(h int) string { return a.today.list.render(a.bodyWidth(), h) }
		if t, ok := a.selectedTask(); ok {
			detail = taskDetail(t, now)
		}
	case TabTasks:
		sub = a.tasks.subHeader(a.cache.Tasks)
		body = func(h int) string { return a.tasks.list.render(a.bodyWidth(), h) }
		if t, ok := a.selectedTask(); ok {
			detail = taskDetail(t, now)
		}
	case TabPRs:
		var mine, review int
		for _, p := range a.cache.PRs {
			switch p.Kind {
			case github.Mine:
				mine++
			case github.ReviewRequested:
				review++
			}
		}
		sub = sFaint.Render(fmt.Sprintf("%d mine · %d review requested", mine, review))
		if a.prErr != nil {
			sub += sFaint.Render(" · ") + sRed.Render(a.prErr.Error())
		}
		body = func(h int) string { return a.prs.list.render(a.bodyWidth(), h) }
		if p, ok := a.selectedPR(); ok {
			detail = prDetail(p)
		}
	case TabAgents:
		sub = sFaint.Render(plural(len(a.state.Agents), "agent") + " started by huddle")
		body = func(h int) string { return a.agents.list.render(a.bodyWidth(), h) }
		if ag, ok := a.selectedAgent(); ok {
			detail = agentDetail(ag, a.agentInfo[ag.BlockID])
		}
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
	right := a.counters() + a.syncLabel() + "  " + sBold.Render("huddle")
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
