package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A message stays until the next key press; a key that sets a new
// message keeps it.
func TestFlashClearsOnNextKey(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	e.app.flash = sGreen.Render("→ DEV-7053 in Snowcap")
	e.press("j")
	if e.app.flash != "" {
		t.Errorf("flash after j: %q", ansi.Strip(e.app.flash))
	}
	e.press("`") // no other profile: sets a new message
	if !strings.Contains(ansi.Strip(e.app.flash), "no other profile") {
		t.Errorf("new message lost: %q", ansi.Strip(e.app.flash))
	}
}

// "refreshing…" goes away when the refresh ends.
func TestRefreshingClearsWhenDone(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	e.press("r") // runs the refresh to the end
	if strings.Contains(ansi.Strip(screen(e.app)), "refreshing…") {
		t.Errorf("still refreshing:\n%s", ansi.Strip(screen(e.app)))
	}
}

// A note or error from the refresh replaces "refreshing…" and stays.
func TestRefreshNoteReplacesRefreshing(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	e.app.flash = sFaint.Render("refreshing…")
	e.app.Update(tasksMsg{results: []fetched{{source: "clickup", note: "lead features off", err: errors.New("x")}}})
	if got := ansi.Strip(e.app.flash); got != "lead features off" {
		t.Errorf("flash = %q", got)
	}
}
