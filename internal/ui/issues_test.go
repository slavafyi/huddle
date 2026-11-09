package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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

func selectToday(t *testing.T, e *testEnv, ref string) {
	t.Helper()
	e.press("1")
	for i := 0; i < 30; i++ {
		if r, _ := e.app.today.list.selected(); r.key == ref {
			return
		}
		e.press("j")
	}
	t.Fatalf("task %s not in Today", ref)
}

func TestTodayMixesClickUpAndIssueGroups(t *testing.T) {
	e := withIssues(t)
	out := plain(e)
	order := []string{"In progress 2", "DEV-7808", "shop#44", "In review 1", "shop#43", "Up next 2", "Open 2", "shop#42", "Waiting on others 1", "Done this week 1"}
	pos := -1
	for _, want := range order {
		i := strings.Index(out, want)
		if i < 0 {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
		if i < pos {
			t.Errorf("%q out of order", want)
		}
		pos = i
	}
	if !strings.Contains(out, "Mon, Nov 9 · 2 in progress · 1 in review · 2 up next · 2 open · 1 waiting · 1 done this week") {
		t.Errorf("summary line wrong:\n%s", out)
	}
	e.press("2")
	if out := plain(e); !strings.Contains(out, "All 8") || !strings.Contains(out, "Lead / opened") {
		t.Errorf("mixed filter labels:\n%s", out)
	}
}

func TestStartPanelForIssue(t *testing.T) {
	e := withIssues(t)
	selectToday(t, e, task.Ref(github.IssuesSource, "acme/shop#42"))
	e.press("enter")
	p := e.app.start
	if p == nil {
		t.Fatal("panel not open")
	}
	if p.branch != "feature/42-fix-checkout" || p.prompt != "Take issue acme/shop#42" || p.showMove {
		t.Errorf("branch %q prompt %q move %v", p.branch, p.prompt, p.showMove)
	}
	for _, f := range p.fields() {
		if f == fRepo || f == fMove {
			t.Errorf("field %v shown", f)
		}
	}
	out := plain(e)
	for _, want := range []string{"shop#42 Fix checkout", "Repo", "shop", "new tab in session shop"} {
		if !strings.Contains(out, want) {
			t.Errorf("panel missing %q:\n%s", want, out)
		}
	}
	e.press("right", "ctrl+s") // checkout
	want := "new-session shop | /r/shop | shop#42 Fix checkout | claude Take issue acme/shop#42"
	if len(e.rex.calls) == 0 || e.rex.calls[0] != want {
		t.Fatalf("rex calls:\n%s\nwant:\n%s", strings.Join(e.rex.calls, "\n"), want)
	}
}

func TestStartPanelForIssueWithoutClone(t *testing.T) {
	e := withIssues(t)
	selectToday(t, e, task.Ref(github.IssuesSource, "acme/other#5"))
	e.press("enter")
	if !strings.Contains(plain(e), "no local clone of acme/other in repo_roots") {
		t.Errorf("no message:\n%s", plain(e))
	}
	e.press("ctrl+s")
	if e.app.mode != modeStart || !strings.Contains(plain(e), "no local clone of acme/other in repo_roots") || len(e.rex.calls) != 0 {
		t.Fatalf("worktree launch must fail: mode %v calls %v", e.app.mode, e.rex.calls)
	}
	e.press("left", "ctrl+s") // scratch still works, in a safe folder
	if len(e.rex.calls) == 0 || !strings.Contains(e.rex.calls[0], "/other-5-elsewhere |") {
		t.Fatalf("rex calls: %v", e.rex.calls)
	}
}
