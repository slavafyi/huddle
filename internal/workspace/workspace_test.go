package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/slavafyi/cuddle/internal/run"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Promo: Checkout personalisation":                                       "promo-checkout-personalisation",
		"Add analytics consent tag for pinecone-shop.test":                      "add-analytics-consent-tag-for",
		"Fix  blank nav — collections tabs!!":                                   "fix-blank-nav-collections-tabs",
		"Kalévo: Issue switching language":                                      "kalevo-issue-switching-language",
		"🚀 NEW LAUNCH Paper Planes":                                             "new-launch-paper-planes",
		"Supercalifragilisticexpialidocious antidisestablishmentarianism words": "supercalifragilisticexpialidocious",
		"":   "task",
		"日本": "task",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
		if len(Slug(in)) > 50 {
			t.Errorf("Slug(%q) too long", in)
		}
	}
}

func TestExpand(t *testing.T) {
	v := Vars{ID: "DEV-7700", Title: "Promo: Checkout personalisation", URL: "https://x"}
	if got := Expand("feature/{id_lower}-{slug}", v); got != "feature/dev-7700-promo-checkout-personalisation" {
		t.Errorf("branch = %q", got)
	}
	if got := Expand("Take a task {id} from CU", v); got != "Take a task DEV-7700 from CU" {
		t.Errorf("prompt = %q", got)
	}
	// A different prefix works the same way: nothing assumes "DEV".
	if got := Expand("feature/{id_lower}-{slug}", Vars{ID: "86c1x2abc", Title: "Hi"}); got != "feature/86c1x2abc-hi" {
		t.Errorf("no custom id: %q", got)
	}
}

func TestScanRepos(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"snowcap/.git", "snowcap-subscriptions/.git", "notes", "kalevo-theme"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	os.WriteFile(filepath.Join(root, "kalevo-theme", ".git"), []byte("gitdir: x"), 0o644) // worktree-style .git file
	got := ScanRepos([]string{root, filepath.Join(root, "missing")})
	var names []string
	for _, r := range got {
		names = append(names, r.Name)
	}
	want := []string{"kalevo-theme", "snowcap", "snowcap-subscriptions"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("repos = %v, want %v", names, want)
	}
}

func TestRankRepos(t *testing.T) {
	repos := []Repo{{Name: "dawn"}, {Name: "snowcap"}, {Name: "snowcap-uk-theme"}, {Name: "snowcap-subscriptions"}, {Name: "daisylaneshop-delivery"}, {Name: "harbor-and-goods"}}
	names := func(rs []Repo) []string {
		var out []string
		for _, r := range rs {
			out = append(out, r.Name)
		}
		return out
	}
	got := names(RankRepos(repos, "Snowcap UK", nil))
	if got[0] != "snowcap-uk-theme" {
		t.Errorf("Snowcap UK: %v", got)
	}
	got = names(RankRepos(repos, "Snowcap", []string{"snowcap-subscriptions"}))
	if got[0] != "snowcap-subscriptions" || got[1] != "snowcap" {
		t.Errorf("recent first: %v", got)
	}
	got = names(RankRepos(repos, "Daisy Lane Shop", nil))
	if got[0] != "daisylaneshop-delivery" {
		t.Errorf("joined client name: %v", got)
	}
	if len(RankRepos(repos, "Harbor and Goods", nil)) != len(repos) {
		t.Error("ranking must keep all repos")
	}
}

func TestFilterRepos(t *testing.T) {
	repos := []Repo{{Name: "snowcap"}, {Name: "kalevo-theme"}, {Name: "snowcap-subscriptions"}}
	got := FilterRepos(repos, "subs")
	if len(got) != 1 || got[0].Name != "snowcap-subscriptions" {
		t.Errorf("got %v", got)
	}
}

