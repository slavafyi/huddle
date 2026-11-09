package github

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/slavafyi/huddle/internal/run"
	"github.com/slavafyi/huddle/internal/task"
)

var issuesNow = time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)

const issuesReply = `{"data": {
  "openAssigned": {"nodes": [
    {"number": 42, "title": "Fix checkout", "url": "https://github.com/acme/shop/issues/42", "state": "OPEN",
     "closedAt": null, "updatedAt": "2026-11-08T09:00:00Z",
     "repository": {"nameWithOwner": "acme/shop", "name": "shop"},
     "author": {"login": "bob", "databaseId": 2},
     "assignees": {"nodes": [{"login": "me", "databaseId": 1}, {"login": "ann", "databaseId": 3}]},
     "labels": {"nodes": [{"name": "bug"}, {"name": "Priority:Urgent"}]}},
    {}
  ]},
  "openAuthored": {"nodes": [
    {"number": 42, "title": "Fix checkout", "url": "https://github.com/acme/shop/issues/42", "state": "OPEN",
     "closedAt": null, "updatedAt": "2026-11-08T09:00:00Z",
     "repository": {"nameWithOwner": "acme/shop", "name": "shop"},
     "author": {"login": "bob", "databaseId": 2},
     "assignees": {"nodes": [{"login": "me", "databaseId": 1}, {"login": "ann", "databaseId": 3}]},
     "labels": {"nodes": []}},
    {"number": 7, "title": "Docs", "url": "u7", "state": "OPEN", "closedAt": null, "updatedAt": "2026-11-01T09:00:00Z",
     "repository": {"nameWithOwner": "me/tool", "name": "tool"},
     "author": {"login": "me", "databaseId": 1}, "assignees": {"nodes": []},
     "labels": {"nodes": [{"name": "priority:high"}]}}
  ]},
  "closedAssigned": {"nodes": [
    {"number": 3, "title": "Old", "url": "u3", "state": "CLOSED", "closedAt": "2026-11-07T12:00:00Z",
     "updatedAt": "2026-11-07T12:00:00Z", "repository": {"nameWithOwner": "acme/shop", "name": "shop"},
     "author": {"login": "bob", "databaseId": 2}, "assignees": {"nodes": [{"login": "me", "databaseId": 1}]},
     "labels": {"nodes": []}}
  ]},
  "closedAuthored": {"nodes": []}
}}`

func newIssues(f *run.Fake) *Issues {
	return &Issues{GH: &GH{Runner: f, Orgs: []string{"acme"}, Token: "tok"}}
}

func TestIssuesFetchFull(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "gh api graphql", Out: issuesReply}}}
	ts, err := newIssues(f).Fetch(context.Background(), 1, time.Time{}, issuesNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 3 {
		t.Fatalf("tasks = %d (duplicates and empty nodes must go): %+v", len(ts), ts)
	}
	byID := map[string]task.Task{}
	for _, x := range ts {
		byID[x.ID] = x
	}
	got := byID["acme/shop#42"]
	want := task.Task{
		Source: IssuesSource, ID: "acme/shop#42", Key: "shop#42", Name: "Fix checkout",
		URL: "https://github.com/acme/shop/issues/42", Status: "open", ListID: "acme/shop", Project: "shop",
		Priority: "urgent", Labels: []string{"bug"}, Mine: true, Others: []string{"ann"}, Updated: time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("issue 42:\n got %+v\nwant %+v", got, want)
	}
	if mine := byID["me/tool#7"]; mine.Mine || !mine.Lead || mine.Priority != "high" || mine.Key != "tool#7" {
		t.Errorf("issue 7 = %+v", mine)
	}
	if c := byID["acme/shop#3"]; c.Status != "closed" || c.StatusOrder != 1 || !c.Done.Equal(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("closed issue = %+v", c)
	}
	call := f.CallsWithDir()[0]
	if !reflect.DeepEqual(call.Env, []string{"GH_TOKEN=tok"}) {
		t.Errorf("env = %v", call.Env)
	}
	for _, q := range []string{
		"openAssigned=is:issue is:open assignee:@me archived:false org:acme",
		"openAuthored=is:issue is:open author:@me archived:false org:acme",
		"closedAssigned=is:issue is:closed assignee:@me closed:>=2026-11-02 archived:false org:acme",
		"closedAuthored=is:issue is:closed author:@me closed:>=2026-11-02 archived:false org:acme",
	} {
		if !strings.Contains(call.String(), q) {
			t.Errorf("query missing %q in %s", q, call.String())
		}
	}
}

