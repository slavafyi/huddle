package ui

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/slavafyi/cuddle/internal/clickup"
	"github.com/slavafyi/cuddle/internal/config"
	"github.com/slavafyi/cuddle/internal/run"
	"github.com/slavafyi/cuddle/internal/store"
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
func (f *fakeClickUp) record(s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, s)
	return f.writeErr
}
func (f *fakeClickUp) SetStatus(_ context.Context, id, st string) error {
	return f.record("status " + id + " " + st)
}
func (f *fakeClickUp) AddComment(_ context.Context, id, text string) error {
	return f.record("comment " + id + " " + text)
}
func (f *fakeClickUp) SetAssignee(_ context.Context, id string, u int64, add bool) error {
	if add {
		return f.record("assign " + id)
	}
	return f.record("unassign " + id)
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
	app    *App
	cu     *fakeClickUp
	runner *run.Fake
	cache  string
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	cfg := config.Default()
	cfg.WorkspaceID, cfg.LeadFieldID = "77", "lead"
	e := &testEnv{
		cu:     &fakeClickUp{tasks: sampleTasks()},
		runner: &run.Fake{},
		cache:  filepath.Join(t.TempDir(), "cache.json"),
	}
	e.app = NewApp(Deps{
		Config:    cfg,
		ClickUp:   e.cu,
		Runner:    e.runner,
		CachePath: e.cache,
		StatePath: filepath.Join(t.TempDir(), "state.json"),
		Now:       func() time.Time { return testNow },
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
