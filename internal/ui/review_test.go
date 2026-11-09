package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/slavafyi/cuddle/internal/clickup"
	"github.com/slavafyi/cuddle/internal/github"
	"github.com/slavafyi/cuddle/internal/rex"
	"github.com/slavafyi/cuddle/internal/run"
	"github.com/slavafyi/cuddle/internal/store"
	"github.com/slavafyi/cuddle/internal/workspace"
)

// A refresh that started before a write finished must not undo it on screen.
func TestStaleRefreshDoesNotRevertWrite(t *testing.T) {
	e := newEnv(t)
	e.cu.statuses = listStatuses
	e.load(t)
	selectTask(t, e, "t2")
	// A refresh that started before the write (seq 0) and still has the old
	// status arrives after the write succeeded.
	old := append([]clickup.Task(nil), e.cu.tasks...)
	e.app.loadingTasks = true  // that refresh is still running…
	e.press("s", "j", "enter") // …when the status write succeeds
	if status(e, "t2") != "in progress" {
		t.Fatalf("status = %q", status(e, "t2"))
	}
	e.app.Update(tasksMsg{me: me, tasks: old, at: testNow, seq: 0})
	if s := status(e, "t2"); s != "in progress" {
		t.Errorf("stale refresh reverted the write: %q", s)
	}
}

func TestSuccessfulWriteTriggersRefresh(t *testing.T) {
	e := newEnv(t)
	e.cu.statuses = listStatuses
	e.load(t)
	before := len(e.cu.sinces)
	selectTask(t, e, "t2")
	e.press("s", "j", "enter")
	if len(e.cu.sinces) <= before {
		t.Error("no refresh after a successful write")
	}
}

// A failed status write must only undo the status, not a later assign.
func TestUndoOnlyTheFailedField(t *testing.T) {
	e := newEnv(t)
	e.cu.statuses = listStatuses
	e.load(t)
	selectTask(t, e, "t3") // pending, with Malcolm
	e.press("s")
	_, statusCmd := e.app.Update(key("3")) // → in progress, not sent yet
	e.press("a")                           // assign me: succeeds
	e.cu.writeErr = errors.New("500")
	e.run(statusCmd)
	tk, _ := e.app.taskByID("t3")
	if tk.Status != "pending" {
		t.Errorf("status not undone: %q", tk.Status)
	}
	if !tk.AssignedTo(me.ID) {
		t.Error("undo of the status wiped the assign")
	}
}

func TestEmptyStatusListDoesNotOpenPicker(t *testing.T) {
	e := newEnv(t)
	e.cu.statuses = []clickup.Status{}
	e.load(t)
	selectTask(t, e, "t2")
	e.press("s", "j", "enter")
	if e.app.mode == modeStatus {
		t.Error("picker opened with no statuses")
	}
}

func TestPasteIntoComment(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("c")
	e.app.Update(tea.PasteMsg{Content: "pasted\ntext"})
	e.press("enter")
	if got := e.cu.writes; len(got) != 1 || got[0] != "comment t2 pasted text" {
		t.Errorf("writes = %v", got)
	}
}

// The agent is recorded as soon as its tab opens, before ClickUp answers.
func TestAgentSavedBeforeClickUpWrites(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t4")
	e.press("enter")
	_, launchCmd := e.app.Update(key("ctrl+s"))
	opened := launchCmd()
	if len(e.cu.writes) != 0 {
		t.Fatalf("ClickUp written before the tab was recorded: %v", e.cu.writes)
	}
	_, next := e.app.Update(opened)
	if len(e.app.state.Agents) != 1 {
		t.Fatal("agent not recorded when the tab opened")
	}
	e.run(next)
	if got := e.cu.writes; len(got) != 1 || got[0] != "status t4 in progress" {
		t.Errorf("writes = %v", got)
	}
}

func TestStatusFailureDuringLaunchDoesNotShowAssign(t *testing.T) {
	e := newEnv(t)
	e.cu.tasks[2].Status = "to start" // t3: lead-only, with Malcolm
	e.cu.writeErr = errors.New("400 bad status")
	e.runner.Responses = []run.Response{
		{Match: "git -C /r/snowcap rev-parse", Err: errors.New("no branch")},
		{Match: "git -C /r/snowcap symbolic-ref", Out: "origin/main"},
	}
	e.load(t)
	selectTask(t, e, "t3")
	e.press("enter", "ctrl+s")
	tk, _ := e.app.taskByID("t3")
	if tk.AssignedTo(me.ID) || tk.Status != "to start" {
		t.Errorf("failed launch writes still shown: status=%q assigned=%v", tk.Status, tk.AssignedTo(me.ID))
	}
	if !strings.Contains(plain(e), "400 bad status") {
		t.Errorf("warning not shown:\n%s", plain(e))
	}
}

func TestNoDoubleLaunch(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t4")
	e.press("enter")
	e.app.Update(key("ctrl+s")) // launch running, result not back yet
	e.press("enter")
	if e.app.mode == modeStart {
		t.Error("panel opened again while the first launch is running")
	}
	if !strings.Contains(plain(e), "already starting") {
		t.Errorf("no message:\n%s", plain(e))
	}
}

func TestPanelReopensAfterLaunchError(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t2")
	e.press("enter", "tab", "s", "u", "b") // filter repos
	e.runner.Responses = append(e.runner.Responses, runResp("git -C /r/snowcap-subscriptions fetch", errors.New("offline")))
	e.press("ctrl+s")
	if e.app.mode != modeStart || e.app.start == nil || e.app.start.repoFilter != "sub" {
		t.Error("panel edits lost after a failed launch")
	}
}

