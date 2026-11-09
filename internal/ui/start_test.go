package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slavafyi/huddle/internal/github"
	"github.com/slavafyi/huddle/internal/run"
	"github.com/slavafyi/huddle/internal/task"
	"github.com/slavafyi/huddle/internal/workspace"
)

func TestEnterOpensStartPanelWithDefaults(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2") // DEV-7700 Promo, Snowcap, to start, Coding, assigned to me
	e.press("enter")
	if e.app.mode != modeStart {
		t.Fatal("panel not open")
	}
	p := e.app.start
	if p.kind != workspace.Checkout {
		t.Errorf("kind = %v, want checkout by default", p.kind)
	}
	if r, _ := p.repo(); r.Name != "snowcap" {
		t.Errorf("first repo = %q (want client match first)", r.Name)
	}
	if p.branch != "feature/dev-7700-promo-checkout-personalisation" {
		t.Errorf("branch = %q", p.branch)
	}
	if !p.moveStatus || p.showAssign {
		t.Errorf("move=%v showAssign=%v", p.moveStatus, p.showAssign)
	}
	out := plain(e)
	for _, want := range []string{"Start", "DEV-7700", "● checkout", "snowcap", "type to filter", "Take a task DEV-7700 from CU", "Rex", "Snowcap › new tab", "[x] Move to in progress", "Launch ⏎", "┄┄┄"} {
		if !strings.Contains(out, want) {
			t.Errorf("panel missing %q:\n%s", want, out)
		}
	}
}

func TestResearchTaskDefaultsToScratch(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t4") // Research low stock notifications
	e.press("enter")
	if e.app.start.kind != workspace.Scratch {
		t.Errorf("kind = %v", e.app.start.kind)
	}
	if !strings.Contains(plain(e), "dev-7053-research-low-stock-notifications") || !strings.Contains(plain(e), "● scratch") {
		t.Errorf("scratch folder not shown:\n%s", plain(e))
	}
}

func TestLeadOnlyTaskOffersAssign(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t3") // pending, with Malcolm
	e.press("enter")
	p := e.app.start
	if !p.showAssign || !p.assign || p.moveStatus {
		t.Errorf("showAssign=%v assign=%v move=%v", p.showAssign, p.assign, p.moveStatus)
	}
}

func TestPanelEditing(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter")
	p := e.app.start
	if p.kind != workspace.Checkout {
		t.Errorf("kind = %v", p.kind)
	}
	e.press("tab", "s", "u", "b") // repo filter
	if r, _ := p.repo(); r.Name != "snowcap-subscriptions" {
		t.Errorf("filtered repo = %q", r.Name)
	}
	e.press("backspace", "backspace", "backspace", "down")
	if r, _ := p.repo(); r.Name == "snowcap" {
		t.Errorf("down should move to the next repo, still %q", r.Name)
	}
	e.press("esc")
	if e.app.mode != modeNormal || len(e.rex.calls) != 0 {
		t.Errorf("esc: mode=%v rex=%v", e.app.mode, e.rex.calls)
	}
}

func TestLaunchScratchAgent(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t4")
	e.press("enter", "ctrl+s")
	if e.app.mode != modeNormal {
		t.Fatal("panel still open")
	}
	dir := filepath.Join(e.scratch, "dev-7053-research-low-stock-notifications")
	want := "new-session Snowcap | " + dir + " | DEV-7053 Research low stock notifications | claude Take a task DEV-7053 from CU"
	if len(e.rex.calls) < 1 || e.rex.calls[0] != want {
		t.Fatalf("rex calls:\n%s\nwant:\n%s", strings.Join(e.rex.calls, "\n"), want)
	}
	if got := e.cu.writes; len(got) != 1 || got[0] != "status t4 in progress" {
		t.Errorf("clickup writes = %v", got)
	}
	if status(e, "t4") != "in progress" {
		t.Errorf("task not updated on screen: %q", status(e, "t4"))
	}
	st := e.stateOnDisk(t)
	if len(st.Agents) != 1 || st.Agents[0].TaskID != "t4" || st.ClientSessions["Snowcap"] != "session:Snowcap" {
		t.Errorf("state = %+v", st)
	}
	if !strings.Contains(plain(e), "started DEV-7053") {
		t.Errorf("no confirmation:\n%s", plain(e))
	}
}

func TestLaunchUsesExistingClientSessionAndRemembersRepo(t *testing.T) {
	e := newEnv(t)
	e.runner.Responses = []run.Response{
		{Match: "git -C /r/snowcap-subscriptions rev-parse", Err: errors.New("no branch")},
		{Match: "git -C /r/snowcap-subscriptions symbolic-ref", Out: "origin/main"},
	}
	e.rex.sessions["session:S"] = "Snowcap"
	e.app.state.ClientSessions = map[string]string{"Snowcap": "session:S"}
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter", "left", "tab", "s", "u", "b", "tab", "tab", "tab", "x", "ctrl+s") // worktree
	if len(e.rex.calls) == 0 || !strings.HasPrefix(e.rex.calls[0], "new-window session:S | DEV-7700 ") {
		t.Fatalf("rex calls: %v", e.rex.calls)
	}
	if !strings.HasSuffix(e.rex.calls[0], "claude Take a task DEV-7700 from CUx") {
		t.Errorf("edited prompt not used: %s", e.rex.calls[0])
	}
	st := e.stateOnDisk(t)
	if got := st.RecentRepos["Snowcap"]; len(got) != 1 || got[0] != "snowcap-subscriptions" {
		t.Errorf("recent repos = %v", got)
	}
	// Next time, the remembered repo comes first.
	selectTask(t, e, "t4")
	e.press("enter", "left", "left") // scratch → checkout → worktree
	if r, _ := e.app.start.repo(); r.Name != "snowcap-subscriptions" {
		t.Errorf("remembered repo not first: %q", r.Name)
	}
}