func TestIssuesFetchSince(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "gh api graphql", Out: `{"data":{"assigned":{"nodes":[]},"authored":{"nodes":[]}}}`}}}
	is := &Issues{GH: &GH{Runner: f, Exclude: []string{"acme"}}}
	since := time.Date(2026, 11, 9, 9, 0, 0, 0, time.FixedZone("x", 3600))
	if _, err := is.Fetch(context.Background(), 1, since, issuesNow); err != nil {
		t.Fatal(err)
	}
	call := f.CallsWithDir()[0]
	for _, q := range []string{
		"assigned=is:issue assignee:@me updated:>=2026-11-09T08:00:00+00:00 archived:false -org:acme",
		"authored=is:issue author:@me updated:>=2026-11-09T08:00:00+00:00 archived:false -org:acme",
	} {
		if !strings.Contains(call.String(), q) {
			t.Errorf("query missing %q in %s", q, call.String())
		}
	}
	if call.Env != nil {
		t.Errorf("no token: env = %v", call.Env)
	}
}

func TestIssuesMe(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "gh api user", Out: `{"login":"me","id":1234}`}}}
	id, err := newIssues(f).Me(context.Background())
	if err != nil || id != 1234 {
		t.Fatalf("me = %d, %v", id, err)
	}
	if env := f.CallsWithDir()[0].Env; len(env) != 1 || env[0] != "GH_TOKEN=tok" {
		t.Errorf("env = %v", env)
	}
}

func TestIssuesWrites(t *testing.T) {
	is := task.Task{Source: IssuesSource, ID: "acme/shop#42", ListID: "acme/shop"}
	ctx := context.Background()
	cases := []struct {
		do   func(s *Issues) error
		want []string
	}{
		{func(s *Issues) error { return s.SetStatus(ctx, is, "closed") }, []string{"issue", "close", "42", "-R", "acme/shop"}},
		{func(s *Issues) error { return s.SetStatus(ctx, is, "open") }, []string{"issue", "reopen", "42", "-R", "acme/shop"}},
		{func(s *Issues) error { return s.AddComment(ctx, is, "on it; see #3") }, []string{"issue", "comment", "42", "-R", "acme/shop", "--body", "on it; see #3"}},
		{func(s *Issues) error { return s.SetAssignee(ctx, is, 1, true) }, []string{"issue", "edit", "42", "-R", "acme/shop", "--add-assignee", "@me"}},
		{func(s *Issues) error { return s.SetAssignee(ctx, is, 1, false) }, []string{"issue", "edit", "42", "-R", "acme/shop", "--remove-assignee", "@me"}},
	}
	for _, c := range cases {
		f := &run.Fake{}
		if err := c.do(newIssues(f)); err != nil {
			t.Fatal(err)
		}
		calls := f.CallsWithDir()
		if len(calls) != 1 || calls[0].Name != "gh" || !reflect.DeepEqual(calls[0].Args, c.want) ||
			!reflect.DeepEqual(calls[0].Env, []string{"GH_TOKEN=tok"}) {
			t.Errorf("calls = %+v, want gh %v", calls, c.want)
		}
	}
	if err := newIssues(&run.Fake{}).SetStatus(ctx, is, "to review"); err == nil {
		t.Error("unknown status must fail")
	}
}

func TestIssuesStatusesAndRules(t *testing.T) {
	st, err := newIssues(&run.Fake{}).Statuses(context.Background(), task.Task{})
	if err != nil || !reflect.DeepEqual(st, []task.Status{{Name: "open"}, {Name: "closed", Order: 1, Done: true}}) {
		t.Fatalf("statuses = %v, %v", st, err)
	}
	r := newIssues(&run.Fake{}).Rules()
	if len(r.Startable) != 0 || !r.RepoInList || r.BranchTemplate != "feature/{number}-{slug}" || r.PromptTemplate != "Take issue {full_id}" {
		t.Errorf("rules = %+v", r)
	}
	s := task.Signals{Live: map[string]bool{"github:a/b#1": true}, InReview: map[string]bool{"github:a/b#2": true}}
	for _, c := range []struct {
		t    task.Task
		want string
	}{
		{task.Task{Source: IssuesSource, ID: "a/b#1", Lead: true, Others: []string{"x"}}, task.GroupInProgress},
		{task.Task{Source: IssuesSource, ID: "a/b#2", Mine: true}, task.GroupInReview},
		{task.Task{Source: IssuesSource, ID: "a/b#3", Mine: true}, task.GroupOpen},
		{task.Task{Source: IssuesSource, ID: "a/b#4", Lead: true}, task.GroupOpen},
		{task.Task{Source: IssuesSource, ID: "a/b#5", Lead: true, Others: []string{"x"}}, ""},
	} {
		if got := r.Classify(c.t, s); got != c.want {
			t.Errorf("%s: %q, want %q", c.t.ID, got, c.want)
		}
	}
}
