package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/slavafyi/cuddle/internal/clickup"
	"github.com/slavafyi/cuddle/internal/config"
	"github.com/slavafyi/cuddle/internal/rex"
	"github.com/slavafyi/cuddle/internal/run"
	"github.com/slavafyi/cuddle/internal/store"
	"github.com/slavafyi/cuddle/internal/workspace"
)

var (
	testNow = time.Date(2026, 11, 9, 13, 0, 0, 0, time.Local)
	me      = clickup.User{ID: 1, Username: "Alexa"}
	malcolm = clickup.User{ID: 2, Username: "Malcolm"}
)

type fakeClickUp struct {
	mu       sync.Mutex
	tasks    []clickup.Task
	err      error
	statuses []clickup.Status
	writes   []string
	writeErr error
	sinces   []time.Time
}

func (f *fakeClickUp) Me(context.Context) (clickup.User, error) { return me, f.err }
func (f *fakeClickUp) MyTasks(_ context.Context, _ int64, since, _ time.Time) ([]clickup.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sinces = append(f.sinces, since)
	return f.tasks, f.err
}
func (f *fakeClickUp) Statuses(context.Context, string) ([]clickup.Status, error) {
	return f.statuses, f.err
}
func (f *fakeClickUp) record(s string, change func(*clickup.Task), id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, s)
	if f.writeErr != nil {
		return f.writeErr
	}
	// Like the real API, later reads see successful writes.
	for i := range f.tasks {
		if f.tasks[i].ID == id && change != nil {
			change(&f.tasks[i])
		}
	}
	return nil
}
func (f *fakeClickUp) SetStatus(_ context.Context, id, st string) error {
	return f.record("status "+id+" "+st, func(t *clickup.Task) {
		t.Status = st
		for _, s := range f.statuses {
			if s.Name == st {
				t.StatusType, t.StatusOrder = s.Type, s.Order
			}
		}
	}, id)
}
func (f *fakeClickUp) AddComment(_ context.Context, id, text string) error {
	return f.record("comment "+id+" "+text, nil, id)
}
func (f *fakeClickUp) SetAssignee(_ context.Context, id string, u int64, add bool) error {
	change := func(t *clickup.Task) {
		var rest []clickup.User
		for _, a := range t.Assignees {
			if a.ID != u {
				rest = append(rest, a)
			}
		}
		if add {
			rest = append(rest, me)
		}
		t.Assignees = rest
	}
	if add {
		return f.record("assign "+id, change, id)
	}
	return f.record("unassign "+id, change, id)
}

var errOffline = errors.New("dial tcp: no network")

func sampleTasks() []clickup.Task {
	return []clickup.Task{
		{ID: "t1", CustomID: "DEV-7808", Name: "Compare preorder apps", Status: "in progress", StatusOrder: 2, ListID: "L1", ListName: "Harbor and Goods", Assignees: []clickup.User{me}, Due: testNow.AddDate(0, 0, -6), URL: "https://app.clickup.com/t/t1"},
		{ID: "t2", CustomID: "DEV-7700", Name: "Promo: Checkout personalisation", Status: "to start", StatusOrder: 1, ListID: "L2", ListName: "Snowcap", Assignees: []clickup.User{me}, Due: testNow, TypeName: "Coding"},
		{ID: "t3", CustomID: "DEV-7324", Name: "EasySubscribePro - Pause rule", Status: "pending", StatusOrder: 5, ListID: "L2", ListName: "Snowcap", Assignees: []clickup.User{malcolm}, Leads: []clickup.User{me}, Updated: testNow},
		{ID: "t4", CustomID: "DEV-7053", Name: "Research low stock notifications", Status: "to start", StatusOrder: 1, ListID: "L2", ListName: "Snowcap", Assignees: []clickup.User{me}},
		{ID: "t5", CustomID: "DEV-7142", Name: "Import old form answers", Status: "Closed", StatusOrder: 8, ListID: "L3", ListName: "Rosewood Studios", Assignees: []clickup.User{me}, Done: testNow.AddDate(0, 0, -2)},
	}
}

