package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slavafyi/huddle/internal/config"
	"github.com/slavafyi/huddle/internal/github"
	"github.com/slavafyi/huddle/internal/rex"
	"github.com/slavafyi/huddle/internal/run"
	"github.com/slavafyi/huddle/internal/store"
	"github.com/slavafyi/huddle/internal/workspace"
)

type fakeGH struct {
	prs []github.PR
	err error
}

func (f *fakeGH) PRs(context.Context, time.Time) ([]github.PR, error) { return f.prs, f.err }

func samplePRs() []github.PR {
	return []github.PR{
		{Number: 14, Title: "DEV-7700: promo checkout widget", URL: "https://github.com/o/snowcap/pull/14", Repo: "o/snowcap", RepoName: "snowcap",
			Branch: "feature/dev-7700-promo-checkout", Draft: true, CI: "PENDING", Kind: github.Mine, Updated: testNow.Add(-30 * time.Minute)},
		{Number: 3, Title: "chore: update CLI", URL: "https://github.com/o/fn/pull/3", Repo: "o/fn", RepoName: "fn",
			Branch: "chore/cli", CI: "FAILURE", Kind: github.Mine, Updated: testNow.Add(-96 * time.Hour)},
		{Number: 22, Title: "feat: auto-label", URL: "https://github.com/o/printer/pull/22", Repo: "o/printer", RepoName: "printer",
			Branch: "fix/print", CI: "SUCCESS", Review: "REVIEW_REQUIRED", Kind: github.ReviewRequested, Updated: testNow.Add(-5 * time.Hour)},
		{Number: 9, Title: "old", Repo: "o/snowcap", RepoName: "snowcap", Branch: "feature/dev-7808-preorder", Kind: github.Merged},
	}
}

func prEnv(t *testing.T) (*testEnv, *fakeGH) {
	e := newEnv(t)
	gh := &fakeGH{prs: samplePRs()}
	e.app.deps.GitHub = gh
	e.app.deps.HomeSession = "session:home"
	e.load(t)
	e.run(e.app.refreshPRs())
	return e, gh
}

func TestPRsViewShowsSectionsAndLinks(t *testing.T) {
	e, _ := prEnv(t)
	e.press("3")
	out := plain(e)
	for _, want := range []string{"Mine 2", "Review requested 1", "#14", "DEV-7700", "● running", "draft", "#3", "✗ fail", "none", "#22", "✓ pass", "you", "30m", "4d"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "#9 ") {
		t.Error("merged PRs should not be listed")
	}
}

func TestPRKeys(t *testing.T) {
	e, _ := prEnv(t)
	e.press("3", "o")
	if calls := e.runner.Calls(); len(calls) != 1 || calls[0] != "open https://github.com/o/snowcap/pull/14" {
		t.Errorf("open: %v", calls)
	}
	e.app.deps.Profile = "work"
	e.app.deps.Profiles = []config.Profile{{Name: "home", GitHubAccount: "other"}, {Name: "work", GitHubAccount: "me"}}
	e.press("enter")
	want := "new-window session:home | PR #14 | "
	if len(e.rex.calls) < 2 || !strings.HasPrefix(e.rex.calls[0], want) || !strings.HasSuffix(e.rex.calls[0], " sh 14 o/snowcap me") {
		t.Errorf("diff tab: %v", e.rex.calls)
	}
}

func TestOpenTaskPR(t *testing.T) {
	e, _ := prEnv(t)
	selectTask(t, e, "t2") // DEV-7700, linked to #14
	e.press("p")
	if calls := e.runner.Calls(); len(calls) != 1 || calls[0] != "open https://github.com/o/snowcap/pull/14" {
		t.Errorf("calls = %v", calls)
	}
	selectTask(t, e, "t4") // no PR
	e.press("p")
	if !strings.Contains(plain(e), "no PR") {
		t.Errorf("no message:\n%s", plain(e))
	}
}

func TestPRErrorKeepsCache(t *testing.T) {
	e, gh := prEnv(t)
	gh.err = errors.New("gh: not logged in")
	e.run(e.app.refreshPRs())
	e.press("3")
	out := plain(e)
	if !strings.Contains(out, "#14") || !strings.Contains(out, "not logged in") {
		t.Errorf("cached PRs or error missing:\n%s", out)
	}
	if c := e.cacheOnDisk(t); len(c.PRs) != 4 {
		t.Errorf("cache PRs = %d", len(c.PRs))
	}
}

