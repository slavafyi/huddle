// Package workspace finds local repos, names branches and prepares the
// folder an agent works in.
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/slavafyi/cuddle/internal/run"
)

// Kind is where an agent works.
type Kind int

// Workspace kinds, in the order the Start panel shows them.
const (
	Worktree Kind = iota // new git worktree on the task branch
	Checkout             // the repo folder as it is
	Scratch              // a plain folder, no git
)

func (k Kind) String() string {
	return [...]string{"Repo worktree", "Repo checkout", "Scratch"}[k]
}

// ID is the stable name stored in state files. Never change these.
func (k Kind) ID() string {
	return [...]string{"worktree", "checkout", "scratch"}[k]
}

// ParseKind reads an ID written by Kind.ID.
func ParseKind(id string) (Kind, bool) {
	for _, k := range []Kind{Worktree, Checkout, Scratch} {
		if k.ID() == id {
			return k, true
		}
	}
	return 0, false
}

// Repo is a local git repo.
type Repo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

const (
	slugWords = 5
	slugMax   = 50
)

var accents = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ä", "a", "ã", "a", "å", "a",
	"è", "e", "é", "e", "ê", "e", "ë", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i",
	"ò", "o", "ó", "o", "ô", "o", "ö", "o", "õ", "o", "ø", "o",
	"ù", "u", "ú", "u", "û", "u", "ü", "u",
	"ñ", "n", "ç", "c", "ß", "ss", "ý", "y", "ÿ", "y",
)

// Slug turns a title into lowercase ASCII words joined by "-": at most 5
// words and 50 characters. An empty result becomes "task".
func Slug(title string) string {
	s := accents.Replace(strings.ToLower(title))
	words := strings.FieldsFunc(s, func(r rune) bool {
		return r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r))
	})
	var out string
	for i, w := range words {
		if i == slugWords {
			break
		}
		next := w
		if out != "" {
			next = out + "-" + w
		}
		if len(next) > slugMax {
			if out == "" {
				out = w[:slugMax]
			}
			break
		}
		out = next
	}
	if out == "" {
		return "task"
	}
	return out
}

// Vars are the values a template can use.
type Vars struct {
	ID    string // task ID as shown, e.g. DEV-7700
	Title string
	URL   string
}

// Expand fills {id}, {id_lower}, {slug}, {title} and {url} in tmpl.
func Expand(tmpl string, v Vars) string {
	return strings.NewReplacer(
		"{id_lower}", strings.ToLower(v.ID),
		"{id}", v.ID,
		"{slug}", Slug(v.Title),
		"{title}", v.Title,
		"{url}", v.URL,
	).Replace(tmpl)
}

// ScanRepos lists folders directly under roots that contain .git, sorted by
// name. Missing roots are skipped.
func ScanRepos(roots []string) []Repo {
	var out []Repo
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(root, e.Name())
			if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
				out = append(out, Repo{Name: e.Name(), Path: p})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RankRepos orders repos for a client: recently used ones first (in the
// given order), then by how well the repo name matches the client name,
// then by name.
func RankRepos(repos []Repo, client string, recent []string) []Repo {
	rank := map[string]int{}
	for i, n := range recent {
		if _, ok := rank[n]; !ok {
			rank[n] = i
		}
	}
	words := strings.Fields(strings.ToLower(client))
	joined := strings.Join(words, "")
	score := func(name string) int {
		s := 0
		tokens := strings.Split(strings.ToLower(name), "-")
		for _, w := range words {
			for _, t := range tokens {
				if t == w {
					s += 2
				}
			}
		}
		if len(joined) >= 4 && strings.Contains(strings.ReplaceAll(strings.ToLower(name), "-", ""), joined) {
			s++
		}
		return s
	}
	out := append([]Repo(nil), repos...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, iok := rank[out[i].Name]
		rj, jok := rank[out[j].Name]
		switch {
		case iok && jok:
			return ri < rj
		case iok != jok:
			return iok
		}
		si, sj := score(out[i].Name), score(out[j].Name)
		if si != sj {
			return si > sj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// FilterRepos keeps repos whose name contains q (case-insensitive).
func FilterRepos(repos []Repo, q string) []Repo {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return repos
	}
	var out []Repo
	for _, r := range repos {
		if strings.Contains(strings.ToLower(r.Name), q) {
			out = append(out, r)
		}
	}
	return out
}

// Plan says which folder to prepare.
type Plan struct {
	Kind         Kind
	Repo         Repo
	Branch       string
	BaseBranch   string // "" means the remote's default branch
	TaskKey      string // lowercase task ID, used in folder names
	Slug         string
	WorktreeRoot string
	ScratchRoot  string
}

// Dir returns the folder Prepare will use, without touching anything.
func (p Plan) Dir() string {
	switch p.Kind {
	case Worktree:
		return filepath.Join(p.WorktreeRoot, p.Repo.Name, p.TaskKey)
	case Checkout:
		return p.Repo.Path
	}
	return filepath.Join(p.ScratchRoot, p.TaskKey+"-"+p.Slug)
}

// Prepare makes the folder ready and returns it. It reuses an existing
// worktree folder or branch and never overwrites anything. Checkout uses the
// repo as is and never switches branches.
func Prepare(ctx context.Context, r run.Runner, p Plan) (string, error) {
	dir := p.Dir()
	switch p.Kind {
	case Checkout:
		return dir, nil
	case Scratch:
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create scratch folder: %w", err)
		}
		return dir, nil
	}
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	git := func(args ...string) (string, error) {
		out, err := r.Run(ctx, run.Cmd{Name: "git", Args: append([]string{"-C", p.Repo.Path}, args...)})
		return strings.TrimSpace(string(out)), err
	}
	if _, err := git("fetch", "origin"); err != nil {
		return "", fmt.Errorf("fetch %s: %w", p.Repo.Name, err)
	}
	base := "origin/" + p.BaseBranch
	if p.BaseBranch == "" {
		ref, err := git("symbolic-ref", "--short", "refs/remotes/origin/HEAD")
		if err != nil || ref == "" {
			return "", fmt.Errorf("find default branch of %s (set repos.base_branch in config): %w", p.Repo.Name, err)
		}
		base = ref
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	if _, err := git("rev-parse", "--verify", "--quiet", "refs/heads/"+p.Branch); err == nil {
		_, err = git("worktree", "add", dir, p.Branch)
		if err != nil {
			return "", fmt.Errorf("add worktree: %w", err)
		}
		return dir, nil
	}
	// The branch was pushed before (another machine, or the local branch
	// was deleted): continue from the pushed work.
	if _, err := git("rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+p.Branch); err == nil {
		if _, err := git("worktree", "add", "--track", "-b", p.Branch, dir, "origin/"+p.Branch); err != nil {
			return "", fmt.Errorf("add worktree: %w", err)
		}
		return dir, nil
	}
	// --no-track: the new branch must not have the base branch as upstream,
	// or a plain `git push` could go to main.
	if _, err := git("worktree", "add", "--no-track", "-b", p.Branch, dir, base); err != nil {
		return "", fmt.Errorf("add worktree: %w", err)
	}
	return dir, nil
}
