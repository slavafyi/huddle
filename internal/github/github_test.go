package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/slavafyi/huddle/internal/clickup"
	"github.com/slavafyi/huddle/internal/run"
	"github.com/slavafyi/huddle/internal/task"
)

const reply = `{"data": {
  "mine": {"nodes": [
    {"number": 14, "title": "DEV-7100: referral email", "url": "https://github.com/vertexdigital/store/pull/14",
     "isDraft": true, "updatedAt": "2026-11-09T09:30:00Z", "headRefName": "feature/dev-7100-launch-referral",
     "author": {"login": "slavafyi"}, "repository": {"nameWithOwner": "vertexdigital/store", "name": "store"},
     "reviewDecision": null, "commits": {"nodes": [{"commit": {"statusCheckRollup": {"state": "PENDING"}}}]},
     "closingIssuesReferences": {"nodes": [{"number": 7, "repository": {"nameWithOwner": "VertexDigital/store"}}]}},
    {}
  ]},
  "review": {"nodes": [
    {"number": 22, "title": "feat: auto-label", "url": "https://github.com/vertexdigital/printer/pull/22",
     "isDraft": false, "updatedAt": "2026-11-09T05:00:00Z", "headRefName": "fix/print",
     "author": {"login": "malcolm"}, "repository": {"nameWithOwner": "vertexdigital/printer", "name": "printer"},
     "reviewDecision": "REVIEW_REQUIRED", "commits": {"nodes": [{"commit": {"statusCheckRollup": null}}]}}
  ]},
  "merged": {"nodes": [
    {"number": 9, "title": "new theme", "url": "u", "isDraft": false, "updatedAt": "2026-11-07T10:00:00Z",
     "headRefName": "feature/dev-7301-new-theme", "author": {"login": "slavafyi"},
     "repository": {"nameWithOwner": "vertexdigital/theme", "name": "theme"}, "reviewDecision": "APPROVED",
     "commits": {"nodes": [{"commit": {"statusCheckRollup": {"state": "SUCCESS"}}}]}}
  ]}
}}`

func TestPRsParsesAndQueriesOrg(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "gh api graphql", Out: reply}}}
	g := &GH{Runner: f, Orgs: []string{"vertexdigital", "acme"}}
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	prs, err := g.PRs(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 3 {
		t.Fatalf("prs = %d (empty nodes must be skipped)", len(prs))
	}
	p := prs[0]
	if p.Number != 14 || p.Repo != "vertexdigital/store" || p.RepoName != "store" || p.Branch != "feature/dev-7100-launch-referral" ||
		!p.Draft || p.CI != "PENDING" || p.Kind != Mine || p.Author != "slavafyi" || p.Updated.IsZero() {
		t.Errorf("pr = %+v", p)
	}
	if len(p.Closes) != 1 || p.Closes[0] != "vertexdigital/store#7" {
		t.Errorf("closes = %v", p.Closes)
	}
	if prs[1].Kind != ReviewRequested || prs[1].CI != "" || prs[1].Review != "REVIEW_REQUIRED" {
		t.Errorf("review pr = %+v", prs[1])
	}
	if prs[2].Kind != Merged {
		t.Errorf("merged pr = %+v", prs[2])
	}
	call := strings.Join(f.Calls(), "\n")
	for _, want := range []string{
		"mine=is:pr is:open author:@me archived:false org:vertexdigital org:acme",
		"review=is:pr is:open review-requested:@me archived:false org:vertexdigital org:acme",
		"merged=is:pr is:merged author:@me merged:>=2026-10-10 org:vertexdigital org:acme",
	} {
		if !strings.Contains(call, want) {
			t.Errorf("query missing %q in %s", want, call)
		}
	}
}

func TestPRsWithoutOrg(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "gh api graphql", Out: `{"data":{"mine":{"nodes":[]},"review":{"nodes":[]},"merged":{"nodes":[]}}}`}}}
	g := &GH{Runner: f}
	if _, err := g.PRs(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.Calls()[0], "org:") {
		t.Errorf("no org filter expected: %s", f.Calls()[0])
	}
	if env := f.CallsWithDir()[0].Env; env != nil {
		t.Errorf("no account: gh must use its own login, env = %v", env)
	}
}

