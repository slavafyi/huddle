package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "shift+enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+n":
		return tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
	case "ctrl+p":
		return tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func press(t *testing.T, m tea.Model, keys ...string) (tea.Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		m, cmd = m.Update(key(k))
	}
	return m, cmd
}

func screen(m tea.Model) string {
	return m.(*App).Render()
}

func TestTabsSwitchWithNumberKeys(t *testing.T) {
	var m tea.Model = NewApp(Deps{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if !strings.Contains(screen(m), "1 Today") {
		t.Fatalf("tab bar missing:\n%s", screen(m))
	}
	for k, want := range map[string]Tab{"2": TabTasks, "3": TabPRs, "4": TabAgents, "1": TabToday} {
		m, _ = press(t, m, k)
		if got := m.(*App).tab; got != want {
			t.Errorf("key %s: tab = %v, want %v", k, got, want)
		}
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		_, cmd := press(t, NewApp(Deps{}), k)
		if cmd == nil {
			t.Fatalf("%s: no command", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s: want QuitMsg", k)
		}
	}
}

func TestViewUsesAltScreenAndTitle(t *testing.T) {
	v := NewApp(Deps{}).View()
	if !v.AltScreen {
		t.Error("want alt screen")
	}
	if v.WindowTitle != "CUddle" {
		t.Errorf("title = %q", v.WindowTitle)
	}
}

func TestTabCyclesViews(t *testing.T) {
	var m tea.Model = NewApp(Deps{})
	want := []Tab{TabTasks, TabPRs, TabAgents, TabToday}
	for _, w := range want {
		m, _ = press(t, m, "tab")
		if got := m.(*App).tab; got != w {
			t.Fatalf("tab: got %v want %v", got, w)
		}
	}
	m, _ = press(t, m, "shift+tab")
	if got := m.(*App).tab; got != TabAgents {
		t.Errorf("shift+tab from Today: got %v", got)
	}
}
