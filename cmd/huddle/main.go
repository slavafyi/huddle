// Command huddle is a terminal UI for ClickUp tasks, GitHub issues and PRs,
// and coding agents in Rex.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/slavafyi/huddle/internal/clickup"
	"github.com/slavafyi/huddle/internal/config"
	"github.com/slavafyi/huddle/internal/github"
	"github.com/slavafyi/huddle/internal/rex"
	"github.com/slavafyi/huddle/internal/run"
	"github.com/slavafyi/huddle/internal/store"
	"github.com/slavafyi/huddle/internal/task"
	"github.com/slavafyi/huddle/internal/ui"
)

// version is set at release build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	profile := flag.String("p", "", "start in this profile (default: the last used one)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("huddle", version)
		return
	}
	if err := runApp(*profile); err != nil {
		fmt.Fprintln(os.Stderr, "huddle:", err)
		os.Exit(1)
	}
}

func runApp(flagProfile string) error {
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	p, err := cfg.Select(flagProfile, store.LastProfile(store.StateDir()))
	if err != nil {
		return err
	}
	// The token is read from the environment once, then removed from it:
	// child processes (rex, gh, git, and through rex the agent tabs) must
	// not inherit it. Without it, config.Token falls back to the keychain.
	token := strings.TrimSpace(os.Getenv("CLICKUP_API_TOKEN"))
	os.Unsetenv("CLICKUP_API_TOKEN")
	runner := run.Exec{}
	for {
		if p.ClickUp && token == "" {
			if token, err = config.Token(context.Background(), runner); err != nil {
				return err
			}
		}
		next, err := runProfile(cfg, p, token, runner)
		if err != nil || next == "" {
			return err
		}
		if p, err = cfg.Select(next, ""); err != nil {
			return err
		}
	}
}

// runProfile runs the app in one profile until it quits. It returns the
// profile to switch to, or "" when the user quit.
func runProfile(cfg config.Config, p config.Profile, token string, runner run.Runner) (string, error) {
	if err := store.RememberProfile(store.StateDir(), p.Name); err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // stops the rex event stream when the app ends
	var cache store.Cache
	if err := store.Load(store.CachePath(p.Name), &cache); err != nil || cache.Version != store.CacheVersion {
		cache = store.Cache{} // unreadable or old cache: start fresh
	}
	var state store.State
	if err := store.Load(store.StatePath(p.Name), &state); err != nil {
		return "", fmt.Errorf("%w (fix or move the file; it links tasks to Rex tabs)", err)
	}
	state.Migrate()
	pcfg := cfg.Use(p)
	gh := &github.GH{Runner: runner, Orgs: p.GitHubOrgs, Exclude: cfg.OtherOrgs(p)}
	if p.GitHubAccount != "" {
		token, err := github.Token(ctx, runner, p.GitHubAccount)
		if err != nil {
			return "", err
		}
		gh.Token = token
	}
	var sources []task.Source
	if p.ClickUp {
		// Detected workspace and lead field, kept next to the cache.
		path := filepath.Join(store.CacheDir(), p.Name, "clickup.json")
		h := &clickup.HTTP{BaseURL: clickup.DefaultBaseURL, Token: token}
		d := &clickup.Detector{
			HTTP: h, Want: clickup.IDs{Workspace: p.ClickUpWorkspace, Lead: p.ClickUpLead},
			Save: func(ids clickup.IDs) error { return store.Save(path, ids) },
		}
		if store.Load(path, &d.Saved) != nil {
			d.Saved = clickup.IDs{} // unreadable: detect again
		}
		note, err := d.Setup(ctx)
		if err != nil {
			return "", err
		}
		src := &clickup.Source{Client: h, InProgress: pcfg.StatusInProgress, NoLead: h.LeadFieldID == "", Detector: d}
		src.SetNote(note, false)
		sources = append(sources, src)
	}
	if p.IssuesOn() {
		sources = append(sources, &github.Issues{GH: gh})
	}
	app := ui.NewApp(ui.Deps{
		Config:    pcfg,
		Profile:   p.Name,
		Profiles:  cfg.Profiles,
		Recent:    store.RecentProfiles(store.StateDir()),
		Sources:   sources,
		GitHub:    gh,
		Rex:       &rex.CLI{Runner: runner},
		Runner:    runner,
		Cache:     cache,
		State:     state,
		CachePath: store.CachePath(p.Name),
		StatePath: store.StatePath(p.Name),
		Watch:     func(context.Context) (<-chan rex.Event, error) { return rex.Watch(ctx) },

		HomeSession: os.Getenv("REX_SESSION"),
	})
	if _, err := tea.NewProgram(app).Run(); err != nil {
		return "", err
	}
	return app.SwitchTo(), nil
}
