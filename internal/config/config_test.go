package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/slavafyi/huddle/internal/run"
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
	if c.WorktreeRoot != "/home/u/.local/share/huddle/worktrees" {
		t.Errorf("worktree root = %q", c.WorktreeRoot)
	}
	if got := c.AgentNames(); len(got) != 3 || got[0] != "claude" || got[2] != "shell" {
		t.Errorf("agents = %v", got)
	}
}

func TestLoadFileOverridesAndExpandsHome(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	c, err := Load(write(t, `
repo_roots = ["~/code"]
scratch_root = "~/r"

[agents]
claude = ["claude", "--model", "x", "{prompt}"]

[repos.base_branch]
shop = "develop"

[work]
clickup = true
clickup_workspace = "42"
clickup_lead = "f-1"
github_account = "me"
github_orgs = ["acme"]
`))
	if err != nil {
		t.Fatal(err)
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
	want := []Profile{{Name: "work", ClickUp: true, ClickUpWorkspace: "42", ClickUpLead: "f-1", GitHubAccount: "me", GitHubOrgs: []string{"acme"}}}
	if !reflect.DeepEqual(c.Profiles, want) {
		t.Errorf("profiles = %+v", c.Profiles)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProfilesKeepFileOrder(t *testing.T) {
	c, err := Load(write(t, "[zeta]\n[agents]\nx = [\"x\"]\n[alpha]\ngithub_account = \"a\"\n[mid]\n"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range c.Profiles {
		names = append(names, p.Name)
	}
	if !reflect.DeepEqual(names, []string{"zeta", "alpha", "mid"}) {
		t.Errorf("names = %v", names)
	}
}

func TestLoadRejectsUnknownSettings(t *testing.T) {
	for body, want := range map[string]string{
		"workspace_id = \"1\"\n[work]\n": "workspace_id",
		"[work]\nclickup_team = \"1\"\n": "work.clickup_team",
	} {
		_, err := Load(write(t, body))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want mention of %s", body, err, want)
		}
	}
}

func TestValidate(t *testing.T) {
	for body, want := range map[string]string{
		"repo_roots = []\n":        "no profiles",
		"[work]\nclickup = true\n": "set clickup_workspace and clickup_lead in [work]",
		"[\"a/b\"]\n":              "profile name",
		"[ok]\n[work]\nclickup = true\nclickup_workspace = \"1\"\n": "set clickup_lead in [work]",
	} {
		c, err := Load(write(t, body))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", body, err, want)
		}
	}
}

func TestSelectProfile(t *testing.T) {
	c := Config{Profiles: []Profile{{Name: "work"}, {Name: "personal"}}}
	for _, tc := range []struct{ flag, last, want string }{
		{"", "", "work"},             // first in the file
		{"", "personal", "personal"}, // last used
		{"", "gone", "work"},         // last used no longer exists
		{"work", "personal", "work"}, // -p wins
	} {
		p, err := c.Select(tc.flag, tc.last)
		if err != nil || p.Name != tc.want {
			t.Errorf("Select(%q, %q) = %q, %v; want %q", tc.flag, tc.last, p.Name, err, tc.want)
		}
	}
	_, err := c.Select("nope", "work")
	if err == nil || !strings.Contains(err.Error(), `no profile "nope"`) || !strings.Contains(err.Error(), "work, personal") {
		t.Errorf("err = %v", err)
	}
}

func TestUseAppliesProfileOverrides(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	c, err := Load(write(t, `
repo_roots = ["~/a"]
scratch_root = "~/s"
prompt_template = "shared {id}"

[work]
[personal]
repo_roots = ["~/oss"]
scratch_root = "~/tmp"
prompt_template = "Take issue {id}"
`))
	if err != nil {
		t.Fatal(err)
	}
	w := c.Use(c.Profiles[0])
	if w.RepoRoots[0] != "/home/u/a" || w.ScratchRoot != "/home/u/s" || w.PromptTemplate != "shared {id}" {
		t.Errorf("work = %+v", w)
	}
	p := c.Use(c.Profiles[1])
	if p.RepoRoots[0] != "/home/u/oss" || p.ScratchRoot != "/home/u/tmp" || p.PromptTemplate != "Take issue {id}" {
		t.Errorf("personal = %+v", p)
	}
	if c.RepoRoots[0] != "/home/u/a" {
		t.Errorf("Use changed the shared config: %v", c.RepoRoots)
	}
}

func TestProfileSummary(t *testing.T) {
	for p, want := range map[*Profile]string{
		{ClickUp: true, GitHubOrgs: []string{"vertexdigital"}}: "ClickUp · vertexdigital",
		{GitHubAccount: "slavafyi"}:                            "GitHub · slavafyi",
		{}:                                                     "GitHub",
	} {
		if got := p.Summary(); got != want {
			t.Errorf("summary = %q, want %q", got, want)
		}
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
	f := &run.Fake{Responses: []run.Response{{Match: "security find-generic-password -s huddle-clickup -w", Out: "pk_kc\n"}}}
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