func TestCleanupMergedWorktreesAsksFirst(t *testing.T) {
	e, _ := prEnv(t)
	e.app.cache.Repos = []workspace.Repo{{Name: "snowcap", Path: "/r/snowcap"}}
	merged := store.Agent{Source: "clickup", TaskID: "t1", TaskKey: "DEV-7808", Client: "Harbor and Goods", Workspace: workspace.Worktree.ID(),
		Repo: "snowcap", Branch: "feature/dev-7808-preorder", Dir: "/w/snowcap/dev-7808",
		Placement: rex.Placement{SessionID: "session:S", WindowID: "window:7", BlockID: "block:7"}}
	open := merged
	open.TaskKey, open.Branch, open.Dir, open.BlockID = "DEV-7700", "feature/dev-7700-promo-checkout", "/w/snowcap/dev-7700", "block:8"
	e.app.state.AddAgent(merged)
	e.app.state.AddAgent(open)
	e.rex.blocks = map[string]bool{}
	e.run(e.app.refreshAgents())
	e.press("4", "X")
	if !strings.Contains(plain(e), "remove 1 merged worktree (DEV-7808)?") {
		t.Fatalf("no question:\n%s", plain(e))
	}
	e.press("y")
	calls := e.runner.Calls()
	if len(calls) != 1 || calls[0] != "git -C /r/snowcap worktree remove /w/snowcap/dev-7808" {
		t.Errorf("git calls = %v", calls)
	}
	if len(e.app.state.Agents) != 1 || e.app.state.Agents[0].TaskKey != "DEV-7700" {
		t.Errorf("agents = %+v", e.app.state.Agents)
	}
}

func TestCleanupSkipsOpenTabsAndReportsGitErrors(t *testing.T) {
	e, _ := prEnv(t)
	e.app.cache.Repos = []workspace.Repo{{Name: "snowcap", Path: "/r/snowcap"}}
	e.runner.Responses = []run.Response{{Match: "git -C /r/snowcap worktree remove", Err: errors.New("contains modified files")}}
	ag := store.Agent{TaskKey: "DEV-7808", Workspace: workspace.Worktree.ID(), Repo: "snowcap", Branch: "feature/dev-7808-preorder",
		Dir: "/w/snowcap/dev-7808", Placement: rex.Placement{SessionID: "session:S", BlockID: "block:7"}}
	e.app.state.AddAgent(ag)
	e.rex.blocks = map[string]bool{"block:7": true}
	e.run(e.app.refreshAgents())
	e.press("4", "X")
	if !strings.Contains(plain(e), "close its tab first") {
		t.Errorf("open tab not skipped:\n%s", plain(e))
	}
	e.rex.blocks = map[string]bool{}
	e.run(e.app.refreshAgents())
	e.press("X", "y")
	if !strings.Contains(plain(e), "contains modified files") || len(e.app.state.Agents) != 1 {
		t.Errorf("git error not reported or agent dropped:\n%s", plain(e))
	}
}

// runDiffScript runs diffScript with a fake gh that logs each call as
// "$GH_TOKEN|args", prints tok-NAME for gh auth token --user NAME, and
// fails gh pr diff like GitHub does past 20000 lines.
func runDiffScript(t *testing.T, account string) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	fake := "#!/bin/sh\necho \"$GH_TOKEN|$*\" | tr '\\n' ' ' >> " + log + "; echo >> " + log + "\n" +
		"[ \"$1 $2\" = 'auth token' ] && { echo \"tok-$4\"; exit; }\n" +
		"[ \"$1\" = pr ] && { echo 'HTTP 406: diff too large' >&2; exit 1; }\n" +
		"echo PATCHES\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", diffScript, "sh", "27", "o/journal", account)
	cmd.Env = append(os.Environ(), "PATH="+dir+":/usr/bin:/bin", "PAGER=cat", "GH_TOKEN=")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		lines = append(lines, strings.TrimSpace(l))
	}
	return string(out), lines
}

// A diff too large for gh pr diff (GitHub's 20000-line limit) falls back
// to the per-file patches from the PR files API.
func TestDiffScriptFallsBackToFilesAPI(t *testing.T) {
	out, lines := runDiffScript(t, "")
	if !strings.Contains(out, "PATCHES") {
		t.Errorf("no fallback output: %q", out)
	}
	if len(lines) != 2 || lines[0] != "|pr diff 27 -R o/journal" || !strings.HasPrefix(lines[1], "|api --paginate repos/o/journal/pulls/27/files --jq ") {
		t.Errorf("gh calls: %q", lines)
	}
}

// With a profile github_account, the tab's gh calls use that account's
// token, read in the tab (huddle never passes the token itself).
func TestDiffScriptUsesProfileAccount(t *testing.T) {
	_, lines := runDiffScript(t, "me")
	if len(lines) != 3 || lines[0] != "|auth token --user me" || lines[1] != "tok-me|pr diff 27 -R o/journal" || !strings.HasPrefix(lines[2], "tok-me|api ") {
		t.Errorf("gh calls: %q", lines)
	}
}
