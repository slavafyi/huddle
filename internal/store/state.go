package store

import (
	"time"

	"github.com/slavafyi/huddle/internal/rex"
	"github.com/slavafyi/huddle/internal/task"
)

// StateVersion is the current state file format. Version 2 added
// Agent.Source.
const StateVersion = 2

// maxRecentRepos is how many repos are remembered per client.
const maxRecentRepos = 5

// Agent is a tab huddle started for a task.
type Agent struct {
	Source    string `json:"source"` // the task's source
	TaskID    string `json:"task_id"`
	TaskKey   string `json:"task_key"`
	Title     string `json:"title"`
	Client    string `json:"client"`
	Workspace string `json:"workspace"`
	Dir       string `json:"dir"`
	Repo      string `json:"repo,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Agent     string `json:"agent"`
	rex.Placement
	Started time.Time `json:"started"`
}

// TaskRef is the agent's task ID, unique across sources (task.Ref).
func (a Agent) TaskRef() string { return task.Ref(a.Source, a.TaskID) }

// State is what huddle remembers between runs. Unlike the cache, it is not
// safe to delete: it links tasks to Rex tabs.
type State struct {
	Version        int                 `json:"version"`
	Agents         []Agent             `json:"agents"`
	ClientSessions map[string]string   `json:"client_sessions"`
	RecentRepos    map[string][]string `json:"recent_repos"`
}

// RememberRepo moves repo to the front of the client's recent list.
func (s *State) RememberRepo(client, repo string) {
	if s.RecentRepos == nil {
		s.RecentRepos = map[string][]string{}
	}
	list := []string{repo}
	for _, r := range s.RecentRepos[client] {
		if r != repo && len(list) < maxRecentRepos {
			list = append(list, r)
		}
	}
	s.RecentRepos[client] = list
}

// AddAgent records a new agent tab and the client's session.
func (s *State) AddAgent(a Agent) {
	s.Agents = append(s.Agents, a)
	if s.ClientSessions == nil {
		s.ClientSessions = map[string]string{}
	}
	s.ClientSessions[a.Client] = a.SessionID
}

// RemoveAgent forgets the agent with the given block ID.
func (s *State) RemoveAgent(block string) {
	var keep []Agent
	for _, a := range s.Agents {
		if a.BlockID != block {
			keep = append(keep, a)
		}
	}
	s.Agents = keep
}

// Migrate upgrades a state file from an older version: version 1 only had
// ClickUp tasks.
func (s *State) Migrate() {
	for i := range s.Agents {
		if s.Agents[i].Source == "" {
			s.Agents[i].Source = "clickup"
		}
	}
	s.Version = StateVersion
}
