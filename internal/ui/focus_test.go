package ui

import (
	"strings"
	"testing"

	"github.com/slavafyi/cuddle/internal/rex"
	"github.com/slavafyi/cuddle/internal/store"
)

func withAgent(e *testEnv, taskID, block string) {
	e.app.state.AddAgent(store.Agent{TaskID: taskID, TaskKey: "DEV-7700", Client: "Snowcap",
		Placement: rex.Placement{SessionID: "session:S", WindowID: "window:" + block, BlockID: block}})
}

func TestEnterFocusesLiveAgentTab(t *testing.T) {
	e := newEnv(t)
	e.rex.blocks = map[string]bool{"block:9": true}
	withAgent(e, "t2", "block:9")
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter")
	if e.app.mode == modeStart {
		t.Fatal("panel opened for a task with a live tab")
	}
	if len(e.rex.calls) != 1 || e.rex.calls[0] != "focus session:S window:block:9" {
		t.Errorf("rex calls = %v", e.rex.calls)
	}
}

func TestEnterOpensPanelWhenAgentTabIsGone(t *testing.T) {
	e := newEnv(t)
	e.rex.blocks = map[string]bool{}
	withAgent(e, "t2", "block:9")
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter")
	if e.app.mode != modeStart {
		t.Fatal("panel should open when the old tab is gone")
	}
}

func TestShiftEnterAlwaysOpensPanel(t *testing.T) {
	e := newEnv(t)
	e.rex.blocks = map[string]bool{"block:9": true}
	withAgent(e, "t2", "block:9")
	e.load(t)
	selectTask(t, e, "t2")
	e.press("shift+enter")
	if e.app.mode != modeStart || len(e.rex.calls) != 0 {
		t.Errorf("mode=%v rex=%v", e.app.mode, e.rex.calls)
	}
}

func TestFocusWithoutRemoteControlShowsHint(t *testing.T) {
	e := newEnv(t)
	e.rex.blocks = map[string]bool{"block:9": true}
	e.rex.focusErr = rex.ErrRemoteControl
	withAgent(e, "t2", "block:9")
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter")
	if !strings.Contains(plain(e), "Remote Control") {
		t.Errorf("no hint:\n%s", plain(e))
	}
}
