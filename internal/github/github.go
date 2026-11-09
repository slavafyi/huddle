// Package github reads pull requests and issues through the gh CLI, links
// PRs to tasks, and is the GitHub issues task source.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/slavafyi/huddle/internal/run"
	"github.com/slavafyi/huddle/internal/task"
)

// Kind says why a PR is listed.
type Kind int

// PR kinds.
const (
	Mine Kind = iota
	ReviewRequested
	Merged
)

// mergedWindow is how far back merged PRs are fetched (for worktree cleanup).
const mergedWindow = 30 * 24 * time.Hour

// PR is a pull request.
type PR struct {
	Number   int       `json:"number"`
	Title    string    `json:"title"`
	URL      string    `json:"url"`
	Repo     string    `json:"repo"`
	RepoName string    `json:"repo_name"`
	Branch   string    `json:"branch"`
	Author   string    `json:"author"`
	Draft    bool      `json:"draft"`
	Updated  time.Time `json:"updated"`
	CI       string    `json:"ci,omitempty"`     // SUCCESS, FAILURE, ERROR, PENDING, EXPECTED or ""
	Review   string    `json:"review,omitempty"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED or ""
	Kind     Kind      `json:"kind"`
	Closes   []string  `json:"closes,omitempty"` // issues it closes, lowercase "owner/repo#42"
}

// Client is what huddle needs from GitHub.
type Client interface {
	PRs(ctx context.Context, now time.Time) ([]PR, error)
}

// GH runs gh for one profile. Its PRs only read.
type GH struct {
	Runner run.Runner
	Orgs   []string // search only these orgs; none: everywhere but Exclude
	// Exclude are orgs other profiles claim, left out when Orgs is empty.
	Exclude []string
	// Token is the profile account's token, given to every gh command as
	// GH_TOKEN. Empty: gh's active account.
	Token string
}

// cmd is a gh command with the profile's token.
func (g *GH) cmd(args ...string) run.Cmd {
	c := run.Cmd{Name: "gh", Args: args}
	if g.Token != "" {
		c.Env = []string{"GH_TOKEN=" + g.Token}
	}
	return c
}

// scope is the search filter for the profile's orgs, with a leading space.
func (g *GH) scope() string {
	s := ""
	for _, o := range g.Orgs {
		s += " org:" + o
	}
	if len(g.Orgs) == 0 {
		for _, o := range g.Exclude {
			s += " -org:" + o
		}
	}
	return s
}

// graphql runs a query and decodes its data into v.
func (g *GH) graphql(ctx context.Context, v any, args ...string) error {
	out, err := g.Runner.Run(ctx, g.cmd(append([]string{"api", "graphql"}, args...)...))
	if err != nil {
		return err
	}
	var r struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return err
	}
	if len(r.Errors) > 0 {
		return errors.New("github: " + r.Errors[0].Message)
	}
	if len(r.Data) == 0 {
		return errors.New("github: empty reply")
	}
	return json.Unmarshal(r.Data, v)
}

// Token returns the gh token of account, for GH.Token. It is never logged.
func Token(ctx context.Context, r run.Runner, account string) (string, error) {
	out, err := r.Run(ctx, run.Cmd{Name: "gh", Args: []string{"auth", "token", "--user", account}})
	tok := strings.TrimSpace(string(out))
	if err == nil && tok == "" {
		err = errors.New("empty token")
	}
	if err != nil {
		return "", fmt.Errorf("no gh token for github_account %q (log in with gh auth login): %w", account, err)
	}
	return tok, nil
}

const query = `query($mine: String!, $review: String!, $merged: String!) {
  mine: search(query: $mine, type: ISSUE, first: 50) { nodes { ...pr } }
  review: search(query: $review, type: ISSUE, first: 50) { nodes { ...pr } }
  merged: search(query: $merged, type: ISSUE, first: 50) { nodes { ...pr } }
}
fragment pr on PullRequest {
  number title url isDraft updatedAt headRefName
  author { login }
  repository { nameWithOwner name }
  reviewDecision
  commits(last: 1) { nodes { commit { statusCheckRollup { state } } } }
  closingIssuesReferences(first: 10) { nodes { number repository { nameWithOwner } } }
}`

type node struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	IsDraft   bool   `json:"isDraft"`
	UpdatedAt string `json:"updatedAt"`
	Head      string `json:"headRefName"`
	Author    struct {
		Login string `json:"login"`
	} `json:"author"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
		Name          string `json:"name"`
	} `json:"repository"`
	ReviewDecision *string `json:"reviewDecision"`
	Commits        struct {
		Nodes []struct {
			Commit struct {
				Rollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
	Closing struct {
		Nodes []struct {
			Number     int `json:"number"`
			Repository struct {
				NameWithOwner string `json:"nameWithOwner"`
			} `json:"repository"`
		} `json:"nodes"`
	} `json:"closingIssuesReferences"`
}

// PRs returns my open PRs, PRs waiting for my review, and my PRs merged in
// the last 30 days.
func (g *GH) PRs(ctx context.Context, now time.Time) ([]PR, error) {
	scope := g.scope()
	var data map[string]struct {
		Nodes []node `json:"nodes"`
	}
	err := g.graphql(ctx, &data,
		"-f", "query="+query,
		"-f", "mine=is:pr is:open author:@me archived:false"+scope,
		"-f", "review=is:pr is:open review-requested:@me archived:false"+scope,
		"-f", "merged=is:pr is:merged author:@me merged:>="+now.Add(-mergedWindow).Format("2006-01-02")+scope,
	)
	if err != nil {
		return nil, err
	}
	var prs []PR
	for _, k := range []struct {
		key  string
		kind Kind
	}{{"mine", Mine}, {"review", ReviewRequested}, {"merged", Merged}} {
		for _, n := range data[k.key].Nodes {
			if n.Number == 0 {
				continue // not a pull request
			}
			p := PR{
				Number: n.Number, Title: n.Title, URL: n.URL, Draft: n.IsDraft, Branch: n.Head,
				Author: n.Author.Login, Repo: n.Repository.NameWithOwner, RepoName: n.Repository.Name, Kind: k.kind,
			}
			p.Updated, _ = time.Parse(time.RFC3339, n.UpdatedAt)
			if n.ReviewDecision != nil {
				p.Review = *n.ReviewDecision
			}
			if c := n.Commits.Nodes; len(c) > 0 && c[0].Commit.Rollup != nil {
				p.CI = c[0].Commit.Rollup.State
			}
			for _, c := range n.Closing.Nodes {
				p.Closes = append(p.Closes, strings.ToLower(c.Repository.NameWithOwner+"#"+strconv.Itoa(c.Number)))
			}
			prs = append(prs, p)
		}
	}
	return prs, nil
}

// LinkTask returns the task a PR belongs to: a GitHub issue the PR closes;
// else the first task with a link word (task.Task.Links) that appears as a
// whole word in the branch name, else in the title; else a GitHub issue of
// the PR's repo whose number is a branch word ("42" or "42-…").
func LinkTask(p PR, tasks []task.Task) (task.Task, bool) {
	for _, c := range p.Closes {
		for _, t := range tasks {
			if t.Source == IssuesSource && strings.EqualFold(t.ID, c) {
				return t, true
			}
		}
	}
	split := func(text string) []string {
		return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
			return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-')
		})
	}
	match := func(w, key string) bool { return w == key || strings.HasPrefix(w, key+"-") }
	for _, text := range []string{p.Branch, p.Title} {
		for _, w := range split(text) {
			for _, t := range tasks {
				for _, key := range t.Links {
					if match(w, key) {
						return t, true
					}
				}
			}
		}
	}
	for _, w := range split(p.Branch) {
		for _, t := range tasks {
			if t.Source != IssuesSource || !strings.EqualFold(t.ListID, p.Repo) {
				continue
			}
			if _, num, ok := strings.Cut(t.ID, "#"); ok && match(w, num) {
				return t, true
			}
		}
	}
	return task.Task{}, false
}
