package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/slavafyi/huddle/internal/config"
)

// withProfiles is newEnv in profile "work", with three profiles and
// "personal" used most recently after work.
func withProfiles(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t)
	d := e.app.deps
	d.Profile = "work"
	d.Profiles = []config.Profile{
		{Name: "work", ClickUp: true, GitHubOrgs: []string{"vertexdigital"}},
		{Name: "oss", GitHubAccount: "slavafyi"},
		{Name: "personal", GitHubAccount: "slavafyi"},
	}
	d.Recent = []string{"work", "personal"}
	e.app = NewApp(d)
	e.app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return e
}

func TestHeaderAndTitleShowProfile(t *testing.T) {
	e := withProfiles(t)
	if first := strings.Split(plain(e), "\n")[0]; !strings.HasSuffix(strings.TrimSpace(first), "huddle · work") {
		t.Errorf("header = %q", first)
	}
	if got := e.app.View().WindowTitle; got != "Huddle · work" {
		t.Errorf("title = %q", got)
	}
}

func pickerNames(e *testEnv) []string {
	var names []string
	for _, p := range e.app.profilePicker.shown() {
		names = append(names, p.Name)
	}
	return names
}

func TestProfilePickerOrderAndFilter(t *testing.T) {
	e := withProfiles(t)
	e.press("P")
	if e.app.mode != modeProfile {
		t.Fatal("P did not open the picker")
	}
	if got := strings.Join(pickerNames(e), " "); got != "work personal oss" {
		t.Errorf("order = %q, want recent first then file order", got)
	}
	out := plain(e)
	for _, want := range []string{"Profile", "1 work", "ClickUp · vertexdigital", "2 personal", "GitHub · slavafyi"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker missing %q:\n%s", want, out)
		}
	}
	e.press("o", "s")
	if got := strings.Join(pickerNames(e), " "); got != "oss" {
		t.Errorf("filtered = %q", got)
	}
	e.press("backspace", "backspace", "esc")
	if e.app.mode != modeNormal || e.app.SwitchTo() != "" {
		t.Errorf("esc: mode=%v switch=%q", e.app.mode, e.app.SwitchTo())
	}
}

func TestProfilePickerEnterSwitches(t *testing.T) {
	e := withProfiles(t)
	e.press("P", "p", "e", "r")
	_, cmd := e.app.Update(key("enter"))
	if e.app.SwitchTo() != "personal" {
		t.Fatalf("switch = %q", e.app.SwitchTo())
	}
	if cmd == nil {
		t.Fatal("no quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("switching must end the program")
	}
}

func TestProfilePickerNumberJumps(t *testing.T) {
	e := withProfiles(t)
	e.press("P")
	_, cmd := e.app.Update(key("3"))
	if e.app.SwitchTo() != "oss" || cmd == nil {
		t.Errorf("switch = %q", e.app.SwitchTo())
	}
}

func TestProfilePickerCurrentProfileJustCloses(t *testing.T) {
	e := withProfiles(t)
	e.press("P")
	_, cmd := e.app.Update(key("enter"))
	if e.app.SwitchTo() != "" || cmd != nil || e.app.mode != modeNormal {
		t.Errorf("switch = %q mode=%v", e.app.SwitchTo(), e.app.mode)
	}
}

func TestProfileInHelp(t *testing.T) {
	e := withProfiles(t)
	e.press("?")
	if !strings.Contains(plain(e), "switch profile") {
		t.Errorf("help:\n%s", plain(e))
	}
}

func TestNoSourcesMessage(t *testing.T) {
	e := withProfiles(t)
	d := e.app.deps
	d.Sources = nil
	e.app = NewApp(d)
	e.app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	for _, tab := range []string{"1", "2"} {
		e.press(tab)
		if out := plain(e); !strings.Contains(out, "no sources in this profile") || strings.Contains(out, "loading…") {
			t.Errorf("view %s:\n%s", tab, out)
		}
	}
}
