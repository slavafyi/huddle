package github

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/slavafyi/huddle/internal/run"
)

// TestLivePRs runs the real (read-only) gh query.
// Run with: HUDDLE_GH_LIVE=1 [HUDDLE_GH_ORG=org] go test -run LivePRs ./internal/github -v
func TestLivePRs(t *testing.T) {
	if os.Getenv("HUDDLE_GH_LIVE") != "1" {
		t.Skip("set HUDDLE_GH_LIVE=1 to query GitHub (read-only)")
	}
	g := &GH{Runner: run.Exec{}, Orgs: strings.Fields(os.Getenv("HUDDLE_GH_ORG"))}
	prs, err := g.PRs(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	counts := map[Kind]int{}
	for _, p := range prs {
		counts[p.Kind]++
	}
	t.Logf("orgs=%q mine=%d review=%d merged=%d", g.Orgs, counts[Mine], counts[ReviewRequested], counts[Merged])
	for _, p := range prs[:min(3, len(prs))] {
		t.Logf("  #%d %s %s ci=%s review=%s", p.Number, p.Repo, p.Branch, p.CI, p.Review)
	}
}

// TestLiveIssues runs the real (read-only) issue queries as slavafyi, in
// both halves of the org split.
// Run with: HUDDLE_GH_LIVE=1 go test -run LiveIssues ./internal/github -v
func TestLiveIssues(t *testing.T) {
	if os.Getenv("HUDDLE_GH_LIVE") != "1" {
		t.Skip("set HUDDLE_GH_LIVE=1 to query GitHub (read-only)")
	}
	ctx := context.Background()
	token, err := Token(ctx, run.Exec{}, "slavafyi")
	if err != nil {
		t.Fatal(err)
	}
	for _, gh := range []*GH{
		{Runner: run.Exec{}, Token: token, Orgs: []string{"vertexdigital"}},
		{Runner: run.Exec{}, Token: token, Exclude: []string{"vertexdigital"}},
	} {
		src := &Issues{GH: gh}
		me, err := src.Me(ctx)
		if err != nil {
			t.Fatal(err)
		}
		ts, err := src.Fetch(ctx, me, time.Time{}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		var open, closed, mine, opened int
		for _, x := range ts {
			if x.IsDone() {
				closed++
			} else {
				open++
			}
			if x.Mine {
				mine++
			}
			if x.Lead {
				opened++
			}
		}
		t.Logf("scope=%q open=%d closed=%d mine=%d opened-by-me=%d", gh.scope(), open, closed, mine, opened)
	}
}
