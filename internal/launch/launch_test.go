package launch

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/slavafyi/cuddle/internal/clickup"
	"github.com/slavafyi/cuddle/internal/rex"
	"github.com/slavafyi/cuddle/internal/run"
	"github.com/slavafyi/cuddle/internal/workspace"
)

type fakeRex struct {
	sessions map[string]string
	calls    []string
	focusErr error
}

func (f *fakeRex) Sessions(context.Context) (map[string]string, error) {
	f.calls = append(f.calls, "sessions")
	return f.sessions, nil
}
func (f *fakeRex) NewSession(_ context.Context, name, dir, window string, argv []string) (rex.Placement, error) {
	f.calls = append(f.calls, "new-session "+name+" | "+dir+" | "+window+" | "+strings.Join(argv, " "))
	return rex.Placement{SessionID: "session:new", WindowID: "window:1", BlockID: "block:1"}, nil
}
func (f *fakeRex) NewWindow(_ context.Context, s, name, dir string, argv []string) (rex.Placement, error) {
	f.calls = append(f.calls, "new-window "+s+" | "+name+" | "+dir+" | "+strings.Join(argv, " "))
	return rex.Placement{SessionID: s, WindowID: "window:2", BlockID: "block:2"}, nil
}
func (f *fakeRex) Blocks(context.Context, string) (map[string]bool, error) { return nil, nil }
func (f *fakeRex) Status(context.Context, string, string) (rex.Status, error) {
	return rex.Status{}, nil
}
func (f *fakeRex) Focus(_ context.Context, s, w string) error {
	f.calls = append(f.calls, "focus "+s+" "+w)
	return f.focusErr
}
func (f *fakeRex) CloseWindow(context.Context, string, string) error        { return nil }
func (f *fakeRex) LastLine(context.Context, string, string) (string, error) { return "", nil }

type fakeCU struct {
	writes []string
	err    error
}

func (f *fakeCU) Me(context.Context) (clickup.User, error) { return clickup.User{}, nil }
func (f *fakeCU) MyTasks(context.Context, int64, time.Time, time.Time) ([]clickup.Task, error) {
	return nil, nil
}
func (f *fakeCU) Statuses(context.Context, string) ([]clickup.Status, error) { return nil, nil }
func (f *fakeCU) SetStatus(_ context.Context, id, s string) error {
	f.writes = append(f.writes, "status "+id+" "+s)
	return f.err
}
func (f *fakeCU) AddComment(context.Context, string, string) error { return nil }
func (f *fakeCU) SetAssignee(_ context.Context, id string, u int64, add bool) error {
	f.writes = append(f.writes, "assign "+id)
	return f.err
}

var now = time.Date(2026, 11, 9, 13, 0, 0, 0, time.UTC)

func task() clickup.Task {
	return clickup.Task{ID: "t1", CustomID: "DEV-7053", Name: "Research low stock notifications", ListName: "Snowcap"}
}

func setup() (*fakeRex, *fakeCU, *run.Fake, Deps) {
	r, cu, f := &fakeRex{sessions: map[string]string{"session:S": "Snowcap"}}, &fakeCU{}, &run.Fake{}
	return r, cu, f, Deps{Runner: f, Rex: r, ClickUp: cu, Now: func() time.Time { return now }}
}

func TestScratchInExistingSession(t *testing.T) {
	r, cu, _, d := setup()
	root := t.TempDir()
	p := Plan{
		Task:      task(),
		Workspace: workspace.Plan{Kind: workspace.Scratch, ScratchRoot: root, TaskKey: "dev-7053", Slug: "research-low-stock-notifications"},
		AgentName: "claude", Argv: []string{"claude", "Take a task DEV-7053 from CU"},
		SessionID: "session:S", TabName: "DEV-7053 Research low stock notifications",
		SetStatus: "in progress", AssignMe: 1,
	}
	res, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatal(err)
	}
	dir := root + "/dev-7053-research-low-stock-notifications"
	wantRex := []string{
		"sessions",
		"new-window session:S | DEV-7053 Research low stock notifications | " + dir + " | claude Take a task DEV-7053 from CU",
		"focus session:S window:2",
	}
	if !reflect.DeepEqual(r.calls, wantRex) {
		t.Errorf("rex calls:\n%s", strings.Join(r.calls, "\n"))
	}
	if want := []string{"status t1 in progress", "assign t1"}; !reflect.DeepEqual(cu.writes, want) {
		t.Errorf("clickup writes = %v", cu.writes)
	}
	a := res.Agent
	if a.TaskID != "t1" || a.TaskKey != "DEV-7053" || a.Client != "Snowcap" || a.Dir != dir ||
		a.BlockID != "block:2" || a.SessionID != "session:S" || a.Workspace != "Scratch" || !a.Started.Equal(now) {
		t.Errorf("agent = %+v", a)
	}
}

