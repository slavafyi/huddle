package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/slavafyi/huddle/internal/rex"
	"github.com/slavafyi/huddle/internal/store"
)

func agentsEnv(t *testing.T) *testEnv {
	e := newEnv(t)
	e.rex.blocks = map[string]bool{"block:1": true, "block:2": true}
	e.rex.statuses = map[string]rex.Status{
		"block:1": {State: "working"},
		"block:2": {State: "blocked", Msg: "Which Railway environment?"},
	}
	e.rex.lines = map[string]string{"block:1": "› Running pnpm test…"}
	add := func(task, key, block string, started time.Duration) {
		e.app.state.AddAgent(store.Agent{Source: "clickup", TaskID: task, TaskKey: key, Title: key + " title", Client: "Snowcap",
			Workspace: "Scratch", Dir: "/tmp/" + key, Agent: "claude", Started: testNow.Add(-started),
			Placement: rex.Placement{SessionID: "session:S", WindowID: "window:" + block, BlockID: block}})
	}
	add("t1", "DEV-7808", "block:1", 24*time.Minute)
	add("t2", "DEV-7700", "block:2", time.Hour)
	add("t4", "DEV-7053", "block:3", 2*time.Hour) // tab already closed
	e.load(t)
	e.run(e.app.refreshAgents())
	return e
}

func TestAgentsViewShowsLiveStates(t *testing.T) {
	e := agentsEnv(t)
	e.press("4")
	out := plain(e)
	for _, want := range []string{"Needs input 1", "DEV-7700", "Working 1", "DEV-7808", "Closed 1", "DEV-7053", "24m"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "● 1 working") || !strings.Contains(out, "! 1 waiting") {
		t.Errorf("header counters wrong:\n%s", out)
	}
	if strings.Index(out, "Needs input") > strings.Index(out, "Working") {
		t.Error("needs input should come first")
	}
}

func TestAgentDetailShowsLastLineAndMessage(t *testing.T) {
	e := agentsEnv(t)
	e.press("4")
	_, _, detail := e.app.viewParts()
	if got := strings.Join(detail, "\n"); !strings.Contains(got, "Which Railway environment?") {
		t.Errorf("blocked message missing: %s", got)
	}
	e.press("j") // Working: DEV-7808
	_, _, detail = e.app.viewParts()
	if got := strings.Join(detail, "\n"); !strings.Contains(got, "Running pnpm test") {
		t.Errorf("last line missing: %s", got)
	}
}

func TestEventsUpdateState(t *testing.T) {
	e := agentsEnv(t)
	e.app.Update(agentEventMsg{ev: rex.Event{BlockID: "block:1", HasStatus: true, Status: rex.Status{State: "done"}}})
	e.app.Update(agentEventMsg{ev: rex.Event{BlockID: "block:2", Closed: true}})
	e.press("4")
	out := plain(e)
	if !strings.Contains(out, "Done 1") || !strings.Contains(out, "Closed 2") || strings.Contains(out, "Working") {
		t.Errorf("states after events:\n%s", out)
	}
}

func TestAgentStateShownOnToday(t *testing.T) {
	e := agentsEnv(t)
	out := plain(e) // Today
	if !strings.Contains(out, "● agent working") {
		t.Errorf("Today missing agent state:\n%s", out)
	}
	e.press("2")
	if !strings.Contains(plain(e), "! needs input") {
		t.Errorf("Tasks missing agent badge:\n%s", plain(e))
	}
}

func TestCloseAgentTabAsksFirst(t *testing.T) {
	e := agentsEnv(t)
	e.press("4", "j") // Working: DEV-7808, block:1
	e.press("x")
	if !strings.Contains(plain(e), "close tab DEV-7808?") {
		t.Fatalf("no confirmation:\n%s", plain(e))
	}
	e.press("n")
	if len(e.rex.calls) != 0 || len(e.app.state.Agents) != 3 {
		t.Fatal("n should cancel")
	}
	e.press("x", "y")
	if len(e.rex.calls) != 1 || e.rex.calls[0] != "close session:S window:block:1" {
		t.Errorf("rex calls = %v", e.rex.calls)
	}
	if len(e.app.state.Agents) != 2 {
		t.Errorf("agent not forgotten: %d", len(e.app.state.Agents))
	}
	if st := e.stateOnDisk(t); len(st.Agents) != 2 {
		t.Errorf("state not saved: %d", len(st.Agents))
	}
}

func TestRemoveClosedAgentWithoutRex(t *testing.T) {
	e := agentsEnv(t)
	e.press("4", "j", "j") // Closed: DEV-7053
	e.press("x", "y")
	if len(e.rex.calls) != 0 {
		t.Errorf("closed tab should not call rex: %v", e.rex.calls)
	}
	if len(e.app.state.Agents) != 2 {
		t.Errorf("agents = %d", len(e.app.state.Agents))
	}
}

func TestEnterFocusesFromAgents(t *testing.T) {
	e := agentsEnv(t)
	e.press("4", "enter")
	if len(e.rex.calls) != 1 || e.rex.calls[0] != "focus session:S window:block:2" {
		t.Errorf("rex calls = %v", e.rex.calls)
	}
}
