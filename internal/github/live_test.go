package github

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/slavafyi/cuddle/internal/run"
)

// TestLivePRs runs the real (read-only) gh query.
// Run with: CUDDLE_GH_LIVE=1 [CUDDLE_GH_ORG=org] go test -run LivePRs ./internal/github -v
func TestLivePRs(t *testing.T) {
	if os.Getenv("CUDDLE_GH_LIVE") != "1" {
		t.Skip("set CUDDLE_GH_LIVE=1 to query GitHub (read-only)")
	}
	g := &GH{Runner: run.Exec{}, Org: os.Getenv("CUDDLE_GH_ORG")}
	prs, err := g.PRs(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	counts := map[Kind]int{}
	for _, p := range prs {
		counts[p.Kind]++
	}
	t.Logf("org=%q mine=%d review=%d merged=%d", g.Org, counts[Mine], counts[ReviewRequested], counts[Merged])
	for _, p := range prs[:min(3, len(prs))] {
		t.Logf("  #%d %s %s ci=%s review=%s", p.Number, p.Repo, p.Branch, p.CI, p.Review)
	}
}
