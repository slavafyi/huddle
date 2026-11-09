package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/slavafyi/huddle/internal/clickup"
)

var listStatuses = []clickup.Status{
	{Name: "to plan", Type: "open", Order: 0},
	{Name: "to start", Type: "unstarted", Order: 1},
	{Name: "in progress", Type: "custom", Order: 2},
	{Name: "to review", Type: "custom", Order: 3},
	{Name: "Closed", Type: "closed", Order: 8},
}

// selectTask moves the Tasks view cursor to the task with internal id.
func selectTask(t *testing.T, e *testEnv, id string) {
	t.Helper()
	e.press("2")
	for i := 0; i < 20; i++ {
		if r, _ := e.app.tasks.list.selected(); r.key == ref(id) {
			return
		}
		e.press("j")
	}
	t.Fatalf("task %s not found", id)
}

func status(e *testEnv, id string) string {
	tk, _ := e.app.taskByID(id)
	return tk.Status
}

func TestStatusPickerChangesStatus(t *testing.T) {
	e := newEnv(t)
	e.cu.statuses = listStatuses
	e.load(t)
	selectTask(t, e, "t2") // to start
	e.press("s")
	out := plain(e)
	for _, want := range []string{"Status · DEV-7700", "to plan", "in progress", "(current)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("picker missing %q:\n%s", want, out)
		}
	}
	e.press("j", "enter") // cursor starts on current (to start) → in progress
	if got := e.cu.writes; len(got) != 1 || got[0] != "status t2 in progress" {
		t.Fatalf("writes = %v", got)
	}
	if s := status(e, "t2"); s != "in progress" {
		t.Errorf("status = %q", s)
	}
	if e.app.mode != modeNormal {
		t.Error("picker still open")
	}
}

func TestStatusChangeIsOptimisticAndUndoneOnError(t *testing.T) {
	e := newEnv(t)
	e.cu.statuses = listStatuses
	e.load(t)
	selectTask(t, e, "t2")
	e.press("s")
	e.cu.writeErr = errors.New("clickup PUT /task/t2: 500")
	// Press enter without running the command: the change shows at once.
	_, cmd := e.app.Update(key("4")) // quick pick: 4th status = to review
	if s := status(e, "t2"); s != "to review" {
		t.Fatalf("not optimistic: %q", s)
	}
	e.run(cmd)
	if s := status(e, "t2"); s != "to start" {
		t.Errorf("not undone: %q", s)
	}
	if !strings.Contains(plain(e), "500") {
		t.Errorf("error not shown:\n%s", plain(e))
	}
}

func TestStatusPickerEscWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.cu.statuses = listStatuses
	e.load(t)
	selectTask(t, e, "t2")
	e.press("s", "j", "esc")
	if len(e.cu.writes) != 0 || e.app.mode != modeNormal {
		t.Errorf("writes=%v mode=%v", e.cu.writes, e.app.mode)
	}
}

func TestCommentSends(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("c", "h", "i", " ", "x", "backspace", "Y", "enter")
	if got := e.cu.writes; len(got) != 1 || got[0] != "comment t2 hi Y" {
		t.Fatalf("writes = %v", got)
	}
	if !strings.Contains(plain(e), "comment added") {
		t.Errorf("no confirmation:\n%s", plain(e))
	}
}

func TestCommentEscAndEmptySendNothing(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("c", "h", "esc")
	e.press("c", "enter")
	if len(e.cu.writes) != 0 {
		t.Errorf("writes = %v", e.cu.writes)
	}
}

func TestCommentKeysDoNotTriggerShortcuts(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("c", "q", "2", "enter")
	if got := e.cu.writes; len(got) != 1 || got[0] != "comment t2 q2" {
		t.Fatalf("writes = %v", got)
	}
}

func TestAssignToggle(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t3") // lead only, assigned to Malcolm
	e.press("a")
	tk, _ := e.app.taskByID("t3")
	if !tk.Mine || e.cu.writes[0] != "assign t3" {
		t.Fatalf("assign: writes=%v", e.cu.writes)
	}
	e.press("a")
	tk, _ = e.app.taskByID("t3")
	if tk.Mine || e.cu.writes[1] != "unassign t3" {
		t.Fatalf("unassign: writes=%v", e.cu.writes)
	}
}

func TestAssignUndoneOnError(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t3")
	e.cu.writeErr = errors.New("forbidden")
	e.press("a")
	tk, _ := e.app.taskByID("t3")
	if tk.Mine {
		t.Error("assign not undone")
	}
}
