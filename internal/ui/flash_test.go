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

// The header says "syncing…" while tasks refresh, in the background or
// after r, and "synced …" once done. r puts nothing in the flash line.
func TestHeaderShowsSyncingTasks(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	cmd := e.app.refreshTasks(false) // started, not finished
	if out := ansi.Strip(screen(e.app)); !strings.Contains(out, "syncing…") || strings.Contains(out, "synced") {
		t.Errorf("while refreshing:\n%s", out)
	}
	e.run(cmd)
	if out := ansi.Strip(screen(e.app)); strings.Contains(out, "syncing…") || !strings.Contains(out, "synced") {
		t.Errorf("after refresh:\n%s", out)
	}
	e.press("r")
	if e.app.flash != "" {
		t.Errorf("r flash: %q", ansi.Strip(e.app.flash))
	}
}

func TestHeaderShowsSyncingPRs(t *testing.T) {
	e, _ := prEnv(t)
	cmd := e.app.refreshPRs()
	if out := ansi.Strip(screen(e.app)); !strings.Contains(out, "syncing…") {
		t.Errorf("while PRs refresh:\n%s", out)
	}
	e.run(cmd)
	if out := ansi.Strip(screen(e.app)); strings.Contains(out, "syncing…") {
		t.Errorf("after PRs refresh:\n%s", out)
	}
}

// A note from the refresh shows in the flash line.
func TestRefreshNoteShows(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	e.app.Update(tasksMsg{results: []fetched{{source: "clickup", note: "lead features off", err: errors.New("x")}}})
	if got := ansi.Strip(e.app.flash); got != "lead features off" {
		t.Errorf("flash = %q", got)
	}
}
