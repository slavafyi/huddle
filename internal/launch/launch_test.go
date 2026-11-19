package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/slavafyi/huddle/internal/clickup"
	"github.com/slavafyi/huddle/internal/rex"
	"github.com/slavafyi/huddle/internal/run"
	"github.com/slavafyi/huddle/internal/store"
	"github.com/slavafyi/huddle/internal/task"
	"github.com/slavafyi/huddle/internal/workspace"
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

// runAll runs both launch steps like the UI does.
func runAll(ctx context.Context, d Deps, p Plan) (Result, Updates, error) {
	res, err := Open(ctx, d, p)
	if err != nil {
		return res, Updates{}, err
	}
	return res, Apply(ctx, d.Source, p), nil
}

func sampleTask() task.Task {
	return clickup.ToTask(clickup.Task{ID: "t1", CustomID: "DEV-7053", Name: "Research low stock notifications", ListName: "Snowcap"}, 0)
}

func setup() (*fakeRex, *fakeCU, *run.Fake, Deps) {
	r, cu, f := &fakeRex{sessions: map[string]string{"session:S": "Snowcap"}}, &fakeCU{}, &run.Fake{}
	return r, cu, f, Deps{Runner: f, Rex: r, Source: &clickup.Source{Client: cu}, Now: func() time.Time { return now }}
}

func TestScratchInExistingSession(t *testing.T) {
	r, cu, _, d := setup()
	root := t.TempDir()
	p := Plan{
		Task:      sampleTask(),
		Workspace: workspace.Plan{Kind: workspace.Scratch, ScratchRoot: root, TaskKey: "dev-7053", Slug: "research-low-stock-notifications"},
		AgentName: "claude", Argv: []string{"claude", "Take a task DEV-7053 from CU"},
		SessionID: "session:S", TabName: "DEV-7053 Research low stock notifications",
		SetStatus: "in progress", AssignMe: 1,
	}
	res, up, err := runAll(context.Background(), d, p)
	if err != nil {
		t.Fatal(err)
	}
	if !up.StatusOK || !up.AssignOK {
		t.Errorf("updates = %+v", up)
	}
	dir := root + "/dev-7053-research-low-stock-notifications"
	wantRex := []string{
		"sessions",
		"new-window session:S | DEV-7053 Research low stock notifications | " + dir + " | claude --session-id " + res.Agent.AgentSession + " Take a task DEV-7053 from CU",
		"focus session:S window:2",
	}
	if !reflect.DeepEqual(r.calls, wantRex) {
		t.Errorf("rex calls:\n%s", strings.Join(r.calls, "\n"))
	}
	if want := []string{"status t1 in progress", "assign t1"}; !reflect.DeepEqual(cu.writes, want) {
		t.Errorf("clickup writes = %v", cu.writes)
	}
	a := res.Agent
	if a.Source != "clickup" || a.TaskID != "t1" || a.TaskKey != "DEV-7053" || a.Client != "Snowcap" || a.Dir != dir ||
		a.BlockID != "block:2" || a.SessionID != "session:S" || a.Workspace != "scratch" || !a.Started.Equal(now) {
		t.Errorf("agent = %+v", a)
	}
}

func TestWorktreeInNewSessionWhenStoredSessionIsGone(t *testing.T) {
	r, cu, f, d := setup()
	r.sessions = map[string]string{"session:other": "Lark Kids"}
	f.Responses = []run.Response{
		{Match: "git -C /r/snowcap rev-parse", Err: errors.New("no branch")},
		{Match: "git -C /r/snowcap symbolic-ref", Out: "origin/main"},
	}
	root := t.TempDir()
	p := Plan{
		Task:      sampleTask(),
		Workspace: workspace.Plan{Kind: workspace.Worktree, Repo: workspace.Repo{Name: "snowcap", Path: "/r/snowcap"}, Branch: "feature/dev-7053-x", TaskKey: "dev-7053", WorktreeRoot: root},
		AgentName: "shell", SessionID: "session:gone", TabName: "DEV-7053 R",
	}
	res, err := runAll3(context.Background(), d, p)
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
	if calls := f.Calls(); !strings.Contains(strings.Join(calls, "\n"), "worktree add --no-track -b feature/dev-7053-x") {
		t.Errorf("git calls: %v", calls)
	}
}

