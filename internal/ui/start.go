package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/slavafyi/huddle/internal/launch"
	"github.com/slavafyi/huddle/internal/rex"
	"github.com/slavafyi/huddle/internal/store"
	"github.com/slavafyi/huddle/internal/task"
	"github.com/slavafyi/huddle/internal/workspace"
)

// panelWidth is the Start panel's width, border included.
const panelWidth = 54

// maxPromptLines is how many lines of the prompt the panel shows.
const maxPromptLines = 6

type field int

const (
	fWorkspace field = iota
	fRepo
	fBranch
	fAgent
	fPrompt
	fMove
	fAssign
)

// startPanel is the Start task panel.
type startPanel struct {
	task       task.Task
	field      field
	kind       workspace.Kind
	repos      []workspace.Repo // ranked
	repoFilter string
	repoIdx    int
	branch     string
	agents     []string
	agentIdx   int
	prompt     string
	showMove   bool
	moveStatus bool
	showAssign bool
	assign     bool
	session    string // stored session for the client, if it still exists
}

type (
	// openedMsg: the agent's tab is open (or opening failed).
	openedMsg struct {
		res  launch.Result
		plan launch.Plan
		err  error
	}
	// appliedMsg: the launch's task writes finished.
	appliedMsg struct {
		taskKey  string
		statusOp uint64
		assignOp uint64
		up       launch.Updates
	}
	// enterMsg is the answer to "does this task have a live tab?".
	enterMsg struct {
		ref      string       // task.Ref of the task
		agent    *store.Agent // nil: no live tab, open the panel
		err      error        // focus error, if any
		checkErr error        // Rex could not be asked; do nothing
	}
)

// enterTask focuses the newest live agent tab of a task, or opens the
// Start panel when there is none.
func (a *App) enterTask(t task.Task) tea.Cmd {
	var agents []store.Agent
	for _, ag := range a.state.Agents {
		if ag.TaskRef() == t.Ref() {
			agents = append(agents, ag)
		}
	}
	if len(agents) == 0 || a.deps.Rex == nil {
		a.openStart(t)
		return a.scanRepos(false)
	}
	rx := a.deps.Rex
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for i := len(agents) - 1; i >= 0; i-- {
			ag := agents[i]
			blocks, err := rx.Blocks(ctx, ag.SessionID)
			if err != nil {
				return enterMsg{ref: t.Ref(), checkErr: err}
			}
			if !blocks[ag.BlockID] {
				continue
			}
			return enterMsg{ref: t.Ref(), agent: &ag, err: rx.Focus(ctx, ag.SessionID, ag.WindowID)}
		}
		return enterMsg{ref: t.Ref()}
	}
}

func (a *App) onEnter(m enterMsg) tea.Cmd {
	if m.checkErr != nil {
		a.flash = sRed.Render("can't check the agent's tab: " + m.checkErr.Error())
		return nil
	}
	if m.agent == nil {
		if t, ok := a.taskByRef(m.ref); ok && a.mode == modeNormal {
			a.openStart(t)
		}
		return a.scanRepos(false)
	}
	switch {
	case m.err == nil:
		a.flash = sGreen.Render("→ " + m.agent.TaskKey + " in " + m.agent.Client)
	case errors.Is(m.err, rex.ErrRemoteControl):
		a.flash = sYellow.Render("tab is active in session " + m.agent.Client + " · " + m.err.Error())
	default:
		a.flash = sRed.Render(m.err.Error())
	}
	return nil
}

func (a *App) openStart(t task.Task) {
	cfg := a.deps.Config
	p := &startPanel{
		task:   t,
		repos:  workspace.RankRepos(a.cache.Repos, t.Project, a.state.RecentRepos[t.Project]),
		branch: workspace.Expand(cfg.BranchTemplate, workspace.Vars{ID: t.Key, Title: t.Name, URL: t.URL}),
		agents: cfg.AgentNames(),
		prompt: workspace.Expand(cfg.PromptTemplate, workspace.Vars{ID: t.Key, Title: t.Name, URL: t.URL}),
	}
	if task.WantsScratch(t, cfg.ScratchTypes, cfg.ScratchPrefixes) {
		p.kind = workspace.Scratch
	}
	if slices.Contains(a.rules[t.Source].Startable, t.Status) {
		p.showMove, p.moveStatus = true, true
	}
	if !t.Mine {
		p.showAssign, p.assign = true, true
	}
	p.session = a.state.ClientSessions[t.Project]
	a.start = p
	a.mode = modeStart
}

// filtered returns the repos matching the filter.
func (p *startPanel) filtered() []workspace.Repo {
	return workspace.FilterRepos(p.repos, p.repoFilter)
}

