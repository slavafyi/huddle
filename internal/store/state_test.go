package store

import (
	"reflect"
	"testing"

	"github.com/slavafyi/huddle/internal/rex"
	"github.com/slavafyi/huddle/internal/task"
)

func TestRememberRepo(t *testing.T) {
	var s State
	for _, r := range []string{"a", "b", "c", "a", "d", "e", "f"} {
		s.RememberRepo("Snowcap", r)
	}
	want := []string{"f", "e", "d", "a", "c"}
	if got := s.RecentRepos["Snowcap"]; !reflect.DeepEqual(got, want) {
		t.Errorf("recent = %v, want %v", got, want)
	}
}

func TestAddAndRemoveAgent(t *testing.T) {
	var s State
	s.AddAgent(Agent{TaskID: "t1", Client: "Snowcap", Placement: rex.Placement{SessionID: "session:S", BlockID: "block:1"}})
	s.AddAgent(Agent{TaskID: "t2", Client: "Snowcap", Placement: rex.Placement{SessionID: "session:S", BlockID: "block:2"}})
	if s.ClientSessions["Snowcap"] != "session:S" || len(s.Agents) != 2 {
		t.Fatalf("state = %+v", s)
	}
	s.RemoveAgent("block:1")
	if len(s.Agents) != 1 || s.Agents[0].TaskID != "t2" {
		t.Errorf("agents = %+v", s.Agents)
	}
}

func TestMigrateV1AgentsAreClickUp(t *testing.T) {
	s := State{Version: 1, Agents: []Agent{{TaskID: "t1"}, {TaskID: "gh1", Source: "github"}}}
	s.Migrate()
	if s.Version != StateVersion || StateVersion != 2 {
		t.Errorf("version = %d", s.Version)
	}
	if s.Agents[0].Source != "clickup" || s.Agents[1].Source != "github" {
		t.Errorf("agents = %+v", s.Agents)
	}
	if s.Agents[0].TaskRef() != task.Ref("clickup", "t1") {
		t.Errorf("ref = %q", s.Agents[0].TaskRef())
	}
}
