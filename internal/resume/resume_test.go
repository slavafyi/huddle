package resume

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/slavafyi/huddle/internal/run"
)

var (
	isUUIDv4   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	isUUIDv7   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	isOpencode = regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`)
)

func TestForMatchesTheCommandName(t *testing.T) {
	for _, argv := range [][]string{
		{"claude"}, {"/usr/local/bin/codex", "x"}, {"pi"}, {"opencode"}, {"cursor-agent"}, {"agy"},
	} {
		if For(argv) == nil {
			t.Errorf("no adapter for %v", argv)
		}
	}
	for _, argv := range [][]string{nil, {}, {"aider"}, {"claude-wrapper"}} {
		if For(argv) != nil {
			t.Errorf("adapter for %v", argv)
		}
	}
}

func start(t *testing.T, r run.Runner, argv ...string) ([]string, string) {
	t.Helper()
	out, id, err := For(argv).Start(context.Background(), r, "/w/dev-1", argv)
	if err != nil {
		t.Fatal(err)
	}
	return out, id
}

func TestStartSetsTheAgentsOwnIDFormat(t *testing.T) {
	cases := []struct {
		argv []string
		flag string
		id   *regexp.Regexp
	}{
		{[]string{"claude", "Take a task"}, "--session-id", isUUIDv4},
		{[]string{"pi", "Take a task"}, "--session-id", isUUIDv7},
		{[]string{"opencode", "--prompt", "Take a task"}, "--session", isOpencode},
	}
	for _, c := range cases {
		out, id := start(t, &run.Fake{}, c.argv...)
		if !c.id.MatchString(id) {
			t.Errorf("%s: id %q has the wrong format", c.argv[0], id)
		}
		want := append([]string{c.argv[0], c.flag, id}, c.argv[1:]...)
		if !slices.Equal(out, want) {
			t.Errorf("%s: argv = %q, want %q", c.argv[0], out, want)
		}
	}
}

func TestStartMakesNewIDsEachTime(t *testing.T) {
	for _, bin := range []string{"claude", "pi", "opencode"} {
		_, a := start(t, &run.Fake{}, bin, "x")
		_, b := start(t, &run.Fake{}, bin, "x")
		if a == b {
			t.Errorf("%s: same id twice: %s", bin, a)
		}
	}
}

func TestOpencodeIDsSortNewestFirst(t *testing.T) {
	a := opencodeID(time.UnixMilli(1_000_000))
	b := opencodeID(time.UnixMilli(2_000_000))
	if !(b < a) {
		t.Errorf("newer id %s should sort before %s", b, a)
	}
}

func TestStartAsksCursorForAChat(t *testing.T) {
	r := &run.Fake{Responses: []run.Response{{Match: "cursor-agent create-chat", Out: "  \n7d4c2a10-5b6e-4f1a-9c3d-2e8f6a1b0c9d\n"}}}
	out, id := start(t, r, "cursor-agent", "Take a task")
	if id != "7d4c2a10-5b6e-4f1a-9c3d-2e8f6a1b0c9d" {
		t.Errorf("id = %q", id)
	}
	if want := []string{"cursor-agent", "--resume", id, "Take a task"}; !slices.Equal(out, want) {
		t.Errorf("argv = %q", out)
	}
	if calls := r.CallsWithDir(); len(calls) != 1 || calls[0].Dir != "/w/dev-1" {
		t.Errorf("calls = %+v", r.Calls())
	}
}

func TestStartFailsWhenCursorGivesNoChat(t *testing.T) {
	for _, r := range []*run.Fake{
		{Responses: []run.Response{{Match: "cursor-agent create-chat", Err: errors.New("not logged in")}}},
		{Responses: []run.Response{{Match: "cursor-agent create-chat", Out: "\n"}}},
		{Responses: []run.Response{{Match: "cursor-agent create-chat", Out: "Please log in first\n"}}},
	} {
		argv := []string{"cursor-agent", "x"}
		out, id, err := For(argv).Start(context.Background(), r, "/w", argv)
		if err == nil || id != "" || !slices.Equal(out, argv) {
			t.Errorf("out=%q id=%q err=%v", out, id, err)
		}
	}
}

func TestStartLeavesAgentsThatPickTheirOwnID(t *testing.T) {
	for _, argv := range [][]string{{"codex", "x"}, {"agy", "-i", "x"}} {
		out, id := start(t, &run.Fake{}, argv...)
		if id != "" || !slices.Equal(out, argv) {
			t.Errorf("%s: out=%q id=%q", argv[0], out, id)
		}
	}
}

func TestStartKeepsASessionTheUserChose(t *testing.T) {
	r := &run.Fake{}
	for _, argv := range [][]string{
		{"claude", "--resume", "abc", "x"},
		{"claude", "--session-id=abc", "x"},
		{"pi", "--session", "abc"},
		{"opencode", "-s", "abc"},
		{"cursor-agent", "--resume", "abc"},
	} {
		out, id := start(t, r, argv...)
		if id != "" || !slices.Equal(out, argv) {
			t.Errorf("%v: out=%q id=%q", argv, out, id)
		}
	}
	if len(r.Calls()) != 0 {
		t.Errorf("calls = %+v", r.Calls())
	}
}

func TestBaseDropsThePrompt(t *testing.T) {
	cases := map[string][]string{
		"claude":                       {"claude", "{prompt}"},
		"claude --model opus":          {"claude", "--model", "opus", "{prompt}"},
		"opencode":                     {"opencode", "--prompt", "{prompt}"},
		"agy --mode plan":              {"agy", "--mode", "plan", "-i", "{prompt}"},
		"agy":                          {"agy", "--prompt-interactive", "Do this: {prompt}"},
		"codex --search":               {"codex", "--search", "{prompt}"},
		"cursor-agent --model gpt-6.1": {"cursor-agent", "--model", "gpt-6.1", "{prompt}"},
	}
	for want, tmpl := range cases {
		if got := strings.Join(Base(tmpl), " "); got != want {
			t.Errorf("Base(%q) = %q, want %q", tmpl, got, want)
		}
	}
	if Base(nil) != nil {
		t.Error("no template should give no command")
	}
}

func TestCommandReopensTheSession(t *testing.T) {
	cases := []struct{ base, want []string }{
		{[]string{"claude", "--model", "opus"}, []string{"claude", "--resume", "ID", "--model", "opus"}},
		{[]string{"codex", "--search"}, []string{"codex", "resume", "--search", "ID"}},
		{[]string{"pi"}, []string{"pi", "--session", "ID"}},
		{[]string{"opencode"}, []string{"opencode", "--session", "ID"}},
		{[]string{"cursor-agent"}, []string{"cursor-agent", "--resume", "ID"}},
		{[]string{"agy", "--mode", "plan"}, []string{"agy", "--conversation", "ID", "--mode", "plan"}},
	}
	for _, c := range cases {
		if got := For(c.base).Command(c.base, "ID"); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q, want %q", c.base[0], got, c.want)
		}
	}
}

// Times below are UTC; codex names its day folders and files in local time.
var started = time.Date(2026, 11, 9, 13, 0, 0, 0, time.Local)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rollout(t *testing.T, home string, at time.Time, id, cwd, source string) {
	t.Helper()
	name := "rollout-" + at.Format("2006-01-02T15-04-05") + "-" + id + ".jsonl"
	path := filepath.Join(home, "sessions", at.Format("2006/01/02"), name)
	meta := `{"timestamp":"` + at.UTC().Format(time.RFC3339Nano) + `","type":"session_meta","payload":{"id":"` + id +
		`","timestamp":"` + at.UTC().Format(time.RFC3339Nano) + `","cwd":"` + cwd + `","thread_source":"` + source +
		`","base_instructions":{"text":"` + strings.Repeat("x", 200_000) + `"}}}`
	writeFile(t, path, meta+"\n"+`{"type":"event_msg"}`+"\n")
}

func TestFindCodexTakesTheFirstSessionInTheFolderAfterLaunch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := "/w/snowcap"
	rollout(t, home, started.Add(-time.Hour), "0aaaaaaa-0000-7000-8000-000000000001", dir, "user")           // before launch
	rollout(t, home, started.Add(2*time.Second), "0aaaaaaa-0000-7000-8000-000000000002", "/w/other", "user") // other folder
	rollout(t, home, started.Add(3*time.Second), "0aaaaaaa-0000-7000-8000-000000000003", dir, "user")        // ours
	rollout(t, home, started.Add(4*time.Second), "0aaaaaaa-0000-7000-8000-000000000004", dir, "subagent")    // its subagent
	rollout(t, home, started.Add(time.Hour), "0aaaaaaa-0000-7000-8000-000000000005", dir, "user")            // later session
	rollout(t, home, started.Add(36*time.Hour), "0aaaaaaa-0000-7000-8000-000000000006", dir, "user")         // another day

	id, err := For([]string{"codex"}).Find(dir, started)
	if err != nil || id != "0aaaaaaa-0000-7000-8000-000000000003" {
		t.Errorf("id=%q err=%v", id, err)
	}
	id, err = For([]string{"codex"}).Find(dir, started.Add(30*time.Hour))
	if err != nil || id != "0aaaaaaa-0000-7000-8000-000000000006" {
		t.Errorf("next day: id=%q err=%v", id, err)
	}
	id, err = For([]string{"codex"}).Find("/w/none", started)
	if err != nil || id != "" {
		t.Errorf("no match: id=%q err=%v", id, err)
	}
}

func TestFindCodexUsesTheHomeFolderByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	rollout(t, filepath.Join(home, ".codex"), started.Add(time.Second), "0bbbbbbb-0000-7000-8000-000000000001", "/w/a", "user")
	if id, err := For([]string{"codex"}).Find("/w/a", started); err != nil || id != "0bbbbbbb-0000-7000-8000-000000000001" {
		t.Errorf("id=%q err=%v", id, err)
	}
}

