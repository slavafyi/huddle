// Package rex controls Rex sessions, windows (tabs) and blocks (panes)
// through the rex CLI.
package rex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/slavafyi/cuddle/internal/run"
)

// Placement is where a new tab opened.
type Placement struct {
	SessionID string `json:"session_id"`
	WindowID  string `json:"window_id"`
	BlockID   string `json:"block_id"`
}

// Status is a program's reported state (Rex program status, OSC 7501):
// idle, working, done, blocked, error, or "" when nothing was reported.
type Status struct {
	State string
	Kind  string
	Msg   string
}

// ErrRemoteControl means the Rex app refused a client action, usually
// because Remote Control is off.
var ErrRemoteControl = errors.New("turn on Remote Control in Rex settings to jump to tabs")

// Client is what cuddle needs from Rex.
type Client interface {
	Sessions(ctx context.Context) (map[string]string, error)
	NewSession(ctx context.Context, name, dir, window string, argv []string) (Placement, error)
	NewWindow(ctx context.Context, session, name, dir string, argv []string) (Placement, error)
	Blocks(ctx context.Context, session string) (map[string]bool, error)
	Status(ctx context.Context, session, block string) (Status, error)
	Focus(ctx context.Context, session, window string) error
	CloseWindow(ctx context.Context, session, window string) error
	LastLine(ctx context.Context, session, block string) (string, error)
}

// CLI implements Client with the rex command.
type CLI struct {
	Runner run.Runner
}

func (c *CLI) rex(ctx context.Context, out any, args ...string) error {
	data, err := c.Runner.Run(ctx, run.Cmd{Name: "rex", Args: args})
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("rex %s: decode: %w", args[0], err)
	}
	return nil
}

func withCommand(args, argv []string) []string {
	if len(argv) == 0 {
		return args
	}
	return append(append(args, "--"), argv...)
}

// Sessions returns session IDs and their labels.
func (c *CLI) Sessions(ctx context.Context) (map[string]string, error) {
	var r struct {
		Sessions []struct {
			ID    string  `json:"session_id"`
			Label *string `json:"label"`
		} `json:"sessions"`
	}
	if err := c.rex(ctx, &r, "ls", "--json"); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, s := range r.Sessions {
		label := ""
		if s.Label != nil {
			label = *s.Label
		}
		out[s.ID] = label
	}
	return out, nil
}

// NewSession creates a session named name whose first tab runs argv (a
// shell when argv is empty) in dir.
func (c *CLI) NewSession(ctx context.Context, name, dir, window string, argv []string) (Placement, error) {
	var r struct {
		SessionID string `json:"session_id"`
		Windows   []struct {
			WindowID string   `json:"window_id"`
			Blocks   []string `json:"block_ids"`
		} `json:"initial_windows"`
	}
	args := withCommand([]string{"new", name, "-c", dir, "--window", window, "--json"}, argv)
	if err := c.rex(ctx, &r, args...); err != nil {
		return Placement{}, err
	}
	if len(r.Windows) == 0 || len(r.Windows[0].Blocks) == 0 || r.Windows[0].WindowID == "" {
		return Placement{}, errors.New("rex new: no window in reply")
	}
	return Placement{SessionID: r.SessionID, WindowID: r.Windows[0].WindowID, BlockID: r.Windows[0].Blocks[0]}, nil
}

// NewWindow opens a tab in session without taking focus.
func (c *CLI) NewWindow(ctx context.Context, session, name, dir string, argv []string) (Placement, error) {
	var r struct {
		WindowID string   `json:"window_id"`
		Blocks   []string `json:"block_ids"`
	}
	args := withCommand([]string{"window", "new", "-s", session, name, "-c", dir, "--focus=false", "--json"}, argv)
	if err := c.rex(ctx, &r, args...); err != nil {
		return Placement{}, err
	}
	if len(r.Blocks) == 0 || r.WindowID == "" {
		return Placement{}, errors.New("rex window new: no window or block in reply")
	}
	return Placement{SessionID: session, WindowID: r.WindowID, BlockID: r.Blocks[0]}, nil
}

