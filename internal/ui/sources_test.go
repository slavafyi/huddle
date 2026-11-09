package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/slavafyi/huddle/internal/task"
)

// otherSource is a second task source. Its task IDs overlap ClickUp's.
type otherSource struct {
	tasks  []task.Task
	err    error
	writes []string
}

func (o *otherSource) Name() string                      { return "other" }
func (o *otherSource) Rules() task.Rules                 { return task.Rules{Active: []string{"open"}} }
func (o *otherSource) Me(context.Context) (int64, error) { return 7, o.err }
func (o *otherSource) Statuses(context.Context, task.Task) ([]task.Status, error) {
	return []task.Status{{Name: "open"}, {Name: "closed", Order: 1, Done: true}}, nil
}
func (o *otherSource) Fetch(_ context.Context, me int64, _, _ time.Time) ([]task.Task, error) {
	return o.tasks, o.err
}
func (o *otherSource) SetStatus(_ context.Context, t task.Task, s string) error {
	o.writes = append(o.writes, "status "+t.ID+" "+s)
	for i := range o.tasks { // later reads see the write
		if o.tasks[i].ID == t.ID && s == "closed" {
			o.tasks[i].Status, o.tasks[i].Done = s, testNow
		}
	}
	return nil
}
func (o *otherSource) AddComment(context.Context, task.Task, string) error { return nil }
func (o *otherSource) SetAssignee(context.Context, task.Task, int64, bool) error {
	return nil
}

func withOther(t *testing.T) (*testEnv, *otherSource) {
	t.Helper()
	e := newEnv(t)
	o := &otherSource{tasks: []task.Task{
		{Source: "other", ID: "t1", Key: "repo#1", Name: "Other source issue", Status: "open", Project: "repo", Mine: true},
	}}
	d := e.app.deps
	d.Sources = append(d.Sources, o)
	e.app = NewApp(d)
	e.app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return e, o
}

func TestTwoSourcesShareTheViews(t *testing.T) {
	e, o := withOther(t)
	e.load(t)
	e.press("3")
	if out := plain(e); !strings.Contains(out, "repo#1") {
		t.Fatalf("the second source has its own tab:\n%s", out)
	}
	if c := e.cacheOnDisk(t); c.Me["other"] != 7 || c.Me["clickup"] != 1 {
		t.Errorf("me by source = %v", c.Me)
	}

	// A full refresh where the other source fails keeps its cached tasks,
	// and ClickUp's full refresh doesn't drop them either.
	o.err = errOffline
	e.load(t)
	if out := plain(e); !strings.Contains(out, "repo#1") || !e.app.hasTask("DEV-7808") {
		t.Fatalf("failed source lost its tasks:\n%s", out)
	}
	if e.app.syncErr == nil {
		t.Error("the failed source should show as offline")
	}

	// Writes go to the task's own source, even with the same ID.
	o.err = nil
	e.press("3")
	for i := 0; i < 20; i++ {
		if r, _ := e.app.tasks.list.selected(); r.key == task.Ref("other", "t1") {
			break
		}
		e.press("j")
	}
	e.press("s", "j", "enter")
	if len(o.writes) != 1 || o.writes[0] != "status t1 closed" || len(e.cu.writes) != 0 {
		t.Errorf("other writes = %v, clickup writes = %v", o.writes, e.cu.writes)
	}
	if tk, _ := e.app.taskByRef(task.Ref("other", "t1")); !tk.IsDone() {
		t.Error("closing should mark the task done on screen")
	}
	if tk, _ := e.app.taskByID("t1"); tk.Status != "in progress" {
		t.Errorf("ClickUp t1 changed: %q", tk.Status)
	}
}

func TestStatusStyleFallsBackOnDone(t *testing.T) {
	if got, want := statusStyle("merged", true).Render("x"), sGreen.Render("x"); got != want {
		t.Errorf("unknown done status = %q, want green %q", got, want)
	}
	if got, want := statusStyle("whatever", false).Render("x"), sFaint.Render("x"); got != want {
		t.Errorf("unknown open status = %q, want faint %q", got, want)
	}
	if got, want := statusStyle("in progress", false).Render("x"), sBlue.Render("x"); got != want {
		t.Errorf("known status = %q", got)
	}
}

// hasTask reports whether a task with that key is cached.
func (a *App) hasTask(key string) bool {
	for _, t := range a.cache.Tasks {
		if t.Key == key {
			return true
		}
	}
	return false
}