func TestFindCodexWithoutSessions(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	if id, err := For([]string{"codex"}).Find("/w/a", started); err != nil || id != "" {
		t.Errorf("id=%q err=%v", id, err)
	}
}

func agyHome(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, ".gemini", "antigravity-cli")
}

func historyLine(at time.Time, id, ws string) string {
	cid := ""
	if id != "" {
		cid = `"conversationId":"` + id + `",`
	}
	return `{"display":"hello",` + cid + `"timestamp":` + strconv.FormatInt(at.UnixMilli(), 10) + `,"workspace":"` + ws + `"}` + "\n"
}

func TestFindAgyTakesTheFirstConversationInTheFolderAfterLaunch(t *testing.T) {
	app := agyHome(t)
	dir := "/w/harbor"
	writeFile(t, filepath.Join(app, "history.jsonl"),
		historyLine(started.Add(-time.Hour), "c0000000-0000-4000-8000-000000000001", dir)+
			historyLine(started.Add(time.Second), "", dir)+
			historyLine(started.Add(2*time.Second), "c0000000-0000-4000-8000-000000000002", "/w/other")+
			historyLine(started.Add(3*time.Second), "c0000000-0000-4000-8000-000000000003", dir)+
			"not json\n"+
			historyLine(started.Add(time.Hour), "c0000000-0000-4000-8000-000000000004", dir))
	id, err := For([]string{"agy"}).Find(dir, started)
	if err != nil || id != "c0000000-0000-4000-8000-000000000003" {
		t.Errorf("id=%q err=%v", id, err)
	}
}