func TestPrepareErrorStopsBeforeRex(t *testing.T) {
	r, cu, f, d := setup()
	f.Responses = []run.Response{{Match: "git -C /r/s fetch", Err: errors.New("offline")}}
	p := Plan{Task: sampleTask(), Workspace: workspace.Plan{Kind: workspace.Worktree, Repo: workspace.Repo{Name: "s", Path: "/r/s"}, Branch: "b", TaskKey: "k", WorktreeRoot: t.TempDir()}, SetStatus: "in progress"}
	if _, err := runAll3(context.Background(), d, p); err == nil {
		t.Fatal("want error")
	}
	if len(r.calls) != 0 || len(cu.writes) != 0 {
		t.Errorf("no later steps should run: rex=%v cu=%v", r.calls, cu.writes)
	}
}

func TestClickUpErrorAfterLaunchIsAWarning(t *testing.T) {
	_, cu, _, d := setup()
	cu.err = errors.New("500")
	p := Plan{Task: sampleTask(), Workspace: workspace.Plan{Kind: workspace.Scratch, ScratchRoot: t.TempDir(), TaskKey: "k", Slug: "s"}, SessionID: "session:S", SetStatus: "in progress", AssignMe: 1}
	res, up, err := runAll(context.Background(), d, p)
	if err != nil {
		t.Fatal(err)
	}
	if res.Agent.BlockID == "" || len(up.Warnings) != 1 || !strings.Contains(up.Warnings[0], "500") {
		t.Errorf("res = %+v up = %+v", res, up)
	}
	if up.StatusOK || up.AssignOK {
		t.Errorf("no write succeeded: %+v", up)
	}
	if len(cu.writes) != 1 {
		t.Errorf("should stop after the first failed write: %v", cu.writes)
	}
}