// Blocks returns the IDs of blocks in session. A session that no longer
// exists has no blocks; any other failure is returned, so callers don't
// mistake a slow Rex for closed tabs.
func (c *CLI) Blocks(ctx context.Context, session string) (map[string]bool, error) {
	var r struct {
		Blocks []struct {
			ID string `json:"block_id"`
		} `json:"blocks"`
	}
	out := map[string]bool{}
	if err := c.rex(ctx, &r, "block", "ls", "-s", session, "--json"); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return out, nil
		}
		return nil, err
	}
	for _, b := range r.Blocks {
		out[b.ID] = true
	}
	return out, nil
}

type record struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Kind  string `json:"kind"`
	Msg   string `json:"msg"`
}

// Status returns the block's root program status record.
func (c *CLI) Status(ctx context.Context, session, block string) (Status, error) {
	var r struct {
		Records []record `json:"records"`
	}
	if err := c.rex(ctx, &r, "block", "call", "-s", session, "-b", block, "program_status", "{}"); err != nil {
		return Status{}, err
	}
	for _, rec := range r.Records {
		if rec.ID == "" {
			return Status{State: rec.State, Kind: rec.Kind, Msg: rec.Msg}, nil
		}
	}
	return Status{}, nil
}

// Focus shows the tab in the Rex app. It needs Remote Control; without it,
// Focus only makes the tab active in its session and returns
// ErrRemoteControl.
func (c *CLI) Focus(ctx context.Context, session, window string) error {
	var r struct {
		Clients []struct {
			ID   string `json:"client_id"`
			Info struct {
				Kind string `json:"kind"`
			} `json:"info"`
		} `json:"clients"`
	}
	client := ""
	if err := c.rex(ctx, &r, "client", "ls", "--json"); err == nil {
		for _, cl := range r.Clients {
			if cl.Info.Kind == "app" {
				client = cl.ID
				break
			}
		}
	}
	if client != "" {
		err := c.rex(ctx, nil, "-C", client, "do", "session.select", "session_id="+session, "window_id="+window)
		if err == nil {
			return nil
		}
	}
	if err := c.rex(ctx, nil, "window", "focus", "-s", session, window); err != nil {
		return err
	}
	return ErrRemoteControl
}

// CloseWindow closes a tab.
func (c *CLI) CloseWindow(ctx context.Context, session, window string) error {
	return c.rex(ctx, nil, "window", "close", "-s", session, window)
}

// LastLine returns the last non-empty line on the block's screen.
func (c *CLI) LastLine(ctx context.Context, session, block string) (string, error) {
	out, err := c.Runner.Run(ctx, run.Cmd{Name: "rex", Args: []string{"capture", "-s", session, "-b", block, "--trim"}})
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l, nil
		}
	}
	return "", nil
}

// Event is a program status change or a closed block.
type Event struct {
	SessionID string
	BlockID   string
	HasStatus bool
	Status    Status
	Closed    bool
}

// ParseEvent reads one line of `rex events --all --json`. It reports only
// root-record status changes and closed blocks.
func ParseEvent(line []byte) (Event, bool) {
	var e struct {
		Type    string `json:"type"`
		Payload struct {
			SessionID string `json:"session_id"`
			Event     struct {
				Type    string `json:"type"`
				Payload struct {
					BlockID string `json:"block_id"`
					Name    string `json:"name"`
					Payload struct {
						ID     string  `json:"id"`
						Record *record `json:"record"`
					} `json:"payload"`
				} `json:"payload"`
			} `json:"event"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &e) != nil || e.Type != "session_event" {
		return Event{}, false
	}
	inner := e.Payload.Event
	ev := Event{SessionID: e.Payload.SessionID, BlockID: inner.Payload.BlockID}
	switch {
	case inner.Type == "block_closed":
		ev.Closed = true
		return ev, true
	case inner.Type == "block_event" && inner.Payload.Name == "program_status_changed":
		p := inner.Payload.Payload
		if p.ID != "" {
			return Event{}, false
		}
		ev.HasStatus = true
		if p.Record != nil {
			ev.Status = Status{State: p.Record.State, Kind: p.Record.Kind, Msg: p.Record.Msg}
		}
		return ev, true
	}
	return Event{}, false
}

// Watch streams events from `rex events --all --json` until ctx ends. The
// channel closes when the stream stops, and the rex process is always
// stopped and reaped.
func Watch(parent context.Context) (<-chan Event, error) {
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, "rex", "events", "--all", "--json")
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		defer cmd.Wait() //nolint:errcheck // runs after cancel, exit status is expected
		defer cancel()
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			if ev, ok := ParseEvent(sc.Bytes()); ok {
				select {
				case ch <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}
