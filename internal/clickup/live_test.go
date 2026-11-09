package clickup

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/slavafyi/huddle/internal/task"
)

// TestLiveReadOnly checks parsing against the real API. It only reads.
// Run with: HUDDLE_LIVE=1 HUDDLE_TEAM=... HUDDLE_LEAD_FIELD=... go test -run Live ./internal/clickup -v
func TestLiveReadOnly(t *testing.T) {
	if os.Getenv("HUDDLE_LIVE") != "1" {
		t.Skip("set HUDDLE_LIVE=1 to run against the real ClickUp API (read-only)")
	}
	c := &HTTP{BaseURL: DefaultBaseURL, Token: os.Getenv("CLICKUP_API_TOKEN"),
		TeamID: os.Getenv("HUDDLE_TEAM"), LeadFieldID: os.Getenv("HUDDLE_LEAD_FIELD")}
	ctx := context.Background()
	src := &Source{Client: c}
	me, err := src.Me(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tasks, err := src.Fetch(ctx, me, time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	var leadOnly, done, typed int
	for _, tk := range tasks {
		if tk.Lead && !tk.Mine {
			leadOnly++
		}
		if tk.IsDone() {
			done++
		}
		if tk.Type != "" {
			typed++
		}
	}
	t.Logf("me=%d tasks=%d leadOnly=%d done=%d withType=%d", me, len(tasks), leadOnly, done, typed)
	for _, g := range task.Today(tasks, map[string]task.Rules{SourceName: src.Rules()}, task.Signals{}, now) {
		t.Logf("  %s: %d", g.Name, len(g.Tasks))
	}
	if len(tasks) == 0 {
		t.Error("no tasks")
	}
}

// TestLiveDetect finds the workspace and lead field. It only reads.
// Run with: HUDDLE_LIVE=1 go test -run LiveDetect ./internal/clickup -v
// HUDDLE_TEAM and HUDDLE_LEAD_FIELD, when set, are the expected IDs.
func TestLiveDetect(t *testing.T) {
	if os.Getenv("HUDDLE_LIVE") != "1" {
		t.Skip("set HUDDLE_LIVE=1 to run against the real ClickUp API (read-only)")
	}
	d := &Detector{HTTP: &HTTP{BaseURL: DefaultBaseURL, Token: os.Getenv("CLICKUP_API_TOKEN")}}
	note, err := d.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("workspace=%s lead=%s note=%q", d.HTTP.TeamID, d.HTTP.LeadFieldID, note)
	if w := os.Getenv("HUDDLE_TEAM"); w != "" && d.HTTP.TeamID != w {
		t.Errorf("workspace %s, want %s", d.HTTP.TeamID, w)
	}
	if l := os.Getenv("HUDDLE_LEAD_FIELD"); l != "" && d.HTTP.LeadFieldID != l {
		t.Errorf("lead field %s, want %s", d.HTTP.LeadFieldID, l)
	}
}
