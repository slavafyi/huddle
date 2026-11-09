// Package run runs external commands. Tests use Fake instead of Exec.
package run

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Cmd is one external command, given as an argument list (never a shell
// string).
type Cmd struct {
	Name string
	Args []string
	Dir  string
	Env  []string // extra KEY=VALUE pairs on top of huddle's environment
}

// String joins the name and arguments with spaces. It is for matching in
// tests and for error messages, never for running.
func (c Cmd) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

// Runner runs a command and returns its standard output.
type Runner interface {
	Run(ctx context.Context, c Cmd) ([]byte, error)
}

// Exec runs commands with os/exec.
type Exec struct{}

// Run runs c and returns its standard output. On failure the error includes
// the command's standard error.
func (Exec) Run(ctx context.Context, c Cmd) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return out, fmt.Errorf("%s: %w", c.Name, err)
		}
		return out, fmt.Errorf("%s: %w: %s", c.Name, err, msg)
	}
	return out, nil
}

// Response is a canned result for Fake. Match is compared with the start of
// Cmd.String().
type Response struct {
	Match string
	Out   string
	Err   error
}

// Fake records commands and answers with the first matching Response.
// Commands without a match succeed with empty output.
type Fake struct {
	mu        sync.Mutex
	Responses []Response
	calls     []Cmd
}

// Run records c and returns the first matching response.
func (f *Fake) Run(_ context.Context, c Cmd) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	s := c.String()
	for _, r := range f.Responses {
		if strings.HasPrefix(s, r.Match) {
			return []byte(r.Out), r.Err
		}
	}
	return nil, nil
}

// Calls returns the recorded commands as strings, in order.
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.String()
	}
	return out
}

// CallsWithDir returns the recorded commands.
func (f *Fake) CallsWithDir() []Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Cmd(nil), f.calls...)
}
