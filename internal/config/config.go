// Package config loads huddle's config file and the ClickUp token.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/slavafyi/huddle/internal/run"
)

// Config is the user config from ~/.config/huddle/config.toml.
type Config struct {
	RepoRoots        []string            `toml:"repo_roots"`
	WorktreeRoot     string              `toml:"worktree_root"`
	ScratchRoot      string              `toml:"scratch_root"`
	BranchTemplate   string              `toml:"branch_template"`
	PromptTemplate   string              `toml:"prompt_template"`
	ScratchTypes     []string            `toml:"scratch_task_types"`
	ScratchPrefixes  []string            `toml:"scratch_title_prefixes"`
	Agents           map[string][]string `toml:"agents"`
	Repos            Repos               `toml:"repos"`
	StatusInProgress string              `toml:"status_in_progress"`
	Profiles         []Profile           `toml:"-"` // in file order
}

// Profile is one named world: its sources, GitHub account, cache and state.
// In the file, each top-level table that is not a reserved one is a profile.
type Profile struct {
	Name             string   `toml:"-"`
	ClickUp          bool     `toml:"clickup"`
	ClickUpWorkspace string   `toml:"clickup_workspace"` // optional: found automatically
	ClickUpLead      string   `toml:"clickup_lead"`      // optional: found automatically
	GitHubAccount    string   `toml:"github_account"`
	GitHubOrgs       []string `toml:"github_orgs"`
	GitHubIssues     *bool    `toml:"github_issues"` // nil means on
	// Overrides of the shared settings.
	RepoRoots      []string `toml:"repo_roots"`
	ScratchRoot    string   `toml:"scratch_root"`
	PromptTemplate string   `toml:"prompt_template"`
}

// IssuesOn reports whether the profile shows GitHub issues: yes unless
// github_issues = false.
func (p Profile) IssuesOn() bool { return p.GitHubIssues == nil || *p.GitHubIssues }

// OtherOrgs lists the GitHub orgs that other profiles claim. A profile
// without github_orgs leaves them out of its searches; a profile with
// github_orgs searches only those, so it gets none.
func (c Config) OtherOrgs(p Profile) []string {
	if len(p.GitHubOrgs) > 0 {
		return nil
	}
	var out []string
	for _, o := range c.Profiles {
		if o.Name != p.Name {
			out = append(out, o.GitHubOrgs...)
		}
	}
	return out
}

// reserved are top-level tables that are shared settings, not profiles.
var reserved = map[string]bool{"agents": true, "repos": true}

// Summary describes a profile's sources, e.g. "ClickUp · vertexdigital".
func (p Profile) Summary() string {
	var parts []string
	if p.ClickUp {
		parts = append(parts, "ClickUp")
	}
	switch {
	case len(p.GitHubOrgs) > 0:
		parts = append(parts, strings.Join(p.GitHubOrgs, ", "))
	case !p.ClickUp:
		parts = append(parts, "GitHub")
		if p.GitHubAccount != "" {
			parts = append(parts, p.GitHubAccount)
		}
	}
	return strings.Join(parts, " · ")
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
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return c, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := decode(string(data), &c); err != nil {
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
	c.RepoRoots = expandAll(c.RepoRoots)
	c.WorktreeRoot = ExpandHome(c.WorktreeRoot)
	c.ScratchRoot = ExpandHome(c.ScratchRoot)
	for i := range c.Profiles {
		c.Profiles[i].RepoRoots = expandAll(c.Profiles[i].RepoRoots)
		c.Profiles[i].ScratchRoot = ExpandHome(c.Profiles[i].ScratchRoot)
	}
	return c, nil
}

// decode reads the shared settings into c, and each other top-level table
// into c.Profiles in file order. Unknown settings are errors.
func decode(data string, c *Config) error {
	md, err := toml.Decode(data, c)
	if err != nil {
		return err
	}
	var tables map[string]toml.Primitive
	pmd, err := toml.Decode(data, &tables)
	if err != nil {
		return err
	}
	var unknown []string
	for _, k := range md.Keys() {
		if len(k) != 1 || reserved[k[0]] {
			continue
		}
		if md.Type(k[0]) != "Hash" {
			if slices.ContainsFunc(md.Undecoded(), func(u toml.Key) bool { return u.String() == k.String() }) {
				unknown = append(unknown, k.String())
			}
			continue
		}
		p := Profile{Name: k[0]}
		if err := pmd.PrimitiveDecode(tables[k[0]], &p); err != nil {
			return fmt.Errorf("[%s]: %w", k[0], err)
		}
		c.Profiles = append(c.Profiles, p)
	}
	for _, k := range pmd.Undecoded() {
		if len(k) > 1 && !reserved[k[0]] {
			unknown = append(unknown, k.String())
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown setting %s", strings.Join(unknown, ", "))
	}
	return nil
}

func expandAll(paths []string) []string {
	for i, p := range paths {
		paths[i] = ExpandHome(p)
	}
	return paths
}

// Validate reports settings that must be fixed in the file.
func (c Config) Validate() error {
	if len(c.Profiles) == 0 {
		return fmt.Errorf("no profiles in %s: add a table such as [work] (see SPEC.md §5)", Path())
	}
	for _, p := range c.Profiles {
		if !validName(p.Name) {
			return fmt.Errorf("profile name %q: use letters, digits, - and _ only", p.Name)
		}
	}
	return nil
}

// validName reports whether a profile name is safe to use as a folder name.
func validName(n string) bool {
	if n == "" {
		return false
	}
	for _, r := range n {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// Select picks the profile to start: the -p flag if given, else the last
// used one if it still exists, else the first in the file.
func (c Config) Select(flag, last string) (Profile, error) {
	find := func(name string) (Profile, bool) {
		for _, p := range c.Profiles {
			if p.Name == name {
				return p, true
			}
		}
		return Profile{}, false
	}
	if flag != "" {
		if p, ok := find(flag); ok {
			return p, nil
		}
		var names []string
		for _, p := range c.Profiles {
			names = append(names, p.Name)
		}
		return Profile{}, fmt.Errorf("no profile %q; profiles: %s", flag, strings.Join(names, ", "))
	}
	if p, ok := find(last); ok {
		return p, nil
	}
	return c.Profiles[0], nil
}

// Use returns the config for a profile: the shared settings with the
// profile's overrides.
func (c Config) Use(p Profile) Config {
	if len(p.RepoRoots) > 0 {
		c.RepoRoots = p.RepoRoots
	}
	if p.ScratchRoot != "" {
		c.ScratchRoot = p.ScratchRoot
	}
	if p.PromptTemplate != "" {
		c.PromptTemplate = p.PromptTemplate
	}
	return c
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
