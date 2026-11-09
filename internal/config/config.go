// Package config loads huddle's config file and the ClickUp token.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/slavafyi/huddle/internal/run"
)

// Config is the user config from ~/.config/huddle/config.toml.
type Config struct {
	WorkspaceID      string              `toml:"workspace_id"`
	LeadFieldID      string              `toml:"lead_field_id"`
	RepoRoots        []string            `toml:"repo_roots"`
	WorktreeRoot     string              `toml:"worktree_root"`
	ScratchRoot      string              `toml:"scratch_root"`
	GitHubOrg        string              `toml:"github_org"`
	BranchTemplate   string              `toml:"branch_template"`
	PromptTemplate   string              `toml:"prompt_template"`
	ScratchTypes     []string            `toml:"scratch_task_types"`
	ScratchPrefixes  []string            `toml:"scratch_title_prefixes"`
	Agents           map[string][]string `toml:"agents"`
	Repos            Repos               `toml:"repos"`
	StatusInProgress string              `toml:"status_in_progress"`
}

// Repos holds per-repo settings, keyed by repo folder name.
type Repos struct {
	BaseBranch map[string]string `toml:"base_branch"`
}

// ShellAgent is the agent name that opens a plain shell.
const ShellAgent = "shell"

// Default returns the config used for every field the file leaves out.
func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		WorktreeRoot:     filepath.Join(home, ".local/share/huddle/worktrees"),
		ScratchRoot:      filepath.Join(home, "research"),
		BranchTemplate:   "feature/{id_lower}-{slug}",
		PromptTemplate:   "Take a task {id} from CU",
		ScratchTypes:     []string{"Idea"},
		ScratchPrefixes:  []string{"research", "evaluate", "investigate", "check", "audit"},
		StatusInProgress: "in progress",
		Agents: map[string][]string{
			"claude": {"claude", "{prompt}"},
			"codex":  {"codex", "{prompt}"},
		},
	}
}

// Path returns the config file path.
func Path() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "huddle", "config.toml")
}

// Load reads path over the defaults. A missing file is not an error.
func Load(path string) (Config, error) {
	c := Default()
	agents := c.Agents
	c.Agents = nil
	if _, err := toml.DecodeFile(path, &c); err != nil && !errors.Is(err, os.ErrNotExist) {
		return c, fmt.Errorf("read config %s: %w", path, err)
	}
	for name, argv := range agents {
		if _, ok := c.Agents[name]; !ok {
			if c.Agents == nil {
				c.Agents = map[string][]string{}
			}
			c.Agents[name] = argv
		}
	}
	for i, r := range c.RepoRoots {
		c.RepoRoots[i] = ExpandHome(r)
	}
	c.WorktreeRoot = ExpandHome(c.WorktreeRoot)
	c.ScratchRoot = ExpandHome(c.ScratchRoot)
	return c, nil
}

// Validate reports settings that must be set in the file.
func (c Config) Validate() error {
	var missing []string
	if c.WorkspaceID == "" {
		missing = append(missing, "workspace_id")
	}
	if c.LeadFieldID == "" {
		missing = append(missing, "lead_field_id")
	}
	if len(missing) > 0 {
		return fmt.Errorf("set %s in %s", strings.Join(missing, " and "), Path())
	}
	return nil
}

// BaseBranch returns the configured base branch for a repo, or "" to use the
// remote's default branch.
func (c Config) BaseBranch(repo string) string {
	return c.Repos.BaseBranch[repo]
}

// AgentNames lists agents for the Start panel: claude first, then the others
// sorted, then the plain shell.
func (c Config) AgentNames() []string {
	var names []string
	for n := range c.Agents {
		if n != "claude" && n != ShellAgent {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if _, ok := c.Agents["claude"]; ok {
		names = append([]string{"claude"}, names...)
	}
	return append(names, ShellAgent)
}

// ExpandHome replaces a leading ~ with the home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// Token returns the ClickUp API token from CLICKUP_API_TOKEN or the macOS
// keychain item "huddle-clickup". The token is never logged.
func Token(ctx context.Context, r run.Runner) (string, error) {
	if t := strings.TrimSpace(os.Getenv("CLICKUP_API_TOKEN")); t != "" {
		return t, nil
	}
	out, err := r.Run(ctx, run.Cmd{Name: "security", Args: []string{"find-generic-password", "-s", "huddle-clickup", "-w"}})
	if t := strings.TrimSpace(string(out)); err == nil && t != "" {
		return t, nil
	}
	return "", errors.New("no ClickUp token: set CLICKUP_API_TOKEN or add keychain item huddle-clickup")
}