func TestPRsUseTokenAndLeaveOutOtherOrgs(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "gh api graphql", Out: `{"data":{}}`}}}
	g := &GH{Runner: f, Token: "tok", Exclude: []string{"vertexdigital", "acme"}}
	if _, err := g.PRs(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	c := f.CallsWithDir()[0]
	if len(c.Env) != 1 || c.Env[0] != "GH_TOKEN=tok" {
		t.Errorf("env = %v", c.Env)
	}
	if !strings.Contains(c.String(), "author:@me archived:false -org:vertexdigital -org:acme") {
		t.Errorf("query: %s", c.String())
	}
}

func TestToken(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "gh auth token --user slavafyi", Out: "gho_abc\n"}}}
	tok, err := Token(context.Background(), f, "slavafyi")
	if err != nil || tok != "gho_abc" {
		t.Fatalf("token = %q, %v", tok, err)
	}
	f = &run.Fake{Responses: []run.Response{{Match: "gh auth token", Err: errors.New("no oauth token")}}}
	_, err = Token(context.Background(), f, "ronind")
	if err == nil || !strings.Contains(err.Error(), "ronind") || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(fmt.Sprint(err), "gho_") {
		t.Fatal("token in error")
	}
}

func TestPRsGraphQLError(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "gh api graphql", Out: `{"errors":[{"message":"Bad credentials"}]}`}}}
	if _, err := (&GH{Runner: f}).PRs(context.Background(), time.Now()); err == nil || !strings.Contains(err.Error(), "Bad credentials") {
		t.Fatalf("err = %v", err)
	}
}

func TestLinkTaskGitHubIssues(t *testing.T) {
	tasks := []task.Task{
		{Source: IssuesSource, ID: "acme/shop#42", ListID: "acme/shop"},
		{Source: IssuesSource, ID: "acme/shop#7", ListID: "acme/shop"},
		{Source: IssuesSource, ID: "acme/lib#9", ListID: "acme/lib"},
	}
	cases := []struct {
		name string
		pr   PR
		want string
	}{
		{"closing reference wins", PR{Repo: "acme/shop", Branch: "feature/42-x", Closes: []string{"acme/shop#7"}}, "acme/shop#7"},
		{"closing reference in another repo", PR{Repo: "acme/shop", Branch: "fix/x", Closes: []string{"acme/lib#9"}}, "acme/lib#9"},
		{"number in branch", PR{Repo: "acme/shop", Branch: "feature/42-checkout"}, "acme/shop#42"},
		{"bare number word", PR{Repo: "acme/shop", Branch: "fix/42"}, "acme/shop#42"},
		{"number in branch of another repo", PR{Repo: "acme/lib", Branch: "feature/42-x"}, ""},
		{"number inside a longer word", PR{Repo: "acme/shop", Branch: "feature/142-x"}, ""},
		{"number in title only", PR{Repo: "acme/shop", Branch: "fix/x", Title: "42 things"}, ""},
	}
	for _, c := range cases {
		got, _ := LinkTask(c.pr, tasks)
		if got.ID != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got.ID, c.want)
		}
	}
}

func TestLinkTask(t *testing.T) {
	tasks := []task.Task{
		clickup.ToTask(clickup.Task{ID: "86a1b2c3d", CustomID: "DEV-7725"}, 0),
		clickup.ToTask(clickup.Task{ID: "86c1x2abc"}, 0), // no custom ID
		clickup.ToTask(clickup.Task{ID: "x9", CustomID: "WEB-12"}, 0),
	}
	cases := []struct {
		branch, title, want string
	}{
		{"feature/dev-7725-free-can", "", "86a1b2c3d"},
		{"fix/abc", "DEV-7725 title", "86a1b2c3d"},
		{"fix/abc", "chore: deps", ""},
		{"feature/dev-77255-x", "", ""},         // dev-77255 is not DEV-7725
		{"feature/xdev-7725", "", ""},           // must be a whole word
		{"CU-86c1x2abc-thing", "", "86c1x2abc"}, // ClickUp's own form
		{"feature/86c1x2abc-thing", "", "86c1x2abc"},
		{"feature/web-12-new-prefix", "", "x9"}, // any prefix works
	}
	for _, c := range cases {
		got, _ := LinkTask(PR{Branch: c.branch, Title: c.title}, tasks)
		if got.ID != c.want {
			t.Errorf("%q / %q: got %q, want %q", c.branch, c.title, got.ID, c.want)
		}
	}
}