func TestFocusWithoutRemoteControlIsAWarning(t *testing.T) {
	r, _, _, d := setup()
	r.focusErr = rex.ErrRemoteControl
	p := Plan{Task: sampleTask(), Workspace: workspace.Plan{Kind: workspace.Scratch, ScratchRoot: t.TempDir(), TaskKey: "k", Slug: "s"}, SessionID: "session:S"}
	res, err := runAll3(context.Background(), d, p)
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

func TestFindsClientSessionByNameWhenNoneStored(t *testing.T) {
	r, _, _, d := setup()
	p := Plan{Task: sampleTask(), Workspace: workspace.Plan{Kind: workspace.Scratch, ScratchRoot: t.TempDir(), TaskKey: "k", Slug: "s"}, TabName: "T"}
	res, err := runAll3(context.Background(), d, p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.calls[1], "new-window session:S | T") || res.Agent.SessionID != "session:S" {
		t.Errorf("should reuse the session named after the client: %v", r.calls)
	}
}

func runAll3(ctx context.Context, d Deps, p Plan) (Result, error) {
	res, _, err := runAll(ctx, d, p)
	return res, err
}

func TestOpenDoesNotTouchClickUp(t *testing.T) {
	_, cu, _, d := setup()
	p := Plan{Task: sampleTask(), Workspace: workspace.Plan{Kind: workspace.Scratch, ScratchRoot: t.TempDir(), TaskKey: "k", Slug: "s"}, SessionID: "session:S", SetStatus: "in progress", AssignMe: 1}
	if _, err := Open(context.Background(), d, p); err != nil {
		t.Fatal(err)
	}
	if len(cu.writes) != 0 {
		t.Errorf("Open must not write to ClickUp: %v", cu.writes)
	}
}

var isUUIDv4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestOpenSetsTheAgentsSessionID(t *testing.T) {
	r, _, _, d := setup()
	p := Plan{Task: sampleTask(), Workspace: workspace.Plan{Kind: workspace.Scratch, ScratchRoot: t.TempDir(), TaskKey: "k", Slug: "s"},
		AgentName: "claude", Argv: []string{"claude", "--model", "opus", "Take it"}, Command: []string{"claude", "--model", "opus"},
		SessionID: "session:S", TabName: "T"}
	res, err := Open(context.Background(), d, p)
	if err != nil {
		t.Fatal(err)
	}
	a := res.Agent
	if !isUUIDv4.MatchString(a.AgentSession) {
		t.Fatalf("agent session = %q", a.AgentSession)
	}
	if !strings.HasSuffix(r.calls[1], "| claude --session-id "+a.AgentSession+" --model opus Take it") {
		t.Errorf("rex calls: %v", r.calls)
	}
	if !reflect.DeepEqual(a.Command, []string{"claude", "--model", "opus"}) {
		t.Errorf("command = %q", a.Command)
	}
}

func TestOpenWithoutAChatIDStillLaunches(t *testing.T) {
	r, _, f, d := setup()
	f.Responses = []run.Response{{Match: "cursor-agent create-chat", Err: errors.New("not logged in")}}
	p := Plan{Task: sampleTask(), Workspace: workspace.Plan{Kind: workspace.Scratch, ScratchRoot: t.TempDir(), TaskKey: "k", Slug: "s"},
		AgentName: "cursor", Argv: []string{"cursor-agent", "Take it"}, Command: []string{"cursor-agent"}, SessionID: "session:S", TabName: "T"}
	res, err := Open(context.Background(), d, p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(r.calls[1], "| cursor-agent Take it") || res.Agent.AgentSession != "" {
		t.Errorf("rex calls: %v agent: %+v", r.calls, res.Agent)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "not logged in") {
		t.Errorf("warnings = %q", res.Warnings)
	}
}

func closedAgent(dir string, command []string, session string) store.Agent {
	return store.Agent{Source: "clickup", TaskID: "t1", TaskKey: "DEV-7053", Title: "Research", Client: "Snowcap",
		Workspace: "scratch", Dir: dir, Agent: "x", Command: command, AgentSession: session, Started: now.Add(-time.Hour),
		Placement: rex.Placement{SessionID: "session:S", WindowID: "window:1", BlockID: "block:1"}}
}

func TestReopenResumesTheSavedSession(t *testing.T) {
	r, _, _, d := setup()
	dir := t.TempDir()
	res, err := Reopen(context.Background(), d, closedAgent(dir, []string{"claude", "--model", "opus"}, "abc"), "DEV-7053 Research")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"sessions",
		"new-window session:S | DEV-7053 Research | " + dir + " | claude --resume abc --model opus",
		"focus session:S window:2",
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Errorf("rex calls:\n%s", strings.Join(r.calls, "\n"))
	}
	a := res.Agent
	if a.BlockID != "block:2" || a.WindowID != "window:2" || a.AgentSession != "abc" || !a.Started.Equal(now.Add(-time.Hour)) || a.TaskKey != "DEV-7053" {
		t.Errorf("agent = %+v", a)
	}
}

func TestReopenFindsTheSessionTheAgentPicked(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := t.TempDir()
	at := now.Add(-time.Hour + 2*time.Second).Local()
	file := filepath.Join(home, "sessions", at.Format("2006/01/02"), "rollout-"+at.Format("2006-01-02T15-04-05")+"-0ccccccc-0000-7000-8000-000000000001.jsonl")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"type":"session_meta","payload":{"id":"0ccccccc-0000-7000-8000-000000000001","timestamp":"` + at.UTC().Format(time.RFC3339) + `","cwd":"` + dir + `","thread_source":"user"}}` + "\n"
	if err := os.WriteFile(file, []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	r, _, _, d := setup()
	res, err := Reopen(context.Background(), d, closedAgent(dir, []string{"codex"}, ""), "T")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(r.calls[1], "| codex resume 0ccccccc-0000-7000-8000-000000000001") || res.Agent.AgentSession != "0ccccccc-0000-7000-8000-000000000001" {
		t.Errorf("rex calls: %v agent: %+v", r.calls, res.Agent)
	}
}

func TestReopenInANewSessionWhenTheOldOneIsGone(t *testing.T) {
	r, _, _, d := setup()
	r.sessions = map[string]string{}
	dir := t.TempDir()
	res, err := Reopen(context.Background(), d, closedAgent(dir, []string{"pi"}, "abc"), "T")
	if err != nil {
		t.Fatal(err)
	}
	if r.calls[1] != "new-session Snowcap | "+dir+" | T | pi --session abc" || res.Agent.SessionID != "session:new" {
		t.Errorf("rex calls: %v", r.calls)
	}
}

func TestReopenStopsWhenItCant(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	dir := t.TempDir()
	cases := map[string]store.Agent{
		"has no command":      closedAgent(dir, nil, ""),
		"can't reopen aider":  closedAgent(dir, []string{"aider"}, ""),
		"folder is gone":      closedAgent(filepath.Join(dir, "removed"), []string{"claude"}, "abc"),
		"no session of codex": closedAgent(dir, []string{"codex"}, ""),
	}
	for want, ag := range cases {
		r, _, _, d := setup()
		_, err := Reopen(context.Background(), d, ag, "T")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", want, err)
		}
		if len(r.calls) != 0 {
			t.Errorf("%s: rex calls = %v", want, r.calls)
		}
	}
}
