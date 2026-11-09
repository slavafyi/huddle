package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
	_, cmd := e.app.Update(key("1"))
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

func TestProfilePickerFitsAt80Columns(t *testing.T) {
	e := withProfiles(t)
	e.app.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	e.press("P")
	var line string
	for _, l := range strings.Split(plain(e), "\n") {
		if strings.Contains(l, "● 1 work") {
			line = l
		}
	}
	if !strings.Contains(line, "ClickUp · vertexdigital") || strings.Contains(line, "…") {
		t.Errorf("current profile line cut or unmarked: %q\n%s", line, plain(e))
	}
	if strings.Contains(plain(e), "● 2 personal") {
		t.Error("only the current profile is marked")
	}
}

// The picker has fixed columns (mark, number, name, summary on the right)
// and a reversed cursor row that starts on the next profile.
func TestProfilePickerLayout(t *testing.T) {
	e := withProfiles(t)
	e.press("P")
	if e.app.profilePicker.cursor != 1 {
		t.Errorf("cursor = %d, want the next profile", e.app.profilePicker.cursor)
	}
	lines := e.app.renderProfilePicker()
	var rows []string
	for _, l := range lines {
		if strings.Contains(l, "work") || strings.Contains(l, "personal") || strings.Contains(l, "oss") {
			rows = append(rows, l)
		}
	}
	if len(rows) != 3 {
		t.Fatalf("rows: %q", rows)
	}
	if !strings.Contains(rows[1], "7m") || strings.Contains(rows[0], "7m") {
		t.Errorf("cursor row not reversed: %q", rows)
	}
	// col is the display column where s starts in row r.
	col := func(r, s string) int { return ansi.StringWidth(r[:strings.Index(r, s)]) }
	p := make([]string, len(rows))
	for i, r := range rows {
		p[i] = ansi.Strip(r)
	}
	if col(p[0], "1 work") != col(p[1], "2 personal") || col(p[1], "2 personal") != col(p[2], "3 oss") {
		t.Errorf("number and name columns not aligned: %q", p)
	}
	end := func(r, s string) int { return col(r, s) + ansi.StringWidth(s) }
	if end(p[0], "vertexdigital") != end(p[1], "slavafyi") {
		t.Errorf("summaries not right-aligned: %q", p)
	}
}