type testEnv struct {
	app     *App
	cu      *fakeClickUp
	rex     *fakeRex
	runner  *run.Fake
	cache   string
	state   string
	scratch string
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	cfg := config.Default()
	cfg.WorkspaceID, cfg.LeadFieldID = "77", "lead"
	dir := t.TempDir()
	cfg.ScratchRoot = filepath.Join(dir, "research")
	cfg.WorktreeRoot = filepath.Join(dir, "worktrees")
	e := &testEnv{
		cu:      &fakeClickUp{tasks: sampleTasks()},
		rex:     &fakeRex{sessions: map[string]string{}},
		runner:  &run.Fake{},
		cache:   filepath.Join(dir, "cache.json"),
		state:   filepath.Join(dir, "state.json"),
		scratch: cfg.ScratchRoot,
	}
	e.app = NewApp(Deps{
		Config:    cfg,
		ClickUp:   e.cu,
		Rex:       e.rex,
		Runner:    e.runner,
		CachePath: e.cache,
		StatePath: e.state,
		Cache: store.Cache{Repos: []workspace.Repo{
			{Name: "dawn", Path: "/r/dawn"},
			{Name: "harbor-and-goods", Path: "/r/harbor-and-goods"},
			{Name: "snowcap", Path: "/r/snowcap"},
			{Name: "snowcap-subscriptions", Path: "/r/snowcap-subscriptions"},
		}, ReposAt: testNow},
		Now: func() time.Time { return testNow },
	})
	e.app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return e
}

// load runs a full task refresh synchronously.
func (e *testEnv) load(t *testing.T) {
	t.Helper()
	cmd := e.app.refreshTasks(true)
	if cmd == nil {
		t.Fatal("no refresh command")
	}
	e.run(cmd)
}

// run executes a command and feeds every resulting message back, following
// batches. Tick commands are skipped.
func (e *testEnv) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	switch msg := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			e.run(c)
		}
	case tickMsg:
	default:
		_, next := e.app.Update(msg)
		e.run(next)
	}
}

func (e *testEnv) press(keys ...string) {
	for _, k := range keys {
		_, cmd := e.app.Update(key(k))
		e.run(cmd)
	}
}

func (e *testEnv) cacheOnDisk(t *testing.T) store.Cache {
	t.Helper()
	var c store.Cache
	if err := store.Load(e.cache, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

type fakeRex struct {
	mu        sync.Mutex
	sessions  map[string]string
	blocks    map[string]bool
	statuses  map[string]rex.Status
	lines     map[string]string
	calls     []string
	focusErr  error
	blocksErr error
	next      int
}

func (f *fakeRex) log(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}
func (f *fakeRex) Sessions(context.Context) (map[string]string, error) { return f.sessions, nil }
func (f *fakeRex) NewSession(_ context.Context, name, dir, window string, argv []string) (rex.Placement, error) {
	f.log("new-session " + name + " | " + dir + " | " + window + " | " + strings.Join(argv, " "))
	f.next++
	p := rex.Placement{SessionID: "session:" + name, WindowID: fmt.Sprintf("window:%d", f.next), BlockID: fmt.Sprintf("block:%d", f.next)}
	if f.sessions == nil {
		f.sessions = map[string]string{}
	}
	f.sessions[p.SessionID] = name
	if f.blocks == nil {
		f.blocks = map[string]bool{}
	}
	f.blocks[p.BlockID] = true
	return p, nil
}
func (f *fakeRex) NewWindow(_ context.Context, s, name, dir string, argv []string) (rex.Placement, error) {
	f.log("new-window " + s + " | " + name + " | " + dir + " | " + strings.Join(argv, " "))
	f.next++
	p := rex.Placement{SessionID: s, WindowID: fmt.Sprintf("window:%d", f.next), BlockID: fmt.Sprintf("block:%d", f.next)}
	if f.blocks == nil {
		f.blocks = map[string]bool{}
	}
	f.blocks[p.BlockID] = true
	return p, nil
}
func (f *fakeRex) Blocks(_ context.Context, s string) (map[string]bool, error) {
	if f.blocksErr != nil {
		return nil, f.blocksErr
	}
	return f.blocks, nil
}

func runResp(match string, err error) run.Response { return run.Response{Match: match, Err: err} }
func (f *fakeRex) Status(_ context.Context, s, b string) (rex.Status, error) {
	return f.statuses[b], nil
}
func (f *fakeRex) Focus(_ context.Context, s, w string) error {
	f.log("focus " + s + " " + w)
	return f.focusErr
}
func (f *fakeRex) CloseWindow(_ context.Context, s, w string) error {
	f.log("close " + s + " " + w)
	return nil
}
func (f *fakeRex) LastLine(_ context.Context, s, b string) (string, error) { return f.lines[b], nil }

func (e *testEnv) stateOnDisk(t *testing.T) store.State {
	t.Helper()
	var s store.State
	if err := store.Load(e.state, &s); err != nil {
		t.Fatal(err)
	}
	return s
}
