// Command cuddle is a terminal UI for ClickUp tasks, GitHub PRs and coding
// agents in Rex.
package main

import (
	"context"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/slavafyi/cuddle/internal/config"
	"github.com/slavafyi/cuddle/internal/run"
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
	if _, err := config.Token(context.Background(), run.Exec{}); err != nil {
		return err
	}
	_, err = tea.NewProgram(ui.NewApp(ui.Deps{})).Run()
	return err
}
