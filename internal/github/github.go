// Package github reads pull requests through the gh CLI and links them to
// ClickUp tasks.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/slavafyi/huddle/internal/clickup"
	"github.com/slavafyi/huddle/internal/run"
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
}

// Client is what huddle needs from GitHub.
type Client interface {
	PRs(ctx context.Context, now time.Time) ([]PR, error)
}

// GH implements Client with `gh api graphql`. It only reads.
type GH struct {
	Runner run.Runner
	Org    string // "" searches everywhere
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
}

// PRs returns my open PRs, PRs waiting for my review, and my PRs merged in
// the last 30 days.
func (g *GH) PRs(ctx context.Context, now time.Time) ([]PR, error) {
	scope := ""
	if g.Org != "" {
		scope = " org:" + g.Org
	}
	args := []string{"api", "graphql",
		"-f", "query=" + query,
		"-f", "mine=is:pr is:open author:@me archived:false" + scope,
		"-f", "review=is:pr is:open review-requested:@me archived:false" + scope,
		"-f", "merged=is:pr is:merged author:@me merged:>=" + now.Add(-mergedWindow).Format("2006-01-02") + scope,
	}
	out, err := g.Runner.Run(ctx, run.Cmd{Name: "gh", Args: args})
	if err != nil {
		return nil, err
	}
	var r struct {
		Data map[string]struct {
			Nodes []node `json:"nodes"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	if len(r.Errors) > 0 {
		return nil, errors.New("github: " + r.Errors[0].Message)
	}
	var prs []PR
	for _, k := range []struct {
		key  string
		kind Kind
	}{{"mine", Mine}, {"review", ReviewRequested}, {"merged", Merged}} {
		for _, n := range r.Data[k.key].Nodes {
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
			prs = append(prs, p)
		}
	}
	return prs, nil
}

// LinkTask returns the ID of the task a PR belongs to: the first known task
// ID (custom or ClickUp's own, also as CU-<id>) that appears as a whole word
// in the branch name, else in the title. "" means no link.
func LinkTask(p PR, tasks []clickup.Task) string {
	for _, text := range []string{p.Branch, p.Title} {
		words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
			return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-')
		})
		for _, w := range words {
			for _, t := range tasks {
				for _, key := range taskKeys(t) {
					if w == key || strings.HasPrefix(w, key+"-") {
						return t.ID
					}
				}
			}
		}
	}
	return ""
}

func taskKeys(t clickup.Task) []string {
	keys := []string{strings.ToLower(t.ID), "cu-" + strings.ToLower(t.ID)}
	if t.CustomID != "" {
		keys = append(keys, strings.ToLower(t.CustomID))
	}
	return keys
}
