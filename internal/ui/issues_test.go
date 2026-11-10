package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/slavafyi/huddle/internal/github"
	"github.com/slavafyi/huddle/internal/rex"
	"github.com/slavafyi/huddle/internal/run"
	"github.com/slavafyi/huddle/internal/store"
	"github.com/slavafyi/huddle/internal/task"
	"github.com/slavafyi/huddle/internal/workspace"
)

func issueJSON(repo, num, title string) string {
	_, name, _ := strings.Cut(repo, "/")
	return `{"number": ` + num + `, "title": "` + title + `", "url": "https://github.com/` + repo + `/issues/` + num + `",
	  "state": "OPEN", "closedAt": null, "updatedAt": "2026-11-08T09:00:00Z",
	  "repository": {"nameWithOwner": "` + repo + `", "name": "` + name + `"},
	  "author": {"login": "bob", "databaseId": 2},
	  "assignees": {"nodes": [{"login": "me", "databaseId": 1}]}, "labels": {"nodes": []}}`
}

// withIssues is the work profile: ClickUp plus GitHub issues (through the
// real github.Issues source on a fake gh).
func withIssues(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t)
	gh := &run.Fake{Responses: []run.Response{
		{Match: "gh api user", Out: `{"id": 1}`},
		{Match: "gh api graphql", Out: `{"data": {"openAssigned": {"nodes": [` +
			issueJSON("acme/shop", "42", "Fix checkout") + `,` +
			issueJSON("acme/shop", "43", "Add wishlist") + `,` +
			issueJSON("acme/shop", "44", "Live one") + `,` +
			issueJSON("acme/other", "5", "Elsewhere") + `]}}}`},
	}}
	d := e.app.deps
	d.Sources = append(d.Sources, &github.Issues{GH: &github.GH{Runner: gh}})
	d.Cache.Repos = append(d.Cache.Repos, workspace.Repo{Name: "shop", Path: "/r/shop", Remote: "acme/shop"})
	d.Cache.PRs = []github.PR{{Number: 9, Repo: "acme/shop", RepoName: "shop", Branch: "feature/43-wishlist", Kind: github.Mine}}
	d.State.Agents = []store.Agent{{Source: github.IssuesSource, TaskID: "acme/shop#44", TaskKey: "shop#44", Placement: rex.Placement{BlockID: "b44"}}}
	e.app = NewApp(d)
	e.app.agentInfo["b44"] = agentInfo{known: true, alive: true}
	e.app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	e.load(t)
	return e
}

// selectIn opens view and moves to the task with ref.
func selectIn(t *testing.T, e *testEnv, view, ref string) {
	t.Helper()
	e.press(view)
	for i := 0; i < 30; i++ {
		if r, _ := e.app.tasks.list.selected(); r.key == ref {
			return
		}
		e.press("j")
	}
	t.Fatalf("task %s not in view %s", ref, view)
}

func TestEachSourceHasItsOwnTab(t *testing.T) {
	e := withIssues(t)
	if bar := ansi.Strip(e.app.tabBar()); !strings.HasPrefix(bar, "1 Today   2 Tasks   3 Issues   4 PRs   5 Agents") {
		t.Errorf("tab bar = %q", bar)
	}
	e.press("2")
	out := plain(e)
	if !strings.Contains(out, "DEV-7808") || strings.Contains(out, "Fix checkout") || !strings.Contains(out, "Mine + Lead 4") {
		t.Errorf("Tasks must be ClickUp only:\n%s", out)
	}
	e.press("3")
	out = plain(e)
	if strings.Contains(out, "DEV-7808") || !strings.Contains(out, "All 4") || !strings.Contains(out, "Opened by me 0") {
		t.Errorf("Issues must be GitHub only:\n%s", out)
	}
	// Grouped by repo, with labels and updated age.
	for _, want := range []string{"shop 3", "other 1", "LABELS", "UPDATED"} {
		if !strings.Contains(out, want) {
			t.Errorf("Issues missing %q:\n%s", want, out)
		}
	}
	e.press("5")
	if e.app.tab != TabAgents {
		t.Errorf("5 = %v, want Agents", e.app.tab)
	}
	e.press("tab")
	if e.app.tab != TabToday {
		t.Errorf("tab after Agents = %v", e.app.tab)
	}
}

func TestTodayShowsOnlyTheMainSource(t *testing.T) {
	e := withIssues(t)
	out := plain(e)
	if !strings.Contains(out, "DEV-7808") || strings.Contains(out, "Fix checkout") || strings.Contains(out, "Open ") {
		t.Errorf("work Today must be ClickUp only:\n%s", out)
	}
	if !strings.Contains(out, "Mon, Nov 9 · 1 in progress · 2 up next · 1 waiting · 1 done this week") {
		t.Errorf("summary line wrong:\n%s", out)
	}
}

func TestIssuesOnlyProfile(t *testing.T) {
	e := withIssues(t)
	d := e.app.deps
	d.Sources = d.Sources[1:]
	e.app = NewApp(d)
	e.app.agentInfo["b44"] = agentInfo{known: true, alive: true}
	e.app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	e.load(t)
	if bar := ansi.Strip(e.app.tabBar()); !strings.HasPrefix(bar, "1 Today   2 Issues   3 PRs   4 Agents") {
		t.Errorf("tab bar = %q", bar)
	}
	out := plain(e)
	for _, want := range []string{"In progress 1", "#44", "In review 1", "#43", "Open 2", "#42"} {
		if !strings.Contains(out, want) {
			t.Errorf("Today missing %q:\n%s", want, out)
		}
	}
}

