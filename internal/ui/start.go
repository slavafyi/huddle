package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/slavafyi/cuddle/internal/clickup"
	"github.com/slavafyi/cuddle/internal/launch"
	"github.com/slavafyi/cuddle/internal/rex"
	"github.com/slavafyi/cuddle/internal/store"
	"github.com/slavafyi/cuddle/internal/workspace"
)

// panelWidth is the Start panel's width, border included.
const panelWidth = 54

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
	task       clickup.Task
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
	launchedMsg struct {
		res  launch.Result
		plan launch.Plan
		err  error
	}
	// enterMsg is the answer to "does this task have a live tab?".
	enterMsg struct {
		taskID string
		agent  *store.Agent // nil: no live tab, open the panel
		err    error        // focus error, if any
	}
)

// enterTask focuses the newest live agent tab of a task, or opens the
// Start panel when there is none.
func (a *App) enterTask(t clickup.Task) tea.Cmd {
	var agents []store.Agent
	for _, ag := range a.state.Agents {
		if ag.TaskID == t.ID {
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
			if err != nil || !blocks[ag.BlockID] {
				continue
			}
			return enterMsg{taskID: t.ID, agent: &ag, err: rx.Focus(ctx, ag.SessionID, ag.WindowID)}
		}
		return enterMsg{taskID: t.ID}
	}
}

