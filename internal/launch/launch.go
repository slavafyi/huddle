// Package launch starts an agent on a task: it prepares the folder, opens a
// Rex tab, and updates ClickUp.
package launch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/slavafyi/cuddle/internal/clickup"
	"github.com/slavafyi/cuddle/internal/rex"
	"github.com/slavafyi/cuddle/internal/run"
	"github.com/slavafyi/cuddle/internal/store"
	"github.com/slavafyi/cuddle/internal/workspace"
)

// Deps are the services a launch uses.
type Deps struct {
	Runner  run.Runner
	Rex     rex.Client
	ClickUp clickup.Client
	Now     func() time.Time
}

// Plan is everything the Start panel decided.
type Plan struct {
	Task      clickup.Task
	Workspace workspace.Plan
	AgentName string
	Argv      []string // nil opens a shell
	SessionID string   // the client's stored session, "" if none
	TabName   string
	SetStatus string // "" leaves the status alone
	AssignMe  int64  // 0 leaves assignees alone
}

// Result is the started agent and problems after the tab opened.
type Result struct {
	Agent    store.Agent
	Warnings []string
}

// Updates reports which ClickUp writes succeeded.
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

// Open prepares the folder and opens the agent's tab (SPEC §4.4 steps 1–2).
// It stops at the first error, with no rollback. The caller saves the
// returned agent record (step 3) before calling ApplyClickUp (step 4).
func Open(ctx context.Context, d Deps, p Plan) (Result, error) {
	dir, err := workspace.Prepare(ctx, d.Runner, p.Workspace)
	if err != nil {
		return Result{}, err
	}

	sessions, err := d.Rex.Sessions(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list Rex sessions: %w", err)
	}
	session := clientSession(sessions, p.SessionID, p.Task.ListName)
	var pl rex.Placement
	if session != "" {
		pl, err = d.Rex.NewWindow(ctx, session, p.TabName, dir, p.Argv)
	} else {
		pl, err = d.Rex.NewSession(ctx, p.Task.ListName, dir, p.TabName, p.Argv)
	}
	if err != nil {
		return Result{}, fmt.Errorf("open Rex tab: %w", err)
	}

	res := Result{Agent: store.Agent{
		TaskID: p.Task.ID, TaskKey: p.Task.Key(), Title: p.Task.Name, Client: p.Task.ListName,
		Workspace: p.Workspace.Kind.ID(), Dir: dir, Agent: p.AgentName,
		Placement: pl, Started: d.Now(),
	}}
	if p.Workspace.Kind != workspace.Scratch {
		res.Agent.Repo = p.Workspace.Repo.Name
	}
	if p.Workspace.Kind == workspace.Worktree {
		res.Agent.Branch = p.Workspace.Branch
	}

	if err := d.Rex.Focus(ctx, pl.SessionID, pl.WindowID); err != nil {
		if errors.Is(err, rex.ErrRemoteControl) {
			res.Warnings = append(res.Warnings, err.Error())
		} else {
			res.Warnings = append(res.Warnings, "focus tab: "+err.Error())
		}
	}

	return res, nil
}

// ApplyClickUp moves the task and assigns me, as the plan says. It stops at
// the first failure.
func ApplyClickUp(ctx context.Context, cu clickup.Client, p Plan) Updates {
	var u Updates
	if p.SetStatus != "" {
		if err := cu.SetStatus(ctx, p.Task.ID, p.SetStatus); err != nil {
			u.Warnings = append(u.Warnings, "move to "+p.SetStatus+": "+err.Error())
			return u
		}
		u.StatusOK = true
	}
	if p.AssignMe != 0 {
		if err := cu.SetAssignee(ctx, p.Task.ID, p.AssignMe, true); err != nil {
			u.Warnings = append(u.Warnings, "assign: "+err.Error())
			return u
		}
		u.AssignOK = true
	}
	return u
}
