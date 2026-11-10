package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/slavafyi/huddle/internal/clickup"
	"github.com/slavafyi/huddle/internal/store"
	"github.com/slavafyi/huddle/internal/task"
)

func plain(e *testEnv) string { return ansi.Strip(e.app.Render()) }

func TestTasksViewShowsGroupsAfterLoad(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	e.press("2")
	out := plain(e)
	for _, want := range []string{"Mine + Lead", "to start 2", "in progress 1", "pending 1", "DEV-7700", "Promo: Checkout personalisation", "Snowcap", "synced"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Done tasks are not in the Tasks view.
	if strings.Contains(out, "DEV-7142") {
		t.Error("closed task shown in Tasks view")
	}
	// Status groups follow the status order: to start (1) before in progress (2).
	if strings.Index(out, "to start 2") > strings.Index(out, "in progress 1") {
		t.Error("groups not in workflow order")
	}
}

func TestTasksFilterCycles(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	e.press("2", "t")
	out := plain(e)
	if strings.Contains(out, "DEV-7324") {
		t.Error("Assigned filter shows lead-only task")
	}
	e.press("t")
	out = plain(e)
	if !strings.Contains(out, "DEV-7324") || strings.Contains(out, "DEV-7700") {
		t.Errorf("Lead only filter wrong:\n%s", out)
	}
}

func TestSelectionMovesAndShowsDetail(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	e.press("2")
	first, _ := e.app.tasks.list.selected()
	e.press("j")
	second, _ := e.app.tasks.list.selected()
	if first.key == second.key {
		t.Fatal("j did not move")
	}
	task, _ := e.app.taskByRef(second.key)
	_, _, detail := e.app.viewParts()
	got := ansi.Strip(strings.Join(detail, "\n"))
	if !strings.Contains(got, task.Name) || !strings.Contains(got, task.Key) {
		t.Errorf("detail for %s wrong:\n%s", task.Key, got)
	}
}

func TestRefreshSavesCache(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	c := e.cacheOnDisk(t)
	if c.Version != store.CacheVersion || len(c.Tasks) != 5 || c.Me[clickup.SourceName] != 1 || !c.TasksFullAt.Equal(testNow) {
		t.Errorf("cache: version=%d tasks=%d me=%d full=%v", c.Version, len(c.Tasks), c.Me[clickup.SourceName], c.TasksFullAt)
	}
}

func TestStartsFromCacheAndShowsOfflineOnError(t *testing.T) {
	e := newEnv(t)
	e.app.applyCache(store.Cache{Version: store.CacheVersion, Me: map[string]int64{clickup.SourceName: me.ID}, Tasks: cachedTasks(), TasksSyncedAt: testNow.Add(-12 * time.Minute), TasksFullAt: testNow.Add(-12 * time.Minute)})
	e.press("2")
	if !strings.Contains(plain(e), "DEV-7700") {
		t.Fatal("cached tasks not shown before refresh")
	}
	e.cu.err = errOffline
	e.run(e.app.refreshTasks(false))
	out := plain(e)
	if !strings.Contains(out, "offline · cached 12m ago") {
		t.Errorf("no offline label:\n%s", out)
	}
	if !strings.Contains(out, "DEV-7700") {
		t.Error("cached tasks lost after failed refresh")
	}
}

func TestIncrementalRefreshUsesLastSync(t *testing.T) {
	e := newEnv(t)
	e.app.applyCache(store.Cache{Version: store.CacheVersion, Me: map[string]int64{clickup.SourceName: me.ID}, Tasks: cachedTasks(), TasksSyncedAt: testNow.Add(-5 * time.Minute), TasksFullAt: testNow.Add(-5 * time.Minute)})
	e.cu.tasks = []clickup.Task{{ID: "t2", CustomID: "DEV-7700", Name: "Renamed", Status: "to start", Assignees: []clickup.User{me}}}
	e.run(e.app.refreshTasks(false))
	if got := e.cu.sinces[0]; !got.Equal(testNow.Add(-6 * time.Minute)) {
		t.Errorf("since = %v", got)
	}
	e.press("2")
	if !strings.Contains(plain(e), "Renamed") || !strings.Contains(plain(e), "DEV-7808") {
		t.Errorf("incremental merge wrong:\n%s", plain(e))
	}
}

// A cycle's task refresh is incremental, and full every 30 minutes.
func TestCycleDecidesFullRefresh(t *testing.T) {
	e := newEnv(t)
	e.app.applyCache(store.Cache{Version: store.CacheVersion, Me: map[string]int64{clickup.SourceName: me.ID}, TasksSyncedAt: testNow.Add(-time.Minute), TasksFullAt: testNow.Add(-time.Minute)})
	if e.app.tasksFull() {
		t.Error("want incremental refresh")
	}
	e.app.cache.TasksFullAt = testNow.Add(-31 * time.Minute)
	if !e.app.tasksFull() {
		t.Error("want full refresh")
	}
}

func TestOpenTaskInBrowser(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	e.press("2")
	for i := 0; i < 5; i++ {
		if r, _ := e.app.tasks.list.selected(); r.key == ref("t1") {
			break
		}
		e.press("j")
	}
	e.press("o")
	calls := e.runner.Calls()
	if len(calls) != 1 || calls[0] != "open https://app.clickup.com/t/t1" {
		t.Errorf("calls = %v", calls)
	}
}

func TestNoLeadFieldHidesLeadFeaturesAndShowsNote(t *testing.T) {
	e := newEnv(t)
	src := &clickup.Source{Client: e.cu, NoLead: true}
	src.SetNote("ClickUp: no lead field; lead features are off", false)
	d := e.app.deps
	d.Sources = []task.Source{src}
	e.app = NewApp(d)
	e.app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	e.load(t)
	out := plain(e)
	if !strings.Contains(out, "lead features are off") || strings.Contains(out, task.GroupWaiting) {
		t.Errorf("Today:\n%s", out)
	}
	e.press("2")
	if out := plain(e); strings.Contains(out, "Lead") || !strings.Contains(out, "Assigned") {
		t.Errorf("Tasks filters:\n%s", out)
	}
	e.press("t")
	if e.app.tasks.filter != task.FilterAll {
		t.Errorf("filter %v: t should stay on the only filter", e.app.tasks.filter)
	}
	e.load(t)
	if strings.Contains(plain(e), "lead features are off") {
		t.Error("note shown again after the next refresh")
	}
}

func TestSourceWarningGoesToFlash(t *testing.T) {
	e := newEnv(t)
	e.app.deps.Sources[0].(*clickup.Source).SetNote("ClickUp settings no longer matched: lead field a → b", true)
	e.load(t)
	if !strings.Contains(plain(e), "no longer matched") {
		t.Errorf("no warning:\n%s", plain(e))
	}
}