func TestFindAgyFallsBackToTheFoldersLastConversation(t *testing.T) {
	app := agyHome(t)
	dir := "/w/harbor"
	writeFile(t, filepath.Join(app, "cache", "last_conversations.json"),
		`{"`+dir+`":"c0000000-0000-4000-8000-000000000007","/w/other":"c0000000-0000-4000-8000-000000000008"}`)
	db := filepath.Join(app, "conversations", "c0000000-0000-4000-8000-000000000007.db")
	writeFile(t, db, "")

	if err := os.Chtimes(db, started.Add(time.Minute), started.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if id, err := For([]string{"agy"}).Find(dir, started); err != nil || id != "c0000000-0000-4000-8000-000000000007" {
		t.Errorf("used after launch: id=%q err=%v", id, err)
	}

	// Last used before the launch: it is not this tab's conversation.
	if err := os.Chtimes(db, started.Add(-time.Hour), started.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if id, err := For([]string{"agy"}).Find(dir, started); err != nil || id != "" {
		t.Errorf("used before launch: id=%q err=%v", id, err)
	}
}

func TestFindAgyWithNothingSaved(t *testing.T) {
	agyHome(t)
	if id, err := For([]string{"agy"}).Find("/w/a", started); err != nil || id != "" {
		t.Errorf("id=%q err=%v", id, err)
	}
}

func TestFindIsEmptyForAgentsWithAPresetID(t *testing.T) {
	for _, bin := range []string{"claude", "pi", "opencode", "cursor-agent"} {
		if id, err := For([]string{bin}).Find("/w/a", started); err != nil || id != "" {
			t.Errorf("%s: id=%q err=%v", bin, id, err)
		}
	}
}

func TestFindFollowsSymlinkedFolders(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	real, _ = filepath.EvalSymlinks(real) // the agents save the resolved path

	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	rollout(t, home, started.Add(time.Second), "0ddddddd-0000-7000-8000-000000000001", real, "user")
	if id, err := For([]string{"codex"}).Find(link, started); err != nil || id != "0ddddddd-0000-7000-8000-000000000001" {
		t.Errorf("codex: id=%q err=%v", id, err)
	}

	app := agyHome(t)
	writeFile(t, filepath.Join(app, "history.jsonl"), historyLine(started.Add(time.Second), "c0000000-0000-4000-8000-000000000009", real))
	if id, err := For([]string{"agy"}).Find(link, started); err != nil || id != "c0000000-0000-4000-8000-000000000009" {
		t.Errorf("agy: id=%q err=%v", id, err)
	}
}
