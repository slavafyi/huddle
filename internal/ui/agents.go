package ui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/slavafyi/cuddle/internal/rex"
	"github.com/slavafyi/cuddle/internal/store"
)

// agentInfo is what Rex says about an agent's tab.
type agentInfo struct {
	known  bool
	alive  bool
	status rex.Status
	line   string
}

type (
	// agentsMsg has fresh info by block ID. Agents whose session Rex could
	// not list are left out, so they keep their last known state.
	agentsMsg struct {
		info map[string]agentInfo
		err  error
	}
	agentEventMsg struct {
		ev rex.Event
		ch <-chan rex.Event
	}
	watchMsg struct {
		ch  <-chan rex.Event
		err error
	}
	closedMsg struct {
		block string
		err   error
	}
	focusedMsg struct {
		agent store.Agent
		err   error
	}
)

// stateLabel returns the short label for an agent's state.
func stateLabel(i agentInfo) (string, lipgloss.Style) {
	switch {
	case !i.known:
		return "…", sFaint
	case !i.alive:
		return "closed", sFaint
	}
	switch i.status.State {
	case "working":
		return "● working", sYellow
	case "blocked":
		return "! needs input", sRed
	case "done":
		return "✓ done", sGreen
	case "error":
		return "✗ error", sRed
	}
	return "idle", sFaint
}

// rank orders states for picking a task's most important agent.
func rank(i agentInfo) int {
	if !i.known || !i.alive {
		return 0
	}
	switch i.status.State {
	case "blocked":
		return 5
	case "working":
		return 4
	case "error":
		return 3
	case "done":
		return 2
	}
	return 1
}

// agentLookup gives each task's most important live agent.
type agentLookup struct {
	byTask map[string]agentInfo
}

func (a *App) lookup() agentLookup {
	l := agentLookup{byTask: map[string]agentInfo{}}
	for _, ag := range a.state.Agents {
		i := a.agentInfo[ag.BlockID]
		if cur, ok := l.byTask[ag.TaskID]; !ok || rank(i) > rank(cur) {
			l.byTask[ag.TaskID] = i
		}
	}
	return l
}

// badge is shown after a task title in the Tasks view.
func (l agentLookup) badge(taskID string) string {
	i, ok := l.byTask[taskID]
	if !ok || rank(i) == 0 {
		return ""
	}
	label, st := stateLabel(i)
	return "  " + st.Render(label)
}

// state is the Today view's agent column.
func (l agentLookup) state(taskID string) string {
	i, ok := l.byTask[taskID]
	if !ok || rank(i) == 0 {
		return ""
	}
	switch i.status.State {
	case "working":
		return sYellow.Render("● agent working")
	case "blocked":
		return sRed.Render("! needs input")
	case "done":
		return sGreen.Render("✓ agent done")
	case "error":
		return sRed.Render("✗ agent error")
	}
	return sFaint.Render("agent idle")
}

// refreshAgents asks Rex which agent tabs still exist and their state.
func (a *App) refreshAgents() tea.Cmd {
	if a.deps.Rex == nil || len(a.state.Agents) == 0 || a.loadingAgents {
		return nil
	}
	a.loadingAgents = true
	agents, rx := append([]store.Agent(nil), a.state.Agents...), a.deps.Rex
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		blocks := map[string]map[string]bool{}
		failed := map[string]bool{}
		out := agentsMsg{info: map[string]agentInfo{}}
		for _, ag := range agents {
			if _, ok := blocks[ag.SessionID]; !ok && !failed[ag.SessionID] {
				b, err := rx.Blocks(ctx, ag.SessionID)
				if err != nil {
					failed[ag.SessionID], out.err = true, err
				}
				blocks[ag.SessionID] = b
			}
			if failed[ag.SessionID] {
				continue // keep the last known state
			}
			i := agentInfo{known: true, alive: blocks[ag.SessionID][ag.BlockID]}
			if i.alive {
				i.status, _ = rx.Status(ctx, ag.SessionID, ag.BlockID)
				i.line, _ = rx.LastLine(ctx, ag.SessionID, ag.BlockID)
			}
			out.info[ag.BlockID] = i
		}
		return out
	}
}

// startWatch starts the Rex event stream.
func (a *App) startWatch() tea.Cmd {
	if a.deps.Watch == nil {
		return nil
	}
	w := a.deps.Watch
	return func() tea.Msg {
		ch, err := w(context.Background())
		return watchMsg{ch: ch, err: err}
	}
}

func waitEvent(ch <-chan rex.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil // stream ended; the periodic refresh keeps going
		}
		return agentEventMsg{ev: ev, ch: ch}
	}
}

func (a *App) onAgentEvent(m agentEventMsg) tea.Cmd {
	if i, ok := a.agentInfo[m.ev.BlockID]; ok || a.isAgent(m.ev.BlockID) {
		i.known = true
		switch {
		case m.ev.Closed:
			i.alive = false
		case m.ev.HasStatus:
			i.alive, i.status = true, m.ev.Status
		}
		a.agentInfo[m.ev.BlockID] = i
		a.rebuild()
	}
	if m.ch == nil {
		return nil
	}
	return waitEvent(m.ch)
}

func (a *App) isAgent(block string) bool {
	for _, ag := range a.state.Agents {
		if ag.BlockID == block {
			return true
		}
	}
	return false
}

// agentView is the Agents tab.
type agentView struct {
	list list
}