func TestWorktreeInNewSessionWhenStoredSessionIsGone(t *testing.T) {
	r, cu, f, d := setup()
	f.Responses = []run.Response{
		{Match: "git -C /r/snowcap rev-parse", Err: errors.New("no branch")},
		{Match: "git -C /r/snowcap symbolic-ref", Out: "origin/main"},
	}
	root := t.TempDir()
	p := Plan{
		Task:      task(),
		Workspace: workspace.Plan{Kind: workspace.Worktree, Repo: workspace.Repo{Name: "snowcap", Path: "/r/snowcap"}, Branch: "feature/dev-7053-x", TaskKey: "dev-7053", WorktreeRoot: root},
		AgentName: "shell", SessionID: "session:gone", TabName: "DEV-7053 R",
	}
	res, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.calls[1], "new-session Snowcap | "+root+"/snowcap/dev-7053 | DEV-7053 R | ") {
		t.Errorf("rex calls: %v", r.calls)
	}
	if len(cu.writes) != 0 {
		t.Errorf("unchecked boxes must not write: %v", cu.writes)
	}
	if res.Agent.Repo != "snowcap" || res.Agent.Branch != "feature/dev-7053-x" || res.Agent.SessionID != "session:new" {
		t.Errorf("agent = %+v", res.Agent)
	}
	if calls := f.Calls(); !strings.Contains(strings.Join(calls, "\n"), "worktree add -b feature/dev-7053-x") {
		t.Errorf("git calls: %v", calls)
	}
}

func TestPrepareErrorStopsBeforeRex(t *testing.T) {
	r, cu, f, d := setup()
	f.Responses = []run.Response{{Match: "git -C /r/s fetch", Err: errors.New("offline")}}
	p := Plan{Task: task(), Workspace: workspace.Plan{Kind: workspace.Worktree, Repo: workspace.Repo{Name: "s", Path: "/r/s"}, Branch: "b", TaskKey: "k", WorktreeRoot: t.TempDir()}, SetStatus: "in progress"}
	if _, err := Run(context.Background(), d, p); err == nil {
		t.Fatal("want error")
	}
	if len(r.calls) != 0 || len(cu.writes) != 0 {
		t.Errorf("no later steps should run: rex=%v cu=%v", r.calls, cu.writes)
	}
}

func TestClickUpErrorAfterLaunchIsAWarning(t *testing.T) {
	_, cu, _, d := setup()
	cu.err = errors.New("500")
	p := Plan{Task: task(), Workspace: workspace.Plan{Kind: workspace.Scratch, ScratchRoot: t.TempDir(), TaskKey: "k", Slug: "s"}, SessionID: "session:S", SetStatus: "in progress", AssignMe: 1}
	res, err := Run(context.Background(), d, p)
	if err != nil {
		t.Fatal(err)
	}
	if res.Agent.BlockID == "" || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "500") {
		t.Errorf("res = %+v", res)
	}
	if len(cu.writes) != 1 {
		t.Errorf("should stop after the first failed write: %v", cu.writes)
	}
}

func TestFocusWithoutRemoteControlIsAWarning(t *testing.T) {
	r, _, _, d := setup()
	r.focusErr = rex.ErrRemoteControl
	p := Plan{Task: task(), Workspace: workspace.Plan{Kind: workspace.Scratch, ScratchRoot: t.TempDir(), TaskKey: "k", Slug: "s"}, SessionID: "session:S"}
	res, err := Run(context.Background(), d, p)
	if err != nil || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "Remote Control") {
		t.Errorf("res = %+v err = %v", res, err)
	}
}

func TestArgv(t *testing.T) {
	agents := map[string][]string{"claude": {"claude", "{prompt}"}}
	got := Argv(agents, "claude", "Take a task DEV-1 from CU")
	if !reflect.DeepEqual(got, []string{"claude", "Take a task DEV-1 from CU"}) {
		t.Errorf("argv = %q", got)
	}
	if Argv(agents, "shell", "x") != nil {
		t.Error("shell has no command")
	}
}
