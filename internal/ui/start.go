package ui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/slavafyi/huddle/internal/launch"
	"github.com/slavafyi/huddle/internal/rex"
	"github.com/slavafyi/huddle/internal/store"
	"github.com/slavafyi/huddle/internal/task"
	"github.com/slavafyi/huddle/internal/workspace"
)

// panelWidth is the Start panel's width, border included.
const panelWidth = 54

// promptLines is the height of the prompt box.
const promptLines = 4

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
	fixedRepo  bool             // the task names its repo: no picker
	noClone    string           // why there is no repo, when fixed
	repoFilter string
	repoIdx    int
	branch     string
	agents     []string
	agentIdx   int
	prompt     string
	autoPrompt string // the prompt huddle filled in; replaced on workspace change while unedited
	promptFor  func(workspace.Kind) string
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
	cfg, r := a.deps.Config, a.rules[t.Source]
	v := templateVars(t)
	p := &startPanel{
		task:   t,
		branch: workspace.Expand(cmp.Or(r.BranchTemplate, cfg.BranchTemplate), v),
		agents: cfg.AgentNames(),
	}
	if r.RepoInList {
		p.fixedRepo = true
		if repo, ok := workspace.FindRemote(a.cache.Repos, t.ListID); ok {
			p.repos = []workspace.Repo{repo}
		} else {
			p.noClone = "no local clone of " + t.ListID + " in repo_roots"
		}
	} else {
		p.repos = workspace.RankRepos(a.cache.Repos, t.Project, a.state.RecentRepos[t.Project])
	}
	p.kind = workspace.Checkout
	if task.WantsScratch(t, cfg.ScratchTypes, cfg.ScratchPrefixes) {
		p.kind = workspace.Scratch
	}
	p.promptFor = func(k workspace.Kind) string { return a.defaultPrompt(t, k) }
	p.prompt = p.promptFor(p.kind)
	p.autoPrompt = p.prompt
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

// defaultPrompt is the prompt for t in a workspace of kind k.
func (a *App) defaultPrompt(t task.Task, k workspace.Kind) string {
	r := a.rules[t.Source]
	tmpl := cmp.Or(r.PromptTemplate, a.deps.Config.PromptTemplate)
	if k == workspace.Scratch && r.ScratchPromptTemplate != "" {
		tmpl = r.ScratchPromptTemplate
	}
	return workspace.Expand(tmpl, templateVars(t))
}

