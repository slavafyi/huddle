package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/slavafyi/cuddle/internal/run"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.BranchTemplate != "feature/{id_lower}-{slug}" {
		t.Errorf("branch template = %q", c.BranchTemplate)
	}
	if c.PromptTemplate != "Take a task {id} from CU" {
		t.Errorf("prompt template = %q", c.PromptTemplate)
	}
	if c.WorktreeRoot != "/home/u/.local/share/cuddle/worktrees" {
		t.Errorf("worktree root = %q", c.WorktreeRoot)
	}
	if got := c.AgentNames(); len(got) != 3 || got[0] != "claude" || got[2] != "shell" {
		t.Errorf("agents = %v", got)
	}
}

func TestLoadFileOverridesAndExpandsHome(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	p := filepath.Join(t.TempDir(), "c.toml")
	err := os.WriteFile(p, []byte(`
workspace_id = "42"
lead_field_id = "f-1"
repo_roots = ["~/code"]
scratch_root = "~/r"
github_org = "acme"

[agents]
claude = ["claude", "--model", "x", "{prompt}"]

[repos.base_branch]
shop = "develop"
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.WorkspaceID != "42" || c.LeadFieldID != "f-1" || c.GitHubOrg != "acme" {
		t.Errorf("ids not loaded: %+v", c)
	}
	if c.RepoRoots[0] != "/home/u/code" || c.ScratchRoot != "/home/u/r" {
		t.Errorf("home not expanded: %v %q", c.RepoRoots, c.ScratchRoot)
	}
	if c.Agents["claude"][2] != "x" || len(c.Agents["codex"]) == 0 {
		t.Errorf("agents merge: %v", c.Agents)
	}
	if c.BaseBranch("shop") != "develop" || c.BaseBranch("other") != "" {
		t.Errorf("base branch override wrong")
	}
	if err := c.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

func TestValidateNeedsWorkspaceAndLeadField(t *testing.T) {
	c := Default()
	if err := c.Validate(); err == nil {
		t.Fatal("want error for empty workspace_id")
	}
}

func TestTokenFromEnv(t *testing.T) {
	t.Setenv("CLICKUP_API_TOKEN", "pk_env")
	f := &run.Fake{}
	tok, err := Token(context.Background(), f)
	if err != nil || tok != "pk_env" {
		t.Fatalf("got %q, %v", tok, err)
	}
	if len(f.Calls()) != 0 {
		t.Errorf("keychain should not be called")
	}
}

func TestTokenFromKeychain(t *testing.T) {
	t.Setenv("CLICKUP_API_TOKEN", "")
	f := &run.Fake{Responses: []run.Response{{Match: "security find-generic-password -s cuddle-clickup -w", Out: "pk_kc\n"}}}
	tok, err := Token(context.Background(), f)
	if err != nil || tok != "pk_kc" {
		t.Fatalf("got %q, %v", tok, err)
	}
}

func TestTokenMissing(t *testing.T) {
	t.Setenv("CLICKUP_API_TOKEN", "")
	f := &run.Fake{Responses: []run.Response{{Match: "security", Err: errors.New("not found")}}}
	if _, err := Token(context.Background(), f); err == nil {
		t.Fatal("want error")
	}
}
