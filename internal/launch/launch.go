// Package launch starts an agent on a task: it prepares the folder, opens a
// Rex tab, and updates the task in its source.
package launch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/slavafyi/huddle/internal/resume"
	"github.com/slavafyi/huddle/internal/rex"
	"github.com/slavafyi/huddle/internal/run"
	"github.com/slavafyi/huddle/internal/store"
	"github.com/slavafyi/huddle/internal/task"
	"github.com/slavafyi/huddle/internal/workspace"
)

// Deps are the services a launch uses.
type Deps struct {
	Runner run.Runner
	Rex    rex.Client
	Source task.Source // the task's source
	Now    func() time.Time
}

// Plan is everything the Start panel decided.
type Plan struct {
	Task      task.Task
	Workspace workspace.Plan
	AgentName string
	Argv      []string // nil opens a shell
	Command   []string // Argv's template without the prompt (resume.Base)
	SessionID string   // the client's stored Rex session, "" if none
	TabName   string
	SetStatus string // "" leaves the status alone
	AssignMe  int64  // 0 leaves assignees alone
}

// Result is the started agent and problems after the tab opened.
type Result struct {
	Agent    store.Agent
	Warnings []string
}

// Updates reports which task writes succeeded.
type Updates struct {
	StatusOK bool
	AssignOK bool
	Warnings []string
}

// Argv returns the command for an agent, with {prompt} filled in. The shell
// agent has no command.
func Argv(agents map[string][]string, name, prompt string) []string {
	tmpl, ok := agents[name]
	if !ok {
		return nil
	}
	out := make([]string, len(tmpl))
	for i, a := range tmpl {
		out[i] = strings.ReplaceAll(a, "{prompt}", prompt)
	}
	return out
}

// clientSession picks the session for a client: the stored one if it still
// exists, else one named after the client, else "".
func clientSession(sessions map[string]string, stored, client string) string {
	if _, ok := sessions[stored]; ok && stored != "" {
		return stored
	}
	var found string
	for id, label := range sessions {
		if label == client && (found == "" || id < found) {
			found = id
		}
	}
	return found
}

// Open prepares the folder and opens the agent's tab.
// It stops at the first error, with no rollback. The caller saves the
// returned agent record (step 3) before calling Apply (step 4).
func Open(ctx context.Context, d Deps, p Plan) (Result, error) {
	dir, err := workspace.Prepare(ctx, d.Runner, p.Workspace)
	if err != nil {
		return Result{}, err
	}

	var res Result
	argv, agentSession := p.Argv, ""
	if ad := resume.For(p.Argv); ad != nil {
		a, id, err := ad.Start(ctx, d.Runner, dir, p.Argv)
		if err != nil {
			res.Warnings = append(res.Warnings, "no session ID, so the tab can't be reopened: "+err.Error())
		}
		argv, agentSession = a, id
	}

	pl, err := openTab(ctx, d.Rex, p.SessionID, p.Task.Project, dir, p.TabName, argv)
	if err != nil {
		return Result{}, err
	}

	res.Agent = store.Agent{
		Source: p.Task.Source, TaskID: p.Task.ID, TaskKey: p.Task.Key, Title: p.Task.Name, Client: p.Task.Project,
		Workspace: p.Workspace.Kind.ID(), Dir: dir, Agent: p.AgentName, Command: p.Command, AgentSession: agentSession,
		Placement: pl, Started: d.Now(),
	}
	if p.Workspace.Kind != workspace.Scratch {
		res.Agent.Repo = p.Workspace.Repo.Name
	}
	if p.Workspace.Kind == workspace.Worktree {
		res.Agent.Branch = p.Workspace.Branch
	}
	res.Warnings = append(res.Warnings, focus(ctx, d.Rex, pl)...)
	return res, nil
}

// Reopen opens a closed agent tab again in its folder, resuming the agent's
// session. The returned agent replaces ag (store.State.ReplaceAgent).
func Reopen(ctx context.Context, d Deps, ag store.Agent, tabName string) (Result, error) {
	ad := resume.For(ag.Command)
	switch {
	case len(ag.Command) == 0:
		return Result{}, errors.New("this tab has no command to reopen")
	case ad == nil:
		return Result{}, fmt.Errorf("can't reopen %s sessions", filepath.Base(ag.Command[0]))
	}
	if fi, err := os.Stat(ag.Dir); err != nil || !fi.IsDir() {
		return Result{}, fmt.Errorf("folder is gone: %s", ag.Dir)
	}
	id := ag.AgentSession
	if id == "" {
		var err error
		if id, err = ad.Find(ag.Dir, ag.Started); err != nil {
			return Result{}, fmt.Errorf("find the %s session: %w", ag.Agent, err)
		}
		if id == "" {
			return Result{}, fmt.Errorf("no session of %s found in %s", filepath.Base(ag.Command[0]), ag.Dir)
		}
	}

	pl, err := openTab(ctx, d.Rex, ag.SessionID, ag.Client, ag.Dir, tabName, ad.Command(ag.Command, id))
	if err != nil {
		return Result{}, err
	}
	ag.Placement, ag.AgentSession = pl, id
	return Result{Agent: ag, Warnings: focus(ctx, d.Rex, pl)}, nil
}

// openTab opens a tab in the client's Rex session, creating the session
// when it is missing.
func openTab(ctx context.Context, rx rex.Client, stored, client, dir, tab string, argv []string) (rex.Placement, error) {
	sessions, err := rx.Sessions(ctx)
	if err != nil {
		return rex.Placement{}, fmt.Errorf("list Rex sessions: %w", err)
	}
	var pl rex.Placement
	if session := clientSession(sessions, stored, client); session != "" {
		pl, err = rx.NewWindow(ctx, session, tab, dir, argv)
	} else {
		pl, err = rx.NewSession(ctx, client, dir, tab, argv)
	}
	if err != nil {
		return rex.Placement{}, fmt.Errorf("open Rex tab: %w", err)
	}
	return pl, nil
}

// focus shows the new tab; failing to is only a warning.
func focus(ctx context.Context, rx rex.Client, pl rex.Placement) []string {
	err := rx.Focus(ctx, pl.SessionID, pl.WindowID)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, rex.ErrRemoteControl):
		return []string{err.Error()}
	}
	return []string{"focus tab: " + err.Error()}
}

// Apply moves the task and assigns me in its source, as the plan says. It
// stops at the first failure.
func Apply(ctx context.Context, src task.Source, p Plan) Updates {
	var u Updates
	if p.SetStatus != "" {
		if err := src.SetStatus(ctx, p.Task, p.SetStatus); err != nil {
			u.Warnings = append(u.Warnings, "move to "+p.SetStatus+": "+err.Error())
			return u
		}
		u.StatusOK = true
	}
	if p.AssignMe != 0 {
		if err := src.SetAssignee(ctx, p.Task, p.AssignMe, true); err != nil {
			u.Warnings = append(u.Warnings, "assign: "+err.Error())
			return u
		}
		u.AssignOK = true
	}
	return u
}
