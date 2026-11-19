// Package resume knows how each agent CLI names its sessions, so a closed
// agent tab can be opened again where it left off. Where the CLI allows it,
// huddle picks the session ID at launch, in the agent's own format; where it
// doesn't, the ID is found in the agent's files later.
package resume

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/slavafyi/huddle/internal/run"
)

// Agent is the adapter for one agent CLI.
type Agent struct {
	// newID makes a session ID for the CLI to use; nil when huddle can't
	// pick it.
	newID func() string
	// create asks the CLI for a new session ID before launch.
	create bool
	// idFlag passes the ID at launch.
	idFlag string
	// chosen are flags that mean the command already picks a session.
	chosen []string
	// find looks up the ID the CLI picked itself; nil when huddle picks it.
	find func(in func(path string) bool, since time.Time) (string, error)
	// command reopens a session.
	command func(base []string, id string) []string
}

// after puts args right after the command name.
func after(args ...string) func([]string, string) []string {
	return func(base []string, id string) []string {
		out := append([]string{base[0]}, args...)
		out = append(out, id)
		return append(out, base[1:]...)
	}
}

var agents = map[string]*Agent{
	"claude": {
		newID: uuidV4, idFlag: "--session-id",
		chosen:  []string{"--session-id", "--resume", "-r", "--continue", "-c"},
		command: after("--resume"),
	},
	"codex": {
		find: findCodex,
		command: func(base []string, id string) []string {
			out := append([]string{base[0], "resume"}, base[1:]...)
			return append(out, id)
		},
	},
	"pi": {
		newID: uuidV7, idFlag: "--session-id",
		chosen:  []string{"--session-id", "--session", "--resume", "-r", "--continue", "-c", "--fork"},
		command: after("--session"),
	},
	"opencode": {
		newID: func() string { return opencodeID(time.Now()) }, idFlag: "--session",
		chosen:  []string{"--session", "-s", "--continue", "-c"},
		command: after("--session"),
	},
	"cursor-agent": {
		create: true, idFlag: "--resume",
		chosen:  []string{"--resume", "--continue"},
		command: after("--resume"),
	},
	"agy": {
		find:    findAgy,
		command: after("--conversation"),
	},
}

// For returns the adapter for a command, by its program name, or nil when
// huddle can't reopen its sessions.
func For(argv []string) *Agent {
	if len(argv) == 0 {
		return nil
	}
	return agents[filepath.Base(argv[0])]
}

// Start returns the launch command with a session ID set, and that ID. The
// ID is "" when the agent picks its own or the command already names a
// session. On error argv is returned unchanged.
func (a *Agent) Start(ctx context.Context, r run.Runner, dir string, argv []string) ([]string, string, error) {
	if (a.newID == nil && !a.create) || a.picksSession(argv) {
		return argv, "", nil
	}
	var id string
	if a.create {
		var err error
		if id, err = createChat(ctx, r, dir, argv[0]); err != nil {
			return argv, "", err
		}
	} else {
		id = a.newID()
	}
	out := append([]string{argv[0], a.idFlag, id}, argv[1:]...)
	return out, id, nil
}

func (a *Agent) picksSession(argv []string) bool {
	for _, arg := range argv[1:] {
		name, _, _ := strings.Cut(arg, "=")
		if slices.Contains(a.chosen, name) {
			return true
		}
	}
	return false
}

// Find returns the ID of the session the agent started in dir at or after
// started, or "" when there is none. Agents whose ID huddle picks have
// nothing to find.
func (a *Agent) Find(dir string, started time.Time) (string, error) {
	if a.find == nil {
		return "", nil
	}
	dir = filepath.Clean(dir)
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		real = dir
	}
	// Agents save the resolved path, which can differ from huddle's.
	in := func(p string) bool {
		p = filepath.Clean(p)
		return p == dir || p == real
	}
	return a.find(in, started.Add(-slack))
}

// slack allows for clock steps between huddle and the agent writing its
// start time.
const slack = 5 * time.Second

// Command returns the command that reopens session id. base is the launch
// command without the prompt.
func (a *Agent) Command(base []string, id string) []string {
	return a.command(base, id)
}

// promptFlags take the prompt as their value.
var promptFlags = []string{"--prompt", "-i", "--prompt-interactive"}

// Base returns an agent command template without the prompt, so it can be
// reused to reopen a session. It is nil for no template.
func Base(template []string) []string {
	if len(template) == 0 {
		return nil
	}
	var out []string
	for _, arg := range template {
		if !strings.Contains(arg, "{prompt}") {
			out = append(out, arg)
			continue
		}
		if n := len(out); n > 1 && slices.Contains(promptFlags, out[n-1]) {
			out = out[:n-1]
		}
	}
	return out
}