// templateVars are a task's values for the branch and prompt templates.
func templateVars(t task.Task) workspace.Vars {
	_, num, _ := strings.Cut(t.ID, "#")
	return workspace.Vars{ID: t.Key, FullID: t.ID, Number: num, Title: t.Name, URL: t.URL}
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
	if p.kind != workspace.Scratch && !p.fixedRepo {
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
		if p.prompt == p.autoPrompt && p.promptFor != nil {
			p.prompt = p.promptFor(p.kind)
			p.autoPrompt = p.prompt
		}
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
		Kind: p.kind, Branch: strings.TrimSpace(p.branch), TaskKey: t.FolderKey(), Slug: workspace.Slug(t.Name),
		WorktreeRoot: cfg.WorktreeRoot, ScratchRoot: cfg.ScratchRoot,
	}
	if p.kind != workspace.Scratch {
		r, ok := p.repo()
		if !ok {
			return launch.Plan{}, cmp.Or(p.noClone, "pick a repo")
		}
		wp.Repo, wp.BaseBranch = r, cfg.BaseBranch(r.Name)
		if p.kind == workspace.Worktree && wp.Branch == "" {
			return launch.Plan{}, "branch name is empty"
		}
	}
	prompt := strings.TrimSpace(p.prompt)
	if prompt == "" {
		prompt = a.defaultPrompt(t, p.kind)
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

// renderStart draws the panel in at most height rows. When space is short
// the prompt box shrinks first, then blank lines go; Launch always shows.
func (a *App) renderStart(height int) []string {
	room := max(1, height-2)
	var lines []string
	for rows := promptLines; rows >= 1; rows-- {
		if lines = a.startLines(rows); len(lines) <= room {
			break
		}
	}
	if len(lines) > room {
		lines = slices.DeleteFunc(lines, func(l string) bool { return l == "" })
	}
	if len(lines) > room {
		lines = append(lines[:room-1], lines[len(lines)-1])
	}
	return titledBox(lines, panelWidth, "Start")
}

// startLines are the panel's lines with a prompt box rows tall.
func (a *App) startLines(rows int) []string {
	p, t := a.start, a.start.task
	inner := panelWidth - 4
	const labelW = 10
	// label is a field's label column: bold with › while focused.
	label := func(f field, s string) string {
		if p.field == f {
			return sBold.Render("› " + fmt.Sprintf("%-*s", labelW, s))
		}
		return sFaint.Render("  " + fmt.Sprintf("%-*s", labelW, s))
	}
	fixed := func(s string) string { return sFaint.Render("  " + fmt.Sprintf("%-*s", labelW, s)) }
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
	rule := func(name string) string {
		return sFaint.Render(name + " " + strings.Repeat("─", inner-len(name)-1))
	}
	indent := strings.Repeat(" ", 2+labelW)

	// Title: wrapped under itself, up to 3 lines.
	key := ansi.StringWidth(t.Key) + 1
	title := strings.Split(ansi.Wrap(t.Name, inner-key, ""), "\n")
	if len(title) > 3 {
		title = title[:3]
		title[2] = ansi.Truncate(title[2], inner-key-1, "") + "…"
	}
	lines := []string{sCyan.Render(t.Key) + " " + sBold.Render(title[0])}
	for _, l := range title[1:] {
		lines = append(lines, strings.Repeat(" ", key)+sBold.Render(l))
	}
	lines = append(lines, sFaint.Render(t.Project+" · "+t.Status))

	// WHERE
	rex := fixed("Rex") + sBold.Render(t.Project) + sFaint.Render(" › new tab")
	if p.session == "" {
		rex += sFaint.Render(" · session created")
	}
	lines = append(lines, "", rule("WHERE"), rex,
		label(fWorkspace, "Workspace")+opt(p.kind == workspace.Worktree, "worktree")+" "+opt(p.kind == workspace.Checkout, "checkout")+" "+opt(p.kind == workspace.Scratch, "scratch"))
	switch {
	case p.kind == workspace.Scratch:
		dir := workspace.Plan{Kind: workspace.Scratch, ScratchRoot: a.deps.Config.ScratchRoot, TaskKey: t.FolderKey(), Slug: workspace.Slug(t.Name)}.Dir()
		parent := shortHome(filepath.Dir(dir)) + "/"
		if w := ansi.StringWidth(parent); w > inner-len(indent) {
			parent = "…" + ansi.TruncateLeft(parent, w-(inner-len(indent)-1), "")
		}
		// The folder name gets a whole line: it is often long.
		lines = append(lines, fixed("Folder")+sFaint.Render(parent), "  "+filepath.Base(dir))
	case p.fixedRepo:
		if r, ok := p.repo(); ok {
			lines = append(lines, fixed("Repo")+r.Name)
		} else {
			lines = append(lines, fixed("Repo")+sRed.Render("none"), "  "+sRed.Render(p.noClone))
		}
	default:
		rs := p.filtered()
		sel := min(p.repoIdx, max(0, len(rs)-1))
		switch {
		case p.field == fRepo || p.repoFilter != "":
			lines = append(lines, label(fRepo, "Repo")+`"`+p.repoFilter+`"`+caret(fRepo))
			if len(rs) == 0 {
				lines = append(lines, indent+sRed.Render("no repo matches"))
			}
			start := max(0, min(sel-1, len(rs)-4))
			for i := start; i < len(rs) && i < start+4; i++ {
				if i == sel {
					lines = append(lines, indent+sBold.Render("› "+rs[i].Name))
				} else {
					lines = append(lines, indent+sFaint.Render("  "+rs[i].Name))
				}
			}
		case len(rs) == 0:
			lines = append(lines, label(fRepo, "Repo")+sRed.Render("no repos in repo_roots"))
		default:
			row := label(fRepo, "Repo") + rs[sel].Name
			if n := len(rs) - 1; n > 0 {
				row += sFaint.Render(" +" + strconv.Itoa(n))
			}
			lines = append(lines, row)
		}
	}
	if p.kind == workspace.Worktree {
		// A long branch shows its end, where you type.
		b := p.branch + caret(fBranch)
		if w, room := ansi.StringWidth(b), inner-len(indent); w > room {
			b = "…" + ansi.TruncateLeft(b, w-room+1, "")
		}
		lines = append(lines, label(fBranch, "Branch")+b)
	}

	// AGENT: 3 per row; the › marks the section while focused.
	head := rule("AGENT")
	if p.field == fAgent {
		head = sBold.Render("› AGENT") + sFaint.Render(" "+strings.Repeat("─", inner-8))
	}
	lines = append(lines, "", head)
	for i := 0; i < len(p.agents); i += 3 {
		row := "  "
		for j := i; j < min(i+3, len(p.agents)); j++ {
			cell := opt(j == p.agentIdx, p.agents[j])
			row += cell + strings.Repeat(" ", max(1, 14-ansi.StringWidth(cell)))
		}
		lines = append(lines, row)
	}

	// PROMPT: a thin box, 4 lines; a long prompt keeps its end in view.
	head = rule("PROMPT")
	border := sFaint
	if p.field == fPrompt {
		head = sBold.Render("› PROMPT") + sFaint.Render(" "+strings.Repeat("─", inner-9))
		border = lipgloss.NewStyle()
	}
	lines = append(lines, "", head, "  "+border.Render("┌"+strings.Repeat("─", inner-4)+"┐"))
	w := inner - 6
	wrapped := strings.Split(ansi.Wrap(p.prompt+caret(fPrompt), w, ""), "\n")
	if len(wrapped) > rows {
		wrapped = append([]string{sFaint.Render("…")}, wrapped[len(wrapped)-rows+1:]...)[:rows]
	}
	for i := range rows {
		l := ""
		if i < len(wrapped) {
			l = wrapped[i]
		}
		lines = append(lines, "  "+border.Render("│")+" "+l+strings.Repeat(" ", max(0, w-ansi.StringWidth(l)))+" "+border.Render("│"))
	}
	lines = append(lines, "  "+border.Render("└"+strings.Repeat("─", inner-4)+"┘"))
	check := func(f field, on bool, s string) string {
		box := "[ ] "
		if on {
			box = "[x] "
		}
		if p.field == f {
			return sBold.Render("› " + box + s)
		}
		return "  " + box + s
	}
	if p.showMove {
		lines = append(lines, check(fMove, p.moveStatus, "Move to "+a.deps.Config.StatusInProgress))
	}
	if p.showAssign {
		lines = append(lines, check(fAssign, p.assign, "Assign to me")+sFaint.Render(" (now: "+t.With()+")"))
	}
	lines = append(lines, "", sMagenta.Reverse(true).Bold(true).Render(" Launch ⏎ "))
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, inner, "…")
	}
	return lines
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
