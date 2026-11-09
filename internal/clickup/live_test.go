package clickup

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveReadOnly checks parsing against the real API. It only reads.
// Run with: CUDDLE_LIVE=1 CUDDLE_TEAM=... CUDDLE_LEAD_FIELD=... go test -run Live ./internal/clickup -v
func TestLiveReadOnly(t *testing.T) {
	if os.Getenv("CUDDLE_LIVE") != "1" {
		t.Skip("set CUDDLE_LIVE=1 to run against the real ClickUp API (read-only)")
	}
	c := &HTTP{BaseURL: DefaultBaseURL, Token: os.Getenv("CLICKUP_API_TOKEN"),
		TeamID: os.Getenv("CUDDLE_TEAM"), LeadFieldID: os.Getenv("CUDDLE_LEAD_FIELD")}
	ctx := context.Background()
	me, err := c.Me(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tasks, err := c.MyTasks(ctx, me.ID, time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	var leadOnly, done, typed int
	for _, tk := range tasks {
		if tk.LeadIs(me.ID) && !tk.AssignedTo(me.ID) {
			leadOnly++
		}
		if tk.IsDone() {
			done++
		}
		if tk.TypeName != "" {
			typed++
		}
	}
	t.Logf("me=%s tasks=%d leadOnly=%d done=%d withType=%d", me.Username, len(tasks), leadOnly, done, typed)
	for _, g := range Today(tasks, me.ID, now) {
		t.Logf("  %s: %d", g.Name, len(g.Tasks))
	}
	if len(tasks) == 0 {
		t.Error("no tasks")
	}
}
