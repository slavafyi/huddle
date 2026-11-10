package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Tasks and PRs refresh together, once every 2 minutes, so the header
// shows one "syncing…" per cycle.
func TestOneSyncCycleForTasksAndPRs(t *testing.T) {
	e, _ := prEnv(t)
	now := testNow
	e.app.deps.Now = func() time.Time { return now }
	e.press("r") // starts a cycle, like the start does

	for _, d := range []time.Duration{15 * time.Second, 90 * time.Second, 115 * time.Second} {
		now = testNow.Add(d)
		e.app.Update(tickMsg(now))
		if e.app.loadingTasks || e.app.loadingPRs {
			t.Errorf("at %v: tasks %v PRs %v, want no refresh", d, e.app.loadingTasks, e.app.loadingPRs)
		}
	}
	now = testNow.Add(2 * time.Minute)
	e.app.Update(tickMsg(now))
	if !e.app.loadingTasks || !e.app.loadingPRs {
		t.Errorf("at 2m: tasks %v PRs %v, want both", e.app.loadingTasks, e.app.loadingPRs)
	}
}

// r starts a new cycle: the next one is 2 minutes after it.
func TestRefreshKeyRestartsCycle(t *testing.T) {
	e, _ := prEnv(t)
	now := testNow
	e.app.deps.Now = func() time.Time { return now }
	e.press("r")
	now = testNow.Add(time.Minute)
	e.press("r")
	now = testNow.Add(2 * time.Minute)
	e.app.Update(tickMsg(now))
	if e.app.loadingTasks || e.app.loadingPRs {
		t.Errorf("1m after r: tasks %v PRs %v, want no refresh", e.app.loadingTasks, e.app.loadingPRs)
	}
}

// "synced … ago" counts from the older of the task and PR syncs.
func TestSyncedAgeIsOldest(t *testing.T) {
	e, _ := prEnv(t)
	e.app.cache.TasksSyncedAt = testNow.Add(-10 * time.Second)
	e.app.cache.PRsAt = testNow.Add(-3 * time.Minute)
	if out := ansi.Strip(screen(e.app)); !strings.Contains(out, "synced 3m ago") {
		t.Errorf("header:\n%s", out)
	}
}
