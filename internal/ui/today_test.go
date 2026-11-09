package ui

import (
	"strings"
	"testing"
)

func TestTodayShowsGroupsInOrder(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	out := plain(e) // Today is the first tab
	order := []string{"In progress 1", "DEV-7808", "Up next 2", "DEV-7700", "DEV-7053", "Waiting on others 1", "DEV-7324", "Done this week 1", "DEV-7142"}
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
	if !strings.Contains(out, "Mon, Nov 9 · 1 in progress · 2 up next · 1 waiting · 1 done") {
		t.Errorf("summary line wrong:\n%s", out)
	}
}

func TestTodayOverdueAndTodayDates(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	out := plain(e)
	if !strings.Contains(out, "Nov 3") || !strings.Contains(out, "today") {
		t.Errorf("dates:\n%s", out)
	}
}

func TestTaskActionsWorkFromToday(t *testing.T) {
	e := newEnv(t)
	e.cu.statuses = listStatuses
	e.load(t)
	// First row of Today is DEV-7808 (in progress).
	if r, _ := e.app.today.list.selected(); r.key != "t1" {
		t.Fatalf("selected %s", r.key)
	}
	e.press("s")
	if e.app.mode != modeStatus || !strings.Contains(plain(e), "Status · DEV-7808") {
		t.Fatalf("picker not open from Today:\n%s", plain(e))
	}
}