func newAgentView() *agentView {
	return &agentView{list: list{
		cols:  []column{{"TASK", 10}, {"TITLE", 0}, {"SESSION", 18}, {"STATE", 14}, {"AGE", 5}},
		empty: "no agents yet · press enter on a task to start one",
	}}
}

var agentGroups = []struct {
	name  string
	match func(agentInfo) bool
}{
	{"Needs input", func(i agentInfo) bool { return i.alive && i.status.State == "blocked" }},
	{"Working", func(i agentInfo) bool { return i.alive && i.status.State == "working" }},
	{"Error", func(i agentInfo) bool { return i.alive && i.status.State == "error" }},
	{"Done", func(i agentInfo) bool { return i.alive && i.status.State == "done" }},
	{"Idle", func(i agentInfo) bool {
		return i.alive && i.status.State != "blocked" && i.status.State != "working" && i.status.State != "error" && i.status.State != "done"
	}},
	{"Closed", func(i agentInfo) bool { return i.known && !i.alive }},
	{"Checking…", func(i agentInfo) bool { return !i.known }},
}

func (v *agentView) rebuild(agents []store.Agent, info map[string]agentInfo, now time.Time) {
	var secs []section
	for _, g := range agentGroups {
		s := section{title: g.name}
		for i := len(agents) - 1; i >= 0; i-- {
			ag := agents[i]
			in := info[ag.BlockID]
			if !g.match(in) {
				continue
			}
			label, st := stateLabel(in)
			s.rows = append(s.rows, row{key: ag.BlockID, cells: []string{
				sFaint.Render(ag.TaskKey), ag.Title, ag.Client, st.Render(label), sFaint.Render(ago(now.Sub(ag.Started))),
			}})
		}
		if len(s.rows) > 0 {
			secs = append(secs, s)
		}
	}
	v.list.setSections(secs)
}

func (a *App) selectedAgent() (store.Agent, bool) {
	r, ok := a.agents.list.selected()
	if !ok {
		return store.Agent{}, false
	}
	for _, ag := range a.state.Agents {
		if ag.BlockID == r.key {
			return ag, true
		}
	}
	return store.Agent{}, false
}

func (a *App) agentKey(k string) tea.Cmd {
	l := &a.agents.list
	switch k {
	case "j", "down":
		l.move(1)
	case "k", "up":
		l.move(-1)
	case "enter":
		ag, ok := a.selectedAgent()
		if !ok {
			return nil
		}
		if !a.agentInfo[ag.BlockID].alive {
			a.flash = sFaint.Render("that tab is closed · x to forget it")
			return nil
		}
		rx := a.deps.Rex
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return focusedMsg{agent: ag, err: rx.Focus(ctx, ag.SessionID, ag.WindowID)}
		}
	case "x":
		ag, ok := a.selectedAgent()
		if !ok {
			return nil
		}
		alive := a.agentInfo[ag.BlockID].alive
		text := "forget closed tab " + ag.TaskKey + "?"
		if alive {
			text = "close tab " + ag.TaskKey + "? the folder is kept"
		}
		a.ask(text, func() tea.Cmd { return a.closeAgent(ag, alive) })
	}
	return nil
}

// closeAgent closes the tab (when open) and forgets the agent.
func (a *App) closeAgent(ag store.Agent, alive bool) tea.Cmd {
	if !alive {
		return func() tea.Msg { return closedMsg{block: ag.BlockID} }
	}
	rx := a.deps.Rex
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return closedMsg{block: ag.BlockID, err: rx.CloseWindow(ctx, ag.SessionID, ag.WindowID)}
	}
}

func (a *App) onClosed(m closedMsg) tea.Cmd {
	if m.err != nil {
		a.flash = sRed.Render(m.err.Error())
		return nil
	}
	a.state.RemoveAgent(m.block)
	delete(a.agentInfo, m.block)
	a.rebuild()
	a.flash = sGreen.Render("closed")
	return a.saveState()
}

// ask shows a y/n question in the flash line.
func (a *App) ask(text string, yes func() tea.Cmd) {
	a.mode, a.confirmText, a.confirmYes = modeConfirm, text, yes
}

func (a *App) confirmKey(k string) tea.Cmd {
	yes := a.confirmYes
	a.mode, a.confirmText, a.confirmYes = modeNormal, "", nil
	a.flash = ""
	if k == "y" && yes != nil {
		return yes()
	}
	return nil
}

func agentDetail(ag store.Agent, i agentInfo) []string {
	label, st := stateLabel(i)
	where := ag.Workspace + " · " + shortHome(ag.Dir)
	if ag.Branch != "" {
		where += " · " + ag.Branch
	}
	last := sFaint.Render(i.line)
	if i.status.State == "blocked" && i.status.Msg != "" {
		last = sRed.Render(i.status.Msg)
	}
	return []string{
		sBold.Render(ag.TaskKey + " " + ag.Title),
		where,
		fmt.Sprintf("%s · %s · session %s", ag.Agent, st.Render(label), ag.Client),
		last,
	}
}

// counters is the header's "● 1 working  ! 1 waiting".
func (a *App) counters() string {
	var working, waiting int
	for _, ag := range a.state.Agents {
		i := a.agentInfo[ag.BlockID]
		if !i.alive {
			continue
		}
		switch i.status.State {
		case "working":
			working++
		case "blocked":
			waiting++
		}
	}
	out := ""
	if working > 0 {
		out += sYellow.Render("●") + fmt.Sprintf(" %d working  ", working)
	}
	if waiting > 0 {
		out += sRed.Render("!") + fmt.Sprintf(" %d waiting  ", waiting)
	}
	return out
}
