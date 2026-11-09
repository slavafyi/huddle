package ui

import (
	"strings"
	"testing"
)

// Key hints live only in the ? popup, not around the screen.
func TestNoKeyHintsOnScreen(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	for _, tab := range []string{"1", "2", "3", "4"} {
		e.press(tab)
		out := plain(e)
		for _, hint := range []string{"j/k", "q quit", "enter start", "enter focus", "enter diff", "t switch", "o open in", "o open on"} {
			if strings.Contains(out, hint) {
				t.Errorf("view %s still shows %q:\n%s", tab, hint, out)
			}
		}
		if !strings.Contains(out, "? keys") {
			t.Errorf("view %s: no '? keys'", tab)
		}
	}
}

func TestPanelHasNoKeyHints(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter")
	out := plain(e)
	for _, hint := range []string{"· space", "tab field", "esc close", "type to filter", "ctrl+n"} {
		if strings.Contains(out, hint) {
			t.Errorf("panel shows %q:\n%s", hint, out)
		}
	}
	for _, label := range []string{"Workspace", "Repo", "Branch", "Agent", "Prompt", "Launch ⏎"} {
		if !strings.Contains(out, label) {
			t.Errorf("panel missing %q", label)
		}
	}
	e.press("tab", "s", "u", "b")
	if !strings.Contains(plain(e), `Repo · "sub"`) {
		t.Errorf("filter not shown:\n%s", plain(e))
	}
}

func TestHelpPopupPerView(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	e.press("2", "?")
	if e.app.mode != modeHelp {
		t.Fatal("? did not open help")
	}
	out := plain(e)
	for _, want := range []string{"Tasks", "start / focus", "status", "comment", "Start panel", "change value", "Anywhere", "quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
	e.press("x") // any key closes, and does nothing else
	if e.app.mode != modeNormal || e.app.tab != TabTasks {
		t.Errorf("mode=%v tab=%v", e.app.mode, e.app.tab)
	}
	e.press("4", "?")
	if out := plain(e); !strings.Contains(out, "Agents") || !strings.Contains(out, "close tab") {
		t.Errorf("agents help:\n%s", out)
	}
}

func TestQuestionMarkIsTextInPanel(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter", "tab", "tab", "tab", "tab", "?") // → prompt
	if e.app.mode != modeStart || !strings.HasSuffix(e.app.start.prompt, "?") {
		t.Errorf("mode=%v prompt=%q", e.app.mode, e.app.start.prompt)
	}
}
