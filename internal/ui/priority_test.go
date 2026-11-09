package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func listText(l *list) string { return ansi.Strip(l.render(140, 60)) }

func TestPriorityMarksOnlyHighAndUrgent(t *testing.T) {
	e := newEnv(t)
	e.cu.tasks[1].Priority = "urgent" // t2 DEV-7700
	e.cu.tasks[3].Priority = "high"   // t4 DEV-7053
	e.cu.tasks[0].Priority = "normal" // t1 DEV-7808
	e.load(t)
	for name, l := range map[string]*list{"tasks": &e.app.tasks.list, "today": &e.app.today.list} {
		out := listText(l)
		if !strings.Contains(out, "!! Promo: Checkout personalisation") {
			t.Errorf("%s: urgent mark missing:\n%s", name, out)
		}
		if !strings.Contains(out, "! Research low stock notifications") || strings.Contains(out, "!! Research") {
			t.Errorf("%s: high mark wrong:\n%s", name, out)
		}
		if strings.Contains(out, "! Evaluate preorder") {
			t.Errorf("%s: normal priority must not be marked", name)
		}
	}
}

func TestNoWhoColumnInLists(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	tasks := listText(&e.app.tasks.list)
	if strings.Contains(tasks, "WITH") || strings.Contains(tasks, "Malcolm") || strings.Contains(tasks, "nobody") {
		t.Errorf("Tasks still shows who has the task:\n%s", tasks)
	}
	if today := listText(&e.app.today.list); strings.Contains(today, "Malcolm") {
		t.Errorf("Today still shows who has the task:\n%s", today)
	}
	// It is still in the detail area.
	selectTask(t, e, "t3")
	_, _, detail := e.app.viewParts()
	if !strings.Contains(ansi.Strip(strings.Join(detail, " ")), "with Malcolm") {
		t.Error("detail lost who has the task")
	}
}

func TestEnterLaunchesFromPanel(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t4")
	e.press("enter", "tab", "tab", "enter") // open, move to a field, launch
	if e.app.mode != modeNormal || len(e.rex.calls) == 0 {
		t.Errorf("enter did not launch: mode=%v rex=%v", e.app.mode, e.rex.calls)
	}
}
