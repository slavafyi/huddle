package store

import (
	"time"

	"github.com/slavafyi/cuddle/internal/rex"
)

// StateVersion is the current state file format.
const StateVersion = 1

// maxRecentRepos is how many repos are remembered per client.
const maxRecentRepos = 5

// Agent is a tab cuddle started for a task.
type Agent struct {
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

// State is what cuddle remembers between runs. Unlike the cache, it is not
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