func TestRexErrorDoesNotLookLikeClosedTabs(t *testing.T) {
	e := agentsEnv(t)
	e.rex.blocksErr = errors.New("rex: timeout")
	e.run(e.app.refreshAgents())
	if i := e.app.agentInfo["block:1"]; !i.alive {
		t.Error("a Rex error marked a live agent as closed")
	}
	selectTask(t, e, "t1")
	e.press("enter")
	if e.app.mode == modeStart {
		t.Error("Rex error opened the Start panel (could start a second agent)")
	}
	if !strings.Contains(plain(e), "timeout") {
		t.Errorf("error not shown:\n%s", plain(e))
	}
}

func TestEnterResultDoesNotTakeOverOtherMode(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	e.app.Update(enterMsg{taskID: "t2"}) // arrives while…
	e.app.mode = modeComment             // …the user started a comment
	e.app.Update(enterMsg{taskID: "t2"})
	if e.app.mode != modeComment {
		t.Errorf("mode = %v", e.app.mode)
	}
}

func mergedAgent(task, dir, block, repo, branch string) store.Agent {
	return store.Agent{TaskID: task, TaskKey: task, Workspace: workspace.Worktree.ID(), Repo: repo, Branch: branch, Dir: dir,
		Placement: rex.Placement{SessionID: "session:S", WindowID: "window:" + block, BlockID: block}}
}

func cleanupEnv(t *testing.T) *testEnv {
	e, _ := prEnv(t)
	e.app.cache.Repos = []workspace.Repo{{Name: "snowcap", Path: "/r/snowcap"}, {Name: "snowcap-app", Path: "/r/snowcap-app"}}
	return e
}

func TestCleanupSkipsFolderUsedByLiveAgent(t *testing.T) {
	e := cleanupEnv(t)
	e.app.state.AddAgent(mergedAgent("DEV-7808", "/w/snowcap/dev-7808", "block:old", "snowcap", "feature/dev-7808-preorder"))
	e.app.state.AddAgent(mergedAgent("DEV-7808", "/w/snowcap/dev-7808", "block:new", "snowcap", "feature/dev-7808-preorder"))
	e.rex.blocks = map[string]bool{"block:new": true}
	e.run(e.app.refreshAgents())
	e.press("4", "X")
	if e.app.mode == modeConfirm {
		t.Fatalf("offered to remove a folder a live agent uses:\n%s", plain(e))
	}
}

func TestCleanupMatchesRepoAndBranch(t *testing.T) {
	e := cleanupEnv(t)
	// Same branch name, but in a repo whose PR is not merged.
	e.app.state.AddAgent(mergedAgent("DEV-7808", "/w/snowcap-app/dev-7808", "block:7", "snowcap-app", "feature/dev-7808-preorder"))
	e.rex.blocks = map[string]bool{}
	e.run(e.app.refreshAgents())
	e.press("4", "X")
	if e.app.mode == modeConfirm {
		t.Error("offered to remove a worktree in another repo")
	}
}

func TestCleanupTreatsUnknownStateAsAlive(t *testing.T) {
	e := cleanupEnv(t)
	e.app.state.AddAgent(mergedAgent("DEV-7808", "/w/snowcap/dev-7808", "block:7", "snowcap", "feature/dev-7808-preorder"))
	// No refreshAgents yet: state unknown.
	e.press("4", "X")
	if e.app.mode == modeConfirm {
		t.Error("offered cleanup before Rex was checked")
	}
}

var _ = github.Merged

func TestFailedLaunchReopensOnlyItsOwnPanel(t *testing.T) {
	e := newEnv(t)
	e.runner.Responses = []run.Response{{Match: "git -C /r/snowcap fetch", Err: errors.New("offline")}}
	e.load(t)
	selectTask(t, e, "t2") // A: worktree in snowcap, will fail
	e.press("enter")
	_, launchA := e.app.Update(key("ctrl+s"))
	selectTask(t, e, "t4") // B: scratch
	e.press("enter")
	_, launchB := e.app.Update(key("ctrl+s"))
	_ = launchB
	e.app.Update(launchA()) // A fails while B is still starting
	if e.app.mode != modeStart || e.app.start == nil || e.app.start.task.ID != "t2" {
		t.Fatalf("wrong panel reopened: %+v", e.app.start)
	}
}

func TestLaunchGuardInPanel(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	selectTask(t, e, "t4")
	e.press("enter")
	e.app.Update(key("ctrl+s")) // still running
	e.app.launching["t4"] = true
	e.app.openStart(e.app.cache.Tasks[3])
	if cmd := e.app.launch(); cmd != nil {
		t.Error("second launch of the same task allowed from the panel")
	}
}

func TestFailedOlderStatusWriteKeepsNewerChange(t *testing.T) {
	e := newEnv(t)
	e.cu.statuses = listStatuses
	e.load(t)
	selectTask(t, e, "t2") // to start
	e.press("s")
	_, first := e.app.Update(key("3")) // → in progress, pending
	e.press("s")
	_, second := e.app.Update(key("4")) // → to review, pending
	e.cu.writeErr = errors.New("500")
	e.run(first) // older write fails
	if s := status(e, "t2"); s != "to review" {
		t.Errorf("failed older write hid the newer change: %q", s)
	}
	_ = second
}

func TestCursorFollowsTaskAfterStatusChange(t *testing.T) {
	e := newEnv(t)
	e.cu.statuses = listStatuses
	e.load(t)
	e.press("2") // first row, cursor never moved
	r, _ := e.app.tasks.list.selected()
	e.press("s", "4") // the task moves to another group
	if got, _ := e.app.tasks.list.selected(); got.key != r.key {
		t.Errorf("cursor left the task: %s → %s", r.key, got.key)
	}
}
