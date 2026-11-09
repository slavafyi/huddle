package ui

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/slavafyi/huddle/internal/github"
	"github.com/slavafyi/huddle/internal/store"
	"github.com/slavafyi/huddle/internal/task"
	"github.com/slavafyi/huddle/internal/workspace"
)

// prsEvery is how often PRs refresh (SPEC §4.7).
const prsEvery = 90 * time.Second

type prsMsg struct {
	prs []github.PR
	at  time.Time
	err error
}

// prView is the PRs tab.
type prView struct {
	list list
}

func newPRView() *prView {
	return &prView{list: list{
		cols:  []column{{"#", 6}, {"TITLE", 0}, {"REPO", 22}, {"TASK", 10}, {"CI", 9}, {"REVIEW", 9}, {"AGE", 4}},
		empty: "no open PRs",
	}}
}

func prKey(p github.PR) string { return p.Repo + "#" + strconv.Itoa(p.Number) }

func ciLabel(s string) string {
	switch s {
	case "SUCCESS":
		return sGreen.Render("✓ pass")
	case "FAILURE", "ERROR":
		return sRed.Render("✗ fail")
	case "PENDING", "EXPECTED":
		return sYellow.Render("● running")
	}
	return sFaint.Render("—")
}

func reviewLabel(p github.PR) string {
	switch {
	case p.Kind == github.ReviewRequested:
		return sYellow.Render("you")
	case p.Draft:
		return sFaint.Render("draft")
	case p.Review == "APPROVED":
		return sGreen.Render("approved")
	case p.Review == "CHANGES_REQUESTED":
		return sRed.Render("changes")
	}
	return sFaint.Render("waiting")
}

func (v *prView) rebuild(prs []github.PR, tasks []task.Task, now time.Time) {
	var secs []section
	for _, g := range []struct {
		name string
		kind github.Kind
	}{{"Mine", github.Mine}, {"Review requested", github.ReviewRequested}} {
		s := section{title: g.name}
		for _, p := range prs {
			if p.Kind != g.kind {
				continue
			}
			linked := sFaint.Render("none")
			if t, ok := github.LinkTask(p, tasks); ok {
				linked = sCyan.Render(t.Key)
			}
			s.rows = append(s.rows, row{key: prKey(p), cells: []string{
				sFaint.Render("#" + strconv.Itoa(p.Number)), p.Title, p.RepoName, linked,
				ciLabel(p.CI), reviewLabel(p), sFaint.Render(ago(now.Sub(p.Updated))),
			}})
		}
		if len(s.rows) > 0 {
			secs = append(secs, s)
		}
	}
	v.list.setSections(secs)
}

func (a *App) refreshPRs() tea.Cmd {
	if a.deps.GitHub == nil || a.loadingPRs {
		return nil
	}
	a.loadingPRs = true
	gh, now := a.deps.GitHub, a.now()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		prs, err := gh.PRs(ctx, now)
		return prsMsg{prs: prs, at: now, err: err}
	}
}

func (a *App) onPRs(m prsMsg) tea.Cmd {
	a.loadingPRs = false
	if m.err != nil {
		a.prErr = m.err
		return nil
	}
	a.prErr = nil
	a.cache.PRs, a.cache.PRsAt = m.prs, m.at
	a.rebuild()
	return a.saveCache()
}

func (a *App) selectedPR() (github.PR, bool) {
	r, ok := a.prs.list.selected()
	if !ok {
		return github.PR{}, false
	}
	for _, p := range a.cache.PRs {
		if prKey(p) == r.key {
			return p, true
		}
	}
	return github.PR{}, false
}

func (a *App) prKey(k string) tea.Cmd {
	l := &a.prs.list
	switch k {
	case "j", "down":
		l.move(1)
	case "k", "up":
		l.move(-1)
	case "o":
		if p, ok := a.selectedPR(); ok {
			return a.openURL(p.URL)
		}
	case "enter":
		p, ok := a.selectedPR()
		if !ok || a.deps.Rex == nil {
			return nil
		}
		if a.deps.HomeSession == "" {
			a.flash = sYellow.Render("run huddle inside Rex to open diffs in a tab")
			return nil
		}
		rx, session := a.deps.Rex, a.deps.HomeSession
		home, _ := os.UserHomeDir()
		argv := []string{"gh", "pr", "diff", strconv.Itoa(p.Number), "-R", p.Repo}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			pl, err := rx.NewWindow(ctx, session, "PR #"+strconv.Itoa(p.Number), home, argv)
			if err != nil {
				return errMsg{err}
			}
			if err := rx.Focus(ctx, pl.SessionID, pl.WindowID); err != nil {
				return errMsg{err}
			}
			return nil
		}
	}
	return nil
}

