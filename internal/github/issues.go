package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/slavafyi/huddle/internal/task"
)

// IssuesSource is the GitHub issues task.Source name.
const IssuesSource = "github"

// Issues is GitHub issues as a task source: issues assigned to me (Mine)
// or opened by me (Lead), in the GH's scope. Statuses are open and closed.
type Issues struct {
	GH *GH
}

var _ task.Source = (*Issues)(nil)

// Name implements task.Source.
func (s *Issues) Name() string { return IssuesSource }

// Rules are GitHub's Today groups, filter labels and Start panel defaults
// (SPEC §4.2–§4.4).
func (s *Issues) Rules() task.Rules {
	return task.Rules{
		Groups:         []string{task.GroupInProgress, task.GroupInReview, task.GroupOpen, task.GroupDone},
		Classify:       classify,
		FilterNames:    [3]string{"All", "Assigned", "Opened by me"},
		RepoInList:     true,
		BranchTemplate: "feature/{number}-{slug}",
		PromptTemplate: "Take issue {full_id}",
	}
}

// classify puts an open issue in a Today group: In progress with a live
// agent tab, In review with an open PR, Open when assigned to me or opened
// by me with nobody assigned.
func classify(t task.Task, s task.Signals) string {
	switch {
	case s.Live[t.Ref()]:
		return task.GroupInProgress
	case s.InReview[t.Ref()]:
		return task.GroupInReview
	case t.Mine || t.Lead && len(t.Others) == 0:
		return task.GroupOpen
	}
	return ""
}

// Me returns my GitHub user ID (the account's databaseId).
func (s *Issues) Me(ctx context.Context) (int64, error) {
	out, err := s.GH.Runner.Run(ctx, s.GH.cmd("api", "user"))
	if err != nil {
		return 0, err
	}
	var u struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(out, &u); err != nil {
		return 0, fmt.Errorf("github user: %w", err)
	}
	return u.ID, nil
}

const issueFragment = `fragment issue on Issue {
  number title url state closedAt updatedAt
  repository { nameWithOwner name }
  author { login ... on User { databaseId } }
  assignees(first: 10) { nodes { login databaseId } }
  labels(first: 20) { nodes { name } }
}`

type issueNode struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      string `json:"state"`
	ClosedAt   string `json:"closedAt"`
	UpdatedAt  string `json:"updatedAt"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
		Name          string `json:"name"`
	} `json:"repository"`
	Author struct {
		DatabaseID int64 `json:"databaseId"`
	} `json:"author"`
	Assignees struct {
		Nodes []struct {
			Login      string `json:"login"`
			DatabaseID int64  `json:"databaseId"`
		} `json:"nodes"`
	} `json:"assignees"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
}

// Fetch implements task.Source. A full fetch gets open issues plus issues
// closed in the last task.DoneWindow; otherwise issues updated since.
func (s *Issues) Fetch(ctx context.Context, me int64, since, now time.Time) ([]task.Task, error) {
	tail := " archived:false" + s.GH.scope()
	var searches [][2]string
	if since.IsZero() {
		closed := " closed:>=" + now.Add(-task.DoneWindow).Format("2006-01-02")
		searches = [][2]string{
			{"openAssigned", "is:issue is:open assignee:@me" + tail},
			{"openAuthored", "is:issue is:open author:@me" + tail},
			{"closedAssigned", "is:issue is:closed assignee:@me" + closed + tail},
			{"closedAuthored", "is:issue is:closed author:@me" + closed + tail},
		}
	} else {
		upd := " updated:>=" + since.UTC().Format("2006-01-02T15:04:05-07:00")
		searches = [][2]string{
			{"assigned", "is:issue assignee:@me" + upd + tail},
			{"authored", "is:issue author:@me" + upd + tail},
		}
	}
	var vars, body []string
	args := []string{}
	for _, q := range searches {
		vars = append(vars, "$"+q[0]+": String!")
		body = append(body, "  "+q[0]+": search(query: $"+q[0]+", type: ISSUE, first: 100) { nodes { ...issue } }")
		args = append(args, "-f", q[0]+"="+q[1])
	}
	query := "query(" + strings.Join(vars, ", ") + ") {\n" + strings.Join(body, "\n") + "\n}\n" + issueFragment
	var data map[string]struct {
		Nodes []issueNode `json:"nodes"`
	}
	if err := s.GH.graphql(ctx, &data, append([]string{"-f", "query=" + query}, args...)...); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []task.Task
	for _, q := range searches {
		for _, n := range data[q[0]].Nodes {
			if n.Number == 0 {
				continue // not an issue
			}
			t := toTask(n, me)
			if !seen[t.ID] {
				seen[t.ID] = true
				out = append(out, t)
			}
		}
	}
	return out, nil
}

func toTask(n issueNode, me int64) task.Task {
	num := strconv.Itoa(n.Number)
	t := task.Task{
		Source: IssuesSource, ID: n.Repository.NameWithOwner + "#" + num, Key: n.Repository.Name + "#" + num,
		Name: n.Title, URL: n.URL, Status: "open", ListID: n.Repository.NameWithOwner, Project: n.Repository.Name,
		Lead: me != 0 && n.Author.DatabaseID == me,
	}
	t.Updated, _ = time.Parse(time.RFC3339, n.UpdatedAt)
	if n.State == "CLOSED" {
		t.Status, t.StatusOrder = "closed", 1
		t.Done, _ = time.Parse(time.RFC3339, n.ClosedAt)
	}
	for _, a := range n.Assignees.Nodes {
		if me != 0 && a.DatabaseID == me {
			t.Mine = true
		} else {
			t.Others = append(t.Others, a.Login)
		}
	}
	for _, l := range n.Labels.Nodes {
		switch strings.ToLower(l.Name) {
		case "priority:urgent":
			t.Priority = "urgent"
		case "priority:high":
			if t.Priority == "" {
				t.Priority = "high"
			}
		}
	}
	return t
}

// Statuses are open and closed.
func (s *Issues) Statuses(context.Context, task.Task) ([]task.Status, error) {
	return []task.Status{{Name: "open"}, {Name: "closed", Order: 1, Done: true}}, nil
}

// issueArgs are gh's arguments for one issue: number and repo.
func issueArgs(t task.Task) []string {
	_, num, _ := strings.Cut(t.ID, "#")
	return []string{num, "-R", t.ListID}
}

func (s *Issues) issue(ctx context.Context, verb string, t task.Task, extra ...string) error {
	args := append(append([]string{"issue", verb}, issueArgs(t)...), extra...)
	_, err := s.GH.Runner.Run(ctx, s.GH.cmd(args...))
	return err
}

// SetStatus closes or reopens the issue.
func (s *Issues) SetStatus(ctx context.Context, t task.Task, status string) error {
	switch status {
	case "closed":
		return s.issue(ctx, "close", t)
	case "open":
		return s.issue(ctx, "reopen", t)
	}
	return fmt.Errorf("github issues have no status %q", status)
}

// AddComment comments on the issue.
func (s *Issues) AddComment(ctx context.Context, t task.Task, text string) error {
	return s.issue(ctx, "comment", t, "--body", text)
}

// SetAssignee assigns or unassigns the profile's account.
func (s *Issues) SetAssignee(ctx context.Context, t task.Task, _ int64, add bool) error {
	if add {
		return s.issue(ctx, "edit", t, "--add-assignee", "@me")
	}
	return s.issue(ctx, "edit", t, "--remove-assignee", "@me")
}