func TestLaunchErrorKeepsPanelClosedAndShowsError(t *testing.T) {
	e := newEnv(t)
	e.runner.Responses = []run.Response{{Match: "git -C /r/snowcap fetch", Err: errors.New("could not read from remote")}}
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter", "left", "ctrl+s") // worktree
	if !strings.Contains(plain(e), "could not read from remote") {
		t.Errorf("error not shown:\n%s", plain(e))
	}
	if len(e.rex.calls) != 0 || len(e.cu.writes) != 0 {
		t.Errorf("later steps ran: rex=%v cu=%v", e.rex.calls, e.cu.writes)
	}
}

func TestPanelTabAndSpace(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter")
	p := e.app.start
	e.press("space") // workspace: checkout → scratch
	if p.kind != workspace.Scratch {
		t.Fatalf("space: kind = %v", p.kind)
	}
	e.press("h", "j", "k", "l", "left") // letters do nothing on a choice field
	if p.kind != workspace.Checkout || p.field != fWorkspace {
		t.Fatalf("letters changed the panel: kind=%v field=%v", p.kind, p.field)
	}
	e.press("tab") // → repo
	e.press("s", "n", "down")
	if r, _ := p.repo(); p.repoIdx != 1 || !strings.Contains(r.Name, "sn") {
		t.Errorf("down in matches: idx %d repo %q", p.repoIdx, r.Name)
	}
	e.press("up", "space") // space is text in the filter
	if p.repoFilter != "sn " || p.repoIdx != 0 {
		t.Errorf("filter = %q idx %d", p.repoFilter, p.repoIdx)
	}
	e.press("left", "right") // no field change, no list move
	if p.field != fRepo || p.repoIdx != 0 {
		t.Errorf("left/right: field %v idx %d", p.field, p.repoIdx)
	}
	e.press("backspace", "backspace", "backspace", "tab") // → agent (checkout has no branch)
	e.press("space")
	if p.agents[p.agentIdx] != "codex" {
		t.Errorf("space on agent: %s", p.agents[p.agentIdx])
	}
	e.press("shift+tab")
	if p.field != fRepo {
		t.Errorf("shift+tab: field = %v", p.field)
	}
}

func TestLongPromptWrapsInsteadOfCutting(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter")
	e.app.start.prompt = "Take a task DEV-7700 from CU and also check the Promo docs for the checkout widget before you start END"
	out := plain(e)
	if !strings.Contains(out, "END") {
		t.Errorf("end of a long prompt is hidden:\n%s", out)
	}
}

func TestLongTitleWrapsInPanel(t *testing.T) {
	e := withIssues(t)
	selectIn(t, e, "3", task.Ref(github.IssuesSource, "acme/shop#42"))
	e.press("enter")
	e.app.start.task.Name = "Account portal: Pause, Skip Next Order, and the frequency picker END"
	out := plain(e)
	if !strings.Contains(out, "frequency picker END") {
		t.Errorf("title cut instead of wrapped:\n%s", out)
	}
}

func TestShortPanelKeepsLaunch(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter")
	for _, h := range []int{40, 24, 18, 12} {
		lines := e.app.renderStart(h)
		all := strings.Join(lines, "\n")
		if !strings.Contains(all, "Move to in progress") {
			t.Errorf("height %d: checkbox cut:\n%s", h, all)
		}
		if len(lines) > h || !strings.Contains(all, "Launch ⏎") {
			t.Errorf("height %d: %d lines, launch shown=%v", h, len(lines), strings.Contains(strings.Join(lines, "\n"), "Launch ⏎"))
		}
	}
}

func TestLongBranchWrapsInPanel(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter", "left") // worktree
	e.app.start.branch = "feature/dev-7700-promo-checkout-personalisation-and-more-words-END"
	out := plain(e)
	if !strings.Contains(out, "feature/dev-7700-") || !strings.Contains(out, "words-END") {
		t.Errorf("branch cut instead of wrapped:\n%s", out)
	}
}

func TestEmptyRepoFilterShowsNoQuotes(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter", "tab") // → repo, nothing typed
	if out := plain(e); strings.Contains(out, `""`) || !strings.Contains(out, "› Repo") {
		t.Errorf("empty filter:\n%s", out)
	}
}

// Arrows never move between fields; they work inside the field.
func TestArrowsStayInField(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter")
	p := e.app.start
	e.press("down") // workspace: next option, not the next field
	if p.field != fWorkspace || p.kind != workspace.Scratch {
		t.Errorf("down on workspace: field %v kind %v", p.field, p.kind)
	}
	e.press("up", "tab", "tab") // checkout → agent
	if p.field != fAgent {
		t.Fatalf("field %v", p.field)
	}
	e.press("down") // the row below: claude → opencode
	if p.agents[p.agentIdx] != "opencode" {
		t.Errorf("down on agent: %s", p.agents[p.agentIdx])
	}
	e.press("up", "right")
	if p.agents[p.agentIdx] != "codex" {
		t.Errorf("up, right on agent: %s", p.agents[p.agentIdx])
	}
	e.press("tab", "up", "down") // prompt: arrows do nothing
	if p.field != fPrompt {
		t.Errorf("arrows left the prompt: %v", p.field)
	}
	e.press("tab", "down") // move checkbox toggles
	if p.field != fMove || p.moveStatus {
		t.Errorf("down on checkbox: field %v move %v", p.field, p.moveStatus)
	}
}