func TestIssueLabelsShowWithoutPriority(t *testing.T) {
	e := withIssues(t)
	tk, _ := e.app.taskByRef(task.Ref(github.IssuesSource, "acme/shop#42"))
	tk.Labels = []string{"bug", "ui"}
	for i, c := range e.app.cache.Tasks {
		if c.Ref() == tk.Ref() {
			e.app.cache.Tasks[i] = tk
		}
	}
	e.app.rebuild()
	e.press("3")
	if row := lineWith(plain(e), "Fix checkout"); !strings.Contains(row, "bug, ui") {
		t.Errorf("row %q", row)
	}
}

func TestListRowsShowShortIssueIDs(t *testing.T) {
	e := withIssues(t)
	e.press("3")
	out := plain(e)
	row := lineWith(out, "Fix checkout")
	if !strings.Contains(row, "#42") || strings.Contains(row, "shop#42") {
		t.Errorf("row %q", row)
	}
}

// lineWith returns the first line of out that contains s.
func lineWith(out, s string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

func TestStartPanelForIssue(t *testing.T) {
	e := withIssues(t)
	selectIn(t, e, "3", task.Ref(github.IssuesSource, "acme/shop#42"))
	e.press("enter")
	p := e.app.start
	if p == nil {
		t.Fatal("panel not open")
	}
	if p.branch != "feature/42-fix-checkout" || p.prompt != "Take an issue #42" || p.showMove {
		t.Errorf("branch %q prompt %q move %v", p.branch, p.prompt, p.showMove)
	}
	for _, f := range p.fields() {
		if f == fRepo || f == fMove {
			t.Errorf("field %v shown", f)
		}
	}
	out := plain(e)
	for _, want := range []string{"shop#42 Fix checkout", "Repo", "shop", "shop › new tab"} {
		if !strings.Contains(out, want) {
			t.Errorf("panel missing %q:\n%s", want, out)
		}
	}
	e.press("ctrl+s") // checkout, the default
	want := "new-session shop | /r/shop | shop#42 Fix checkout | claude Take an issue #42"
	if len(e.rex.calls) == 0 || e.rex.calls[0] != want {
		t.Fatalf("rex calls:\n%s\nwant:\n%s", strings.Join(e.rex.calls, "\n"), want)
	}
}

func TestStartPanelForIssueWithoutClone(t *testing.T) {
	e := withIssues(t)
	selectIn(t, e, "3", task.Ref(github.IssuesSource, "acme/other#5"))
	e.press("enter")
	if !strings.Contains(plain(e), "no local clone of acme/other in repo_roots") {
		t.Errorf("no message:\n%s", plain(e))
	}
	e.press("ctrl+s")
	if e.app.mode != modeStart || !strings.Contains(plain(e), "no local clone of acme/other in repo_roots") || len(e.rex.calls) != 0 {
		t.Fatalf("worktree launch must fail: mode %v calls %v", e.app.mode, e.rex.calls)
	}
	e.press("right", "ctrl+s") // scratch still works, in a safe folder
	if len(e.rex.calls) == 0 || !strings.Contains(e.rex.calls[0], "/other-5-elsewhere |") {
		t.Fatalf("rex calls: %v", e.rex.calls)
	}
}

// In a scratch folder there is no repo, so the prompt names it, unless
// you edited the prompt.
func TestIssuePromptFollowsWorkspace(t *testing.T) {
	e := withIssues(t)
	selectIn(t, e, "3", task.Ref(github.IssuesSource, "acme/shop#42"))
	e.press("enter")
	p := e.app.start
	for p.kind != workspace.Scratch {
		e.press("right")
	}
	if p.prompt != "Take an issue acme/shop#42" {
		t.Errorf("scratch prompt = %q", p.prompt)
	}
	e.press("right")
	if p.prompt != "Take an issue #42" {
		t.Errorf("repo prompt = %q", p.prompt)
	}
	p.prompt = "my own words"
	for p.kind != workspace.Scratch {
		e.press("right")
	}
	if p.prompt != "my own words" {
		t.Errorf("edited prompt replaced: %q", p.prompt)
	}
}

// A repo cloned after the last scan shows up: opening the panel rescans in
// the background and updates the open panel.
func TestStartPanelFindsNewClone(t *testing.T) {
	e := withIssues(t)
	e.disk = append(append([]workspace.Repo(nil), e.app.cache.Repos...), workspace.Repo{Name: "other", Path: "/r/other", Remote: "acme/other"})
	selectIn(t, e, "3", task.Ref(github.IssuesSource, "acme/other#5"))
	e.press("enter")
	if out := plain(e); strings.Contains(out, "no local clone") {
		t.Fatalf("still no clone after rescan:\n%s", out)
	}
	if r, ok := e.app.start.repo(); !ok || r.Path != "/r/other" {
		t.Errorf("repo = %+v", r)
	}
}

// issue_prompt_template replaces the built-in issue prompts, in a repo and
// in a scratch folder; clickup_prompt_template doesn't touch issues.
func TestIssuePrompt(t *testing.T) {
	e := withIssues(t)
	e.app.deps.Config.IssuePrompt = "Take issue {id}: {url}"
	e.app.deps.Config.PromptTemplate = "clickup {id}"
	selectIn(t, e, "3", task.Ref(github.IssuesSource, "acme/shop#42"))
	e.press("enter")
	p := e.app.start
	want := "Take issue shop#42: https://github.com/acme/shop/issues/42"
	if p.prompt != want {
		t.Errorf("repo prompt = %q", p.prompt)
	}
	for p.kind != workspace.Scratch {
		e.press("right")
	}
	if p.prompt != want {
		t.Errorf("scratch prompt = %q", p.prompt)
	}
}