func TestPrepareWorktreeNewBranch(t *testing.T) {
	root := t.TempDir()
	f := &run.Fake{Responses: []run.Response{
		{Match: "git -C /r/snowcap rev-parse", Err: errors.New("exit status 1")},
		{Match: "git -C /r/snowcap symbolic-ref", Out: "origin/main\n"},
	}}
	p := Plan{Kind: Worktree, Repo: Repo{Name: "snowcap", Path: "/r/snowcap"}, Branch: "feature/dev-1-x", TaskKey: "dev-1", WorktreeRoot: root}
	dir, err := Prepare(context.Background(), f, p)
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(root, "snowcap", "dev-1")
	if dir != wantDir {
		t.Errorf("dir = %q", dir)
	}
	want := []string{
		"git -C /r/snowcap fetch origin",
		"git -C /r/snowcap symbolic-ref --short refs/remotes/origin/HEAD",
		"git -C /r/snowcap rev-parse --verify --quiet refs/heads/feature/dev-1-x",
		"git -C /r/snowcap rev-parse --verify --quiet refs/remotes/origin/feature/dev-1-x",
		"git -C /r/snowcap worktree add --no-track -b feature/dev-1-x " + wantDir + " origin/main",
	}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestPrepareWorktreeExistingBranchAndBaseOverride(t *testing.T) {
	root := t.TempDir()
	f := &run.Fake{Responses: []run.Response{{Match: "git -C /r/s rev-parse", Out: "abc\n"}}}
	p := Plan{Kind: Worktree, Repo: Repo{Name: "s", Path: "/r/s"}, Branch: "feature/dev-1-x", TaskKey: "dev-1", WorktreeRoot: root, BaseBranch: "develop"}
	if _, err := Prepare(context.Background(), f, p); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if last := calls[len(calls)-1]; last != "git -C /r/s worktree add "+filepath.Join(root, "s", "dev-1")+" feature/dev-1-x" {
		t.Errorf("reuse branch: %s", last)
	}
	for _, c := range calls {
		if strings.Contains(c, "symbolic-ref") {
			t.Error("base override should skip origin/HEAD lookup")
		}
	}
}

func TestPrepareWorktreeFromRemoteBranch(t *testing.T) {
	root := t.TempDir()
	f := &run.Fake{Responses: []run.Response{
		{Match: "git -C /r/s rev-parse --verify --quiet refs/heads/", Err: errors.New("exit status 1")},
		{Match: "git -C /r/s rev-parse --verify --quiet refs/remotes/origin/feature/dev-1-x", Out: "abc\n"},
		{Match: "git -C /r/s symbolic-ref", Out: "origin/main\n"},
	}}
	p := Plan{Kind: Worktree, Repo: Repo{Name: "s", Path: "/r/s"}, Branch: "feature/dev-1-x", TaskKey: "dev-1", WorktreeRoot: root}
	if _, err := Prepare(context.Background(), f, p); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	want := "git -C /r/s worktree add --track -b feature/dev-1-x " + filepath.Join(root, "s", "dev-1") + " origin/feature/dev-1-x"
	if last := calls[len(calls)-1]; last != want {
		t.Errorf("remote branch: %s", last)
	}
}

func TestKindIDIsStable(t *testing.T) {
	for _, k := range []Kind{Worktree, Checkout, Scratch} {
		if got, ok := ParseKind(k.ID()); !ok || got != k {
			t.Errorf("round trip %v", k)
		}
	}
	if Worktree.ID() != "worktree" || Scratch.ID() != "scratch" {
		t.Error("IDs are stored in state.json and must not change")
	}
}

func TestPrepareWorktreeReusesFolder(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "s", "dev-1"), 0o755)
	f := &run.Fake{}
	p := Plan{Kind: Worktree, Repo: Repo{Name: "s", Path: "/r/s"}, Branch: "b", TaskKey: "dev-1", WorktreeRoot: root}
	dir, err := Prepare(context.Background(), f, p)
	if err != nil || dir != filepath.Join(root, "s", "dev-1") {
		t.Fatalf("dir=%q err=%v", dir, err)
	}
	if len(f.Calls()) != 0 {
		t.Errorf("existing folder must not run git: %v", f.Calls())
	}
}

func TestPrepareStopsOnFetchError(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "git -C /r/s fetch", Err: errors.New("offline")}}}
	p := Plan{Kind: Worktree, Repo: Repo{Name: "s", Path: "/r/s"}, Branch: "b", TaskKey: "dev-1", WorktreeRoot: t.TempDir()}
	if _, err := Prepare(context.Background(), f, p); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("err = %v", err)
	}
	if len(f.Calls()) != 1 {
		t.Errorf("should stop after fetch: %v", f.Calls())
	}
}

func TestPrepareCheckoutAndScratch(t *testing.T) {
	f := &run.Fake{}
	dir, err := Prepare(context.Background(), f, Plan{Kind: Checkout, Repo: Repo{Name: "s", Path: "/r/s"}})
	if err != nil || dir != "/r/s" || len(f.Calls()) != 0 {
		t.Errorf("checkout: dir=%q err=%v calls=%v", dir, err, f.Calls())
	}
	root := t.TempDir()
	dir, err = Prepare(context.Background(), f, Plan{Kind: Scratch, ScratchRoot: root, TaskKey: "dev-2", Slug: "research-x"})
	if err != nil || dir != filepath.Join(root, "dev-2-research-x") {
		t.Fatalf("scratch: dir=%q err=%v", dir, err)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Error("scratch folder not created")
	}
}