// repo returns the selected repo.
func (p *startPanel) repo() (workspace.Repo, bool) {
	rs := p.filtered()
	if len(rs) == 0 {
		return workspace.Repo{}, false
	}
	return rs[min(p.repoIdx, len(rs)-1)], true
}

// fields lists the fields shown for the current choices.
func (p *startPanel) fields() []field {
	fs := []field{fWorkspace}
	if p.kind != workspace.Scratch {
		fs = append(fs, fRepo)
	}
	if p.kind == workspace.Worktree {
		fs = append(fs, fBranch)
	}
	fs = append(fs, fAgent, fPrompt)
	if p.showMove {
		fs = append(fs, fMove)
	}
	if p.showAssign {
		fs = append(fs, fAssign)
	}
	return fs
}

func (p *startPanel) moveField(d int) {
	fs := p.fields()
	i := 0
	for j, f := range fs {
		if f == p.field {
			i = j
		}
	}
	p.field = fs[((i+d)%len(fs)+len(fs))%len(fs)]
}

func cycle(i, d, n int) int {
	if n == 0 {
		return 0
	}
	return ((i+d)%n + n) % n
}

func (a *App) startKey(msg tea.KeyPressMsg) tea.Cmd {
	p := a.start
	k := msg.String()
	switch k {
	case "esc":
		a.mode, a.start = modeNormal, nil
		return nil
	case "enter", "ctrl+s": // all fields are one line, so enter launches
		return a.launch()
	case "tab", "down":
		p.moveField(1)
		return nil
	case "shift+tab", "up":
		p.moveField(-1)
		return nil
	case "left", "right":
		d := 1
		if k == "left" {
			d = -1
		}
		p.change(d)
		return nil
	}
	// space changes the value: next option, toggled checkbox, or next repo
	// (repo names have no spaces, so the filter doesn't need one).
	if k == "space" && (p.field.isChoice() || p.field == fRepo) {
		p.change(1)
		return nil
	}
	if p.field.isChoice() {
		return nil // no text to type here
	}
	switch k {
	case "backspace":
		switch p.field {
		case fRepo:
			p.repoFilter, p.repoIdx = dropLast(p.repoFilter), 0
		case fBranch:
			p.branch = dropLast(p.branch)
		case fPrompt:
			p.prompt = dropLast(p.prompt)
		}
		return nil
	}
	if msg.Text == "" {
		return nil
	}
	switch p.field {
	case fRepo:
		p.repoFilter += msg.Text
		p.repoIdx = 0
	case fBranch:
		p.branch += msg.Text
	case fPrompt:
		p.prompt += msg.Text
	}
	return nil
}

// isChoice reports whether a field has options instead of text.
func (f field) isChoice() bool {
	return f == fWorkspace || f == fAgent || f == fMove || f == fAssign
}

// change moves the current field's value by d (checkboxes toggle).
func (p *startPanel) change(d int) {
	switch p.field {
	case fWorkspace:
		p.kind = workspace.Kind(cycle(int(p.kind), d, 3))
	case fRepo:
		p.repoIdx = cycle(p.repoIdx, d, len(p.filtered()))
	case fAgent:
		p.agentIdx = cycle(p.agentIdx, d, len(p.agents))
	case fMove:
		p.moveStatus = !p.moveStatus
	case fAssign:
		p.assign = !p.assign
	}
}

func dropLast(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

// plan turns the panel into a launch plan.
func (a *App) plan() (launch.Plan, string) {
	p, cfg, t := a.start, a.deps.Config, a.start.task
	wp := workspace.Plan{
		Kind: p.kind, Branch: strings.TrimSpace(p.branch), TaskKey: t.KeyLower(), Slug: workspace.Slug(t.Name),
		WorktreeRoot: cfg.WorktreeRoot, ScratchRoot: cfg.ScratchRoot,
	}
	if p.kind != workspace.Scratch {
		r, ok := p.repo()
		if !ok {
			return launch.Plan{}, "pick a repo"
		}
		wp.Repo, wp.BaseBranch = r, cfg.BaseBranch(r.Name)
		if p.kind == workspace.Worktree && wp.Branch == "" {
			return launch.Plan{}, "branch name is empty"
		}
	}
	prompt := strings.TrimSpace(p.prompt)
	if prompt == "" {
		prompt = workspace.Expand(cfg.PromptTemplate, workspace.Vars{ID: t.Key, Title: t.Name, URL: t.URL})
	}
	agent := p.agents[p.agentIdx]
	lp := launch.Plan{
		Task: t, Workspace: wp, AgentName: agent, Argv: launch.Argv(cfg.Agents, agent, prompt),
		SessionID: p.session, TabName: t.Key + " " + ansi.Truncate(t.Name, 40, "…"),
	}
	if p.showMove && p.moveStatus {
		lp.SetStatus = a.rules[t.Source].MoveTo
	}
	if p.showAssign && p.assign {
		lp.AssignMe = a.cache.Me[t.Source]
	}
	return lp, ""
}

func (a *App) launch() tea.Cmd {
	lp, problem := a.plan()
	if problem != "" {
		a.flash = sRed.Render(problem)
		return nil
	}
	ref := lp.Task.Ref()
	if a.launching[ref] {
		a.flash = sYellow.Render("already starting " + lp.Task.Key + "…")
		return nil
	}
	a.lastStart[ref] = a.start
	a.mode, a.start = modeNormal, nil
	a.launching[ref] = true
	a.flash = sFaint.Render("starting " + lp.Task.Key + "…")
	d := launch.Deps{Runner: a.deps.Runner, Rex: a.deps.Rex, Source: a.source(lp.Task.Source), Now: a.deps.Now}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		res, err := launch.Open(ctx, d, lp)
		return openedMsg{res: res, plan: lp, err: err}
	}
}