// taskPR returns the open PR linked to a task.
func (a *App) taskPR(t task.Task) (github.PR, bool) {
	for _, p := range a.cache.PRs {
		if _, ok := github.LinkTask(p, []task.Task{t}); ok && p.Kind != github.Merged {
			return p, true
		}
	}
	return github.PR{}, false
}

func prDetail(p github.PR) []string {
	who := "by " + p.Author
	if p.Draft {
		who += " · draft"
	}
	return []string{
		sBold.Render("#" + strconv.Itoa(p.Number) + " " + p.Title),
		p.Repo + " · " + p.Branch,
		who + " · CI " + ciLabel(p.CI) + " · review " + reviewLabel(p),
		sFaint.Render(p.URL),
	}
}

// mergedWorktrees returns worktree agents whose branch has a merged PR.
func (a *App) mergedWorktrees() []store.Agent {
	merged := map[[2]string]bool{}
	for _, p := range a.cache.PRs {
		if p.Kind == github.Merged {
			merged[[2]string{p.RepoName, p.Branch}] = true
		}
	}
	var out []store.Agent
	for _, ag := range a.state.Agents {
		if ag.Workspace == workspace.Worktree.ID() && ag.Branch != "" && merged[[2]string{ag.Repo, ag.Branch}] {
			out = append(out, ag)
		}
	}
	return out
}

// dirInUse reports whether any agent in dir may still be running: its tab is
// open, or Rex has not told us yet.
func (a *App) dirInUse(dir string) bool {
	for _, ag := range a.state.Agents {
		if ag.Dir != dir {
			continue
		}
		if i := a.agentInfo[ag.BlockID]; !i.known || i.alive {
			return true
		}
	}
	return false
}

type cleanedMsg struct {
	removed []string // block IDs
	errs    []string
}

// cleanupMerged offers to remove worktrees whose PR is merged. Tabs that are
// still open are skipped; branches are never deleted.
func (a *App) cleanupMerged() tea.Cmd {
	var todo []store.Agent
	var keys, open []string
	for _, ag := range a.mergedWorktrees() {
		if a.dirInUse(ag.Dir) {
			open = append(open, ag.TaskKey)
			continue
		}
		todo = append(todo, ag)
		keys = append(keys, ag.TaskKey)
	}
	if len(todo) == 0 {
		if len(open) > 0 {
			a.flash = sYellow.Render("merged: " + strings.Join(open, ", ") + " · close its tab first")
		} else {
			a.flash = sFaint.Render("no merged worktrees to remove")
		}
		return nil
	}
	word := "worktree"
	if len(todo) > 1 {
		word = "worktrees"
	}
	text := "remove " + strconv.Itoa(len(todo)) + " merged " + word + " (" + strings.Join(keys, ", ") + ")? branches are kept"
	repos := map[string]string{}
	for _, r := range a.cache.Repos {
		repos[r.Name] = r.Path
	}
	runner := a.deps.Runner
	a.ask(text, func() tea.Cmd {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			var m cleanedMsg
			for _, ag := range todo {
				path, ok := repos[ag.Repo]
				if !ok {
					m.errs = append(m.errs, ag.TaskKey+": repo "+ag.Repo+" not found")
					continue
				}
				_, err := runner.Run(ctx, runCmd("git", "-C", path, "worktree", "remove", ag.Dir))
				if err != nil {
					m.errs = append(m.errs, ag.TaskKey+": "+err.Error())
					continue
				}
				m.removed = append(m.removed, ag.BlockID)
			}
			return m
		}
	})
	return nil
}

func (a *App) onCleaned(m cleanedMsg) tea.Cmd {
	for _, b := range m.removed {
		a.state.RemoveAgent(b)
		delete(a.agentInfo, b)
	}
	a.rebuild()
	if len(m.errs) > 0 {
		a.flash = sRed.Render(strings.Join(m.errs, " · "))
	} else {
		a.flash = sGreen.Render("removed " + plural(len(m.removed), "worktree"))
	}
	return a.saveState()
}
