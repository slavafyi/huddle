// Command cuddle is a terminal UI for ClickUp tasks, GitHub PRs and coding
// agents in Rex.
package main

import (
	"context"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/slavafyi/cuddle/internal/clickup"
	"github.com/slavafyi/cuddle/internal/config"
	"github.com/slavafyi/cuddle/internal/rex"
	"github.com/slavafyi/cuddle/internal/run"
	"github.com/slavafyi/cuddle/internal/store"
	"github.com/slavafyi/cuddle/internal/ui"
)

func main() {
	if err := runApp(); err != nil {
		fmt.Fprintln(os.Stderr, "cuddle:", err)
		os.Exit(1)
	}
}

func runApp() error {
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	runner := run.Exec{}
	token, err := config.Token(context.Background(), runner)
	if err != nil {
		return err
	}
	var cache store.Cache
	if err := store.Load(store.CachePath(), &cache); err != nil || cache.Version != store.CacheVersion {
		cache = store.Cache{} // unreadable or old cache: start fresh
	}
	var state store.State
	if err := store.Load(store.StatePath(), &state); err != nil {
		return fmt.Errorf("%w (fix or move the file; it links tasks to Rex tabs)", err)
	}
	deps := ui.Deps{
		Config: cfg,
		ClickUp: &clickup.HTTP{
			BaseURL: clickup.DefaultBaseURL, Token: token,
			TeamID: cfg.WorkspaceID, LeadFieldID: cfg.LeadFieldID,
		},
		Rex:       &rex.CLI{Runner: runner},
		Runner:    runner,
		Cache:     cache,
		State:     state,
		CachePath: store.CachePath(),
		StatePath: store.StatePath(),
		Watch:     rex.Watch,
	}
	_, err = tea.NewProgram(ui.NewApp(deps)).Run()
	return err
}