// onOpened records the agent as soon as its tab exists (SPEC §4.4 step 3),
// then starts the task writes (step 4).
func (a *App) onOpened(m openedMsg) tea.Cmd {
	id := m.plan.Task.Ref()
	panel := a.lastStart[id]
	delete(a.launching, id)
	delete(a.lastStart, id)
	if m.err != nil {
		a.flash = sRed.Render(m.plan.Task.Key + ": " + m.err.Error())
		if a.mode == modeNormal && panel != nil {
			a.start, a.mode = panel, modeStart // this task's panel, with its edits
		}
		return nil
	}
	ag := m.res.Agent
	a.state.AddAgent(ag)
	a.agentInfo[ag.BlockID] = agentInfo{known: true, alive: true}
	if ag.Repo != "" {
		a.state.RememberRepo(ag.Client, ag.Repo)
	}
	a.flash = sGreen.Render("started " + ag.TaskKey + " in " + ag.Client)
	if len(m.res.Warnings) > 0 {
		a.flash += sYellow.Render(" · " + strings.Join(m.res.Warnings, " · "))
	}
	cmds := []tea.Cmd{a.saveState(), a.refreshAgents()}

	// Show the task changes at once; undo each one that fails.
	p, t := m.plan, m.plan.Task
	var statusOp, assignOp uint64
	if p.SetStatus != "" {
		cur, _ := a.taskByRef(t.Ref())
		st, ok := a.statusInfo(t, p.SetStatus)
		if !ok {
			st = task.Status{Name: p.SetStatus, Order: cur.StatusOrder, Done: cur.IsDone()}
		}
		statusOp = a.newOp(t.Ref(), "status", setStatus(st, a.now()), setStatus(currentStatus(cur), cur.Done))
	}
	if p.AssignMe != 0 {
		assignOp = a.newOp(t.Ref(), "assign", setAssigned(true), setAssigned(false))
	}
	if src := a.source(t.Source); src != nil && (statusOp != 0 || assignOp != 0) {
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			return appliedMsg{taskKey: t.Key, statusOp: statusOp, assignOp: assignOp, up: launch.Apply(ctx, src, p)}
		})
	}
	return tea.Batch(cmds...)
}

func (a *App) onApplied(m appliedMsg) tea.Cmd {
	if m.statusOp != 0 {
		a.finishOp(m.statusOp, m.up.StatusOK)
	}
	if m.assignOp != 0 {
		a.finishOp(m.assignOp, m.up.AssignOK)
	}
	if len(m.up.Warnings) > 0 {
		a.flash = sYellow.Render(m.taskKey + ": " + strings.Join(m.up.Warnings, " · "))
	}
	return tea.Batch(a.saveCache(), a.refreshAfterWrite())
}

// statusInfo finds a status of t's list among the statuses seen so far.
func (a *App) statusInfo(t task.Task, name string) (task.Status, bool) {
	for _, s := range a.statuses[task.Ref(t.Source, t.ListID)] {
		if s.Name == name {
			return s, true
		}
	}
	for _, o := range a.cache.Tasks {
		if o.Source == t.Source && o.Status == name {
			return currentStatus(o), true
		}
	}
	return task.Status{}, false
}

