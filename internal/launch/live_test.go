package launch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/slavafyi/cuddle/internal/clickup"
	"github.com/slavafyi/cuddle/internal/rex"
	"github.com/slavafyi/cuddle/internal/run"
	"github.com/slavafyi/cuddle/internal/workspace"
)

// noFocus is the real rex CLI without Focus, so the test does not switch
// the app the user is looking at.
type noFocus struct{ *rex.CLI }

func (noFocus) Focus(context.Context, string, string) error { return nil }

// TestLiveLaunch starts two shell tabs in a throwaway client session with
// the real rex CLI and a fake ClickUp, then removes the session.
// Run with: CUDDLE_REX_LIVE=1 go test -run LiveLaunch ./internal/launch -v
func TestLiveLaunch(t *testing.T) {
	if os.Getenv("CUDDLE_REX_LIVE") != "1" {
		t.Skip("set CUDDLE_REX_LIVE=1 to run against the local Rex server")
	}
	ctx := context.Background()
	rx := &rex.CLI{Runner: run.Exec{}}
	cu := &fakeCU{}
	d := Deps{Runner: run.Exec{}, Rex: noFocus{rx}, ClickUp: cu, Now: time.Now}
	root := t.TempDir()
	tk := clickup.Task{ID: "x1", CustomID: "TEST-1", Name: "Live launch check", ListName: "cuddle-live-client"}
	p := Plan{Task: tk, TabName: "TEST-1 Live launch check",
		Workspace: workspace.Plan{Kind: workspace.Scratch, ScratchRoot: root, TaskKey: "test-1", Slug: "live-launch-check"}}

	first, err := Run(ctx, d, p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { run.Exec{}.Run(ctx, run.Cmd{Name: "rex", Args: []string{"kill", first.Agent.SessionID}}) })
	if st, err := os.Stat(filepath.Join(root, "test-1-live-launch-check")); err != nil || !st.IsDir() {
		t.Fatal("scratch folder missing")
	}
	ss, _ := rx.Sessions(ctx)
	if ss[first.Agent.SessionID] != "cuddle-live-client" {
		t.Fatalf("session label = %q", ss[first.Agent.SessionID])
	}

	// A second task for the same client opens a tab in the same session,
	// found by name even without a stored ID.
	p.Task.ID, p.TabName = "x2", "TEST-2 Second"
	second, err := Run(ctx, d, p)
	if err != nil {
		t.Fatal(err)
	}
	if second.Agent.SessionID != first.Agent.SessionID {
		t.Errorf("second tab in another session: %s vs %s", second.Agent.SessionID, first.Agent.SessionID)
	}
	blocks, _ := rx.Blocks(ctx, first.Agent.SessionID)
	if !blocks[first.Agent.BlockID] || !blocks[second.Agent.BlockID] {
		t.Errorf("blocks = %v", blocks)
	}
	if len(cu.writes) != 0 {
		t.Errorf("unexpected ClickUp writes: %v", cu.writes)
	}
}