var chatID = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z_-]*$`)

// createChat runs `<bin> create-chat`, which prints a new chat ID.
func createChat(ctx context.Context, r run.Runner, dir, bin string) (string, error) {
	out, err := r.Run(ctx, run.Cmd{Name: bin, Args: []string{"create-chat"}, Dir: dir})
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	id := strings.TrimSpace(lines[len(lines)-1])
	if !chatID.MatchString(id) {
		return "", fmt.Errorf("%s create-chat: no chat ID in %q", bin, id)
	}
	return id, nil
}

func uuidV4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return formatUUID(b, 4)
}

func uuidV7() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	ms := uint64(time.Now().UnixMilli())
	for i := range 6 {
		b[i] = byte(ms >> (40 - 8*i))
	}
	return formatUUID(b, 7)
}

func formatUUID(b [16]byte, version byte) string {
	b[6] = b[6]&0x0f | version<<4
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var opencodeClock struct {
	sync.Mutex
	ms, n int64
}

// opencodeID makes an ID the way opencode does: "ses_", the inverted time
// in hex so newer sessions sort first, then random base62.
func opencodeID(now time.Time) string {
	c := &opencodeClock
	c.Lock()
	ms := now.UnixMilli()
	if ms != c.ms {
		c.ms, c.n = ms, 0
	}
	c.n++
	v := ^uint64(ms*0x1000 + c.n)
	c.Unlock()

	var sb strings.Builder
	sb.WriteString("ses_")
	for i := range 6 {
		fmt.Fprintf(&sb, "%02x", byte(v>>(40-8*i)))
	}
	var r [14]byte
	_, _ = rand.Read(r[:])
	for _, x := range r {
		sb.WriteByte(base62[int(x)%len(base62)])
	}
	return sb.String()
}

// readLine reads one line of any length.
func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadBytes('\n')
	if err == io.EOF && len(line) > 0 {
		return line, nil
	}
	return line, err
}

// findCodex reads the first line of codex's session files, which holds the
// session's ID, folder and start time. Files sit in day folders named in
// local time.
func findCodex(in func(string) bool, since time.Time) (string, error) {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(h, ".codex")
	}
	root := filepath.Join(home, "sessions")
	from := since.Local().AddDate(0, 0, -1).Format("2006/01/02")
	days, err := fs.Glob(os.DirFS(root), "[0-9][0-9][0-9][0-9]/[0-9][0-9]/[0-9][0-9]")
	if err != nil {
		return "", err
	}
	var best string
	var bestAt time.Time
	for _, day := range days {
		if day < from {
			continue
		}
		files, err := filepath.Glob(filepath.Join(root, day, "rollout-*.jsonl"))
		if err != nil {
			return "", err
		}
		for _, f := range files {
			id, at, ok := codexSession(f, in)
			if ok && !at.Before(since) && (best == "" || at.Before(bestAt)) {
				best, bestAt = id, at
			}
		}
	}
	return best, nil
}

// codexSession reads a session file's ID and start time when it is a main
// session (not a subagent) in the folder.
func codexSession(path string, in func(string) bool) (string, time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", time.Time{}, false
	}
	defer f.Close()
	line, err := readLine(bufio.NewReader(f))
	if err != nil {
		return "", time.Time{}, false
	}
	var meta struct {
		Payload struct {
			ID           string          `json:"id"`
			Cwd          string          `json:"cwd"`
			Timestamp    time.Time       `json:"timestamp"`
			ThreadSource string          `json:"thread_source"`
			Source       json.RawMessage `json:"source"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &meta) != nil {
		return "", time.Time{}, false
	}
	p := meta.Payload
	sub := p.ThreadSource == "subagent" || strings.HasPrefix(strings.TrimSpace(string(p.Source)), "{")
	if p.ID == "" || sub || !in(p.Cwd) {
		return "", time.Time{}, false
	}
	return p.ID, p.Timestamp, true
}

// findAgy looks in agy's prompt history for the first conversation in the folder
// after since. A conversation started with a prompt on the command line may
// have no history yet, so it falls back to agy's own "last conversation in
// this folder" when that one was used after since.
func findAgy(in func(string) bool, since time.Time) (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	app := filepath.Join(h, ".gemini", "antigravity-cli")
	id, err := agyHistory(filepath.Join(app, "history.jsonl"), in, since)
	if id != "" || err != nil {
		return id, err
	}
	data, err := os.ReadFile(filepath.Join(app, "cache", "last_conversations.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var last map[string]string
	if err := json.Unmarshal(data, &last); err != nil {
		return "", fmt.Errorf("agy last conversations: %w", err)
	}
	for ws, id := range last {
		if !in(ws) || id == "" {
			continue
		}
		for _, ext := range []string{".db", ".db-wal"} {
			fi, err := os.Stat(filepath.Join(app, "conversations", id+ext))
			if err == nil && !fi.ModTime().Before(since) {
				return id, nil
			}
		}
	}
	return "", nil
}

func agyHistory(path string, in func(string) bool, since time.Time) (string, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var best string
	var bestAt int64
	for {
		line, err := readLine(r)
		if err == io.EOF {
			return best, nil
		}
		if err != nil {
			return "", err
		}
		var e struct {
			ConversationID string `json:"conversationId"`
			Timestamp      int64  `json:"timestamp"`
			Workspace      string `json:"workspace"`
		}
		if json.Unmarshal(line, &e) != nil || e.ConversationID == "" || !in(e.Workspace) {
			continue
		}
		if e.Timestamp >= since.UnixMilli() && (best == "" || e.Timestamp < bestAt) {
			best, bestAt = e.ConversationID, e.Timestamp
		}
	}
}