func (a *App) renderStart(height int) []string {
	p, t := a.start, a.start.task
	inner := panelWidth - 4
	label := func(f field, s string) string {
		if p.field == f {
			return sBold.Render("› " + s)
		}
		return sFaint.Render("  " + s)
	}
	opt := func(on bool, s string) string {
		if on {
			return sGreen.Render("●") + " " + s
		}
		return sFaint.Render("○ " + s)
	}
	caret := func(f field) string {
		if p.field == f {
			return "▏"
		}
		return ""
	}
	lines := []string{
		sBold.Render("Start task"),
		sCyan.Render(t.Key) + " " + t.Name,
		sFaint.Render(t.Project + " · " + t.Status),
		"",
		label(fWorkspace, "Workspace"),
		"  " + opt(p.kind == workspace.Worktree, "worktree") + "  " + opt(p.kind == workspace.Checkout, "checkout") + "  " + opt(p.kind == workspace.Scratch, "scratch"),
	}
	if p.kind != workspace.Scratch {
		head := "Repo"
		if p.repoFilter != "" {
			head = `Repo · "` + p.repoFilter + `"` + caret(fRepo)
		}
		lines = append(lines, label(fRepo, head))
		rs := p.filtered()
		if len(rs) == 0 {
			lines = append(lines, sRed.Render("  no repo matches"))
		}
		sel := min(p.repoIdx, max(0, len(rs)-1))
		start := max(0, min(sel-1, len(rs)-4))
		for i := start; i < len(rs) && i < start+4; i++ {
			if i == sel {
				lines = append(lines, "  "+sBold.Render("› "+rs[i].Name))
			} else {
				lines = append(lines, sFaint.Render("    "+rs[i].Name))
			}
		}
		if rest := len(rs) - (start + 4); rest > 0 {
			lines = append(lines, sFaint.Render("    +"+strconv.Itoa(rest)+" more"))
		}
	}
	if p.kind == workspace.Worktree {
		lines = append(lines, label(fBranch, "Branch"), "  "+p.branch+caret(fBranch))
	}
	if p.kind == workspace.Scratch {
		dir := workspace.Plan{Kind: workspace.Scratch, ScratchRoot: a.deps.Config.ScratchRoot, TaskKey: t.KeyLower(), Slug: workspace.Slug(t.Name)}.Dir()
		parent := shortHome(filepath.Dir(dir)) + "/"
		if w := ansi.StringWidth(parent); w > inner-2 {
			parent = "…" + ansi.TruncateLeft(parent, w-(inner-3), "")
		}
		lines = append(lines, sFaint.Render("  Folder"), "  "+sFaint.Render(parent), "  "+filepath.Base(dir))
	}
	where := "new tab in session " + sBold.Render(t.Project) + sFaint.Render(" (created if missing)")
	if p.session != "" {
		where = "new tab in session " + sBold.Render(t.Project)
	}
	lines = append(lines, sFaint.Render("  Rex"), "  "+where)
	var agents []string
	for i, n := range p.agents {
		agents = append(agents, opt(i == p.agentIdx, n))
	}
	lines = append(lines, label(fAgent, "Agent"), "  "+strings.Join(agents, "  "))
	lines = append(lines, label(fPrompt, "Prompt"))
	// Wrap the prompt so all of it shows; keep the end (where you type)
	// when it is very long.
	wrapped := strings.Split(ansi.Wrap(p.prompt+caret(fPrompt), inner-2, ""), "\n")
	if len(wrapped) > maxPromptLines {
		wrapped = append([]string{sFaint.Render("…")}, wrapped[len(wrapped)-maxPromptLines+1:]...)
	}
	for _, l := range wrapped {
		lines = append(lines, "  "+l)
	}
	check := func(on bool) string {
		if on {
			return "[x]"
		}
		return "[ ]"
	}
	if p.showMove {
		lines = append(lines, label(fMove, check(p.moveStatus)+" Move to "+a.deps.Config.StatusInProgress))
	}
	if p.showAssign {
		lines = append(lines, label(fAssign, check(p.assign)+" Assign to me"+sFaint.Render(" (now: "+t.With()+")")))
	}
	lines = append(lines, "", sMagenta.Reverse(true).Bold(true).Render(" Launch ⏎ "))
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, inner, "…")
	}
	if len(lines) > height-2 {
		lines = lines[:max(1, height-2)]
	}
	return box(lines, panelWidth)
}

// shortHome replaces the home folder with ~.
func shortHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// paste adds pasted text to the field being edited.
func (a *App) paste(text string) {
	switch a.mode {
	case modeComment:
		a.input += text
	case modeStart:
		switch p := a.start; p.field {
		case fRepo:
			p.repoFilter, p.repoIdx = p.repoFilter+text, 0
		case fBranch:
			p.branch += text
		case fPrompt:
			p.prompt += text
		}
	}
}