func (a *App) onEnter(m enterMsg) tea.Cmd {
	if m.agent == nil {
		if t, ok := a.taskByID(m.taskID); ok {
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

func (a *App) openStart(t clickup.Task) {
	cfg := a.deps.Config
	me := a.cache.Me.ID
	p := &startPanel{
		task:   t,
		repos:  workspace.RankRepos(a.cache.Repos, t.ListName, a.state.RecentRepos[t.ListName]),
		branch: workspace.Expand(cfg.BranchTemplate, workspace.Vars{ID: t.Key(), Title: t.Name, URL: t.URL}),
		agents: cfg.AgentNames(),
		prompt: workspace.Expand(cfg.PromptTemplate, workspace.Vars{ID: t.Key(), Title: t.Name, URL: t.URL}),
	}
	if clickup.WantsScratch(t, cfg.ScratchTypes, cfg.ScratchPrefixes) {
		p.kind = workspace.Scratch
	}
	if t.Status == "to plan" || t.Status == "to start" {
		p.showMove, p.moveStatus = true, true
	}
	if !t.AssignedTo(me) {
		p.showAssign, p.assign = true, true
	}
	p.session = a.state.ClientSessions[t.ListName]
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
	case "ctrl+s":
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
		switch p.field {
		case fWorkspace:
			p.kind = workspace.Kind(cycle(int(p.kind), d, 3))
		case fRepo:
			p.repoIdx = cycle(p.repoIdx, d, len(p.filtered()))
		case fAgent:
			p.agentIdx = cycle(p.agentIdx, d, len(p.agents))
		}
		return nil
	case "space":
		switch p.field {
		case fMove:
			p.moveStatus = !p.moveStatus
			return nil
		case fAssign:
			p.assign = !p.assign
			return nil
		}
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
		prompt = workspace.Expand(cfg.PromptTemplate, workspace.Vars{ID: t.Key(), Title: t.Name, URL: t.URL})
	}
	agent := p.agents[p.agentIdx]
	lp := launch.Plan{
		Task: t, Workspace: wp, AgentName: agent, Argv: launch.Argv(cfg.Agents, agent, prompt),
		SessionID: p.session, TabName: t.Key() + " " + ansi.Truncate(t.Name, 40, "…"),
	}
	if p.showMove && p.moveStatus {
		lp.SetStatus = cfg.StatusInProgress
	}
	if p.showAssign && p.assign {
		lp.AssignMe = a.cache.Me.ID
	}
	return lp, ""
}

func (a *App) launch() tea.Cmd {
	lp, problem := a.plan()
	if problem != "" {
		a.flash = sRed.Render(problem)
		return nil
	}
	a.mode, a.start = modeNormal, nil
	a.flash = sFaint.Render("starting " + lp.Task.Key() + "…")
	d := launch.Deps{Runner: a.deps.Runner, Rex: a.deps.Rex, ClickUp: a.deps.ClickUp, Now: a.deps.Now}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		res, err := launch.Run(ctx, d, lp)
		return launchedMsg{res: res, plan: lp, err: err}
	}
}

func (a *App) onLaunched(m launchedMsg) tea.Cmd {
	if m.err != nil {
		a.flash = sRed.Render(m.err.Error())
		return nil
	}
	ag := m.res.Agent
	a.state.AddAgent(ag)
	a.agentInfo[ag.BlockID] = agentInfo{known: true, alive: true}
	if ag.Repo != "" {
		a.state.RememberRepo(ag.Client, ag.Repo)
	}
	// Show the ClickUp changes at once; the next refresh confirms them.
	if t, ok := a.taskByID(ag.TaskID); ok {
		failed := strings.Join(m.res.Warnings, " ")
		if m.plan.SetStatus != "" && !strings.Contains(failed, "move to") {
			t.Status = m.plan.SetStatus
			if st, ok := a.statusInfo(t.ListID, t.Status); ok {
				t.StatusType, t.StatusOrder = st.Type, st.Order
			}
		}
		if m.plan.AssignMe != 0 && !strings.Contains(failed, "assign") && !t.AssignedTo(m.plan.AssignMe) {
			t.Assignees = append(append([]clickup.User(nil), t.Assignees...), a.cache.Me)
		}
		a.putTask(t)
	}
	a.flash = sGreen.Render("started " + ag.TaskKey + " in " + ag.Client)
	if len(m.res.Warnings) > 0 {
		a.flash += sYellow.Render(" · " + strings.Join(m.res.Warnings, " · "))
	}
	return tea.Batch(a.saveState(), a.saveCache(), a.refreshAgents())
}

// statusInfo finds a status of a list among the statuses seen so far.
func (a *App) statusInfo(listID, name string) (clickup.Status, bool) {
	for _, s := range a.statuses[listID] {
		if s.Name == name {
			return s, true
		}
	}
	for _, t := range a.cache.Tasks {
		if t.Status == name {
			return clickup.Status{Name: name, Type: t.StatusType, Order: t.StatusOrder}, true
		}
	}
	return clickup.Status{}, false
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
		sCyan.Render(t.Key()) + " " + t.Name,
		sFaint.Render(t.ListName + " · " + t.Status),
		"",
		label(fWorkspace, "Workspace ←→"),
		"  " + opt(p.kind == workspace.Worktree, "worktree") + "  " + opt(p.kind == workspace.Checkout, "checkout") + "  " + opt(p.kind == workspace.Scratch, "scratch"),
	}
	if p.kind != workspace.Scratch {
		head := "Repo ←→ · type to filter"
		if p.repoFilter != "" {
			head = "Repo · filter: " + p.repoFilter + caret(fRepo)
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
	where := "new tab in session " + sBold.Render(t.ListName) + sFaint.Render(" (created if missing)")
	if p.session != "" {
		where = "new tab in session " + sBold.Render(t.ListName)
	}
	lines = append(lines, sFaint.Render("  Rex"), "  "+where)
	var agents []string
	for i, n := range p.agents {
		agents = append(agents, opt(i == p.agentIdx, n))
	}
	lines = append(lines, label(fAgent, "Agent ←→"), "  "+strings.Join(agents, "  "))
	lines = append(lines, label(fPrompt, "Prompt"), "  "+p.prompt+caret(fPrompt))
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
		lines = append(lines, label(fAssign, check(p.assign)+" Assign to me"+sFaint.Render(" (now: "+clickup.With(t, a.cache.Me.ID)+")")))
	}
	lines = append(lines, "", sMagenta.Reverse(true).Bold(true).Render(" Launch · ctrl-s ")+sFaint.Render("  esc close · tab next"))
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
