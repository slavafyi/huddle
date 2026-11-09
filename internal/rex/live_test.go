package rex

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/slavafyi/huddle/internal/run"
)

// TestLiveRex runs the real rex CLI in a throwaway session and removes it.
// Run with: HUDDLE_REX_LIVE=1 go test -run LiveRex ./internal/rex -v
func TestLiveRex(t *testing.T) {
	if os.Getenv("HUDDLE_REX_LIVE") != "1" {
		t.Skip("set HUDDLE_REX_LIVE=1 to run against the local Rex server")
	}
	ctx := context.Background()
	c := &CLI{Runner: run.Exec{}}
	dir := t.TempDir()
	report := `printf '\033]7501;state=blocked:kind=question:msg=TmVlZCBpbnB1dA\033\\'; echo ready; sleep 30`

	events, err := Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.NewSession(ctx, "huddle-live-test", dir, "first tab", []string{"sh", "-c", report})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { run.Exec{}.Run(ctx, run.Cmd{Name: "rex", Args: []string{"kill", p.SessionID}}) })

	ss, err := c.Sessions(ctx)
	if err != nil || ss[p.SessionID] != "huddle-live-test" {
		t.Fatalf("sessions: %v %v", ss[p.SessionID], err)
	}
	w, err := c.NewWindow(ctx, p.SessionID, "second tab", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	blocks, _ := c.Blocks(ctx, p.SessionID)
	if !blocks[p.BlockID] || !blocks[w.BlockID] {
		t.Fatalf("blocks: %v", blocks)
	}

	deadline := time.After(10 * time.Second)
	gotEvent := false
	for !gotEvent {
		select {
		case ev := <-events:
			if ev.BlockID == p.BlockID && ev.HasStatus && ev.Status.State == "blocked" {
				gotEvent = true
			}
		case <-deadline:
			t.Fatal("no status event from rex events --all")
		}
	}
	st, err := c.Status(ctx, p.SessionID, p.BlockID)
	if err != nil || st.State != "blocked" || st.Msg != "Need input" {
		t.Errorf("status = %+v, %v", st, err)
	}
	if l, err := c.LastLine(ctx, p.SessionID, p.BlockID); err != nil || l != "ready" {
		t.Errorf("last line = %q, %v", l, err)
	}
	if err := c.CloseWindow(ctx, p.SessionID, w.WindowID); err != nil {
		t.Errorf("close: %v", err)
	}
	blocks, _ = c.Blocks(ctx, p.SessionID)
	if blocks[w.BlockID] {
		t.Error("closed tab still has a block")
	}
	if b, _ := c.Blocks(ctx, "session:00000000-0000-0000-0000-000000000000"); len(b) != 0 {
		t.Error("missing session should have no blocks")
	}
}
