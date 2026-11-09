package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Blue is grey in some themes (koda-dark), so the active tab and filter
// must stand out by bold alone, never by color.
func TestActiveTabAndFilterAreBoldNotColored(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	bar := e.app.tabBar()
	if !strings.Contains(bar, sBold.Render("1 Today")) {
		t.Errorf("active tab not plain bold: %q", bar)
	}
	e.press("2")
	sub := e.app.tasks.subHeader(e.app.cache.Tasks)
	if !strings.Contains(sub, "\x1b[1mMine + Lead") {
		t.Errorf("active filter not bold: %q", sub)
	}
	for _, s := range []string{bar, sub} {
		if strings.Contains(s, "34m") || strings.Contains(s, "94m") {
			t.Errorf("blue used: %q", s)
		}
	}
}

// Every picker uses the list's grey selection; the Start panel keeps its
// bold › repo instead (no grey there).
func TestSameSelectionEverywhere(t *testing.T) {
	e := newEnv(t)
	e.cu.statuses = listStatuses
	e.load(t)
	e.press("2", "s")
	if out := strings.Join(e.app.renderPicker(), "\n"); !strings.Contains(out, selBG) || strings.Contains(out, "7m") {
		t.Errorf("status picker: %q", out)
	}
	e.press("esc")
	selectTask(t, e, "t2")
	e.press("enter", "tab") // → repo list
	if out := strings.Join(e.app.renderStart(40), "\n"); strings.Contains(out, selBG) || !strings.Contains(out, "› snowcap") {
		t.Errorf("panel repo list: %q", out)
	}
}

// The status picker matches the profile picker: number first, no › column,
// no hint line; the cursor is the grey row.
func TestStatusPickerLayout(t *testing.T) {
	e := newEnv(t)
	e.cu.statuses = listStatuses
	e.load(t)
	e.press("2", "s")
	lines := e.app.renderPicker()
	for _, l := range lines[1 : len(lines)-1] {
		p := ansi.Strip(l)
		if strings.Contains(p, "›") || strings.Contains(p, "enter apply") {
			t.Errorf("picker line %q", p)
		}
		if body := strings.TrimPrefix(p, "│ "); strings.Contains(body, "■") && (body[0] < '1' || body[0] > '9') {
			t.Errorf("row must start with its number: %q", p)
		}
	}
}
