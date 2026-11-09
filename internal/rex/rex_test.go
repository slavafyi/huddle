package rex

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/slavafyi/cuddle/internal/run"
)

func TestNewSession(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "rex new", Out: `{"session_id":"session:S","label":null,"initial_windows":[{"window_id":"window:W","block_ids":["block:B"],"label":null}]}`}}}
	c := &CLI{Runner: f}
	p, err := c.NewSession(context.Background(), "Snowcap", "/w/dir", "DEV-1 Title", []string{"claude", "Take a task DEV-1 from CU"})
	if err != nil {
		t.Fatal(err)
	}
	if p != (Placement{SessionID: "session:S", WindowID: "window:W", BlockID: "block:B"}) {
		t.Errorf("placement = %+v", p)
	}
	want := []string{"rex new Snowcap -c /w/dir --window DEV-1 Title --json -- claude Take a task DEV-1 from CU"}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q", got)
	}
	args := f.CallsWithDir()[0].Args
	if args[len(args)-1] != "Take a task DEV-1 from CU" {
		t.Error("prompt must be one argument")
	}
}

func TestNewWindowShellHasNoCommand(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "rex window new", Out: `{"window_id":"window:W","block_ids":["block:B"]}`}}}
	c := &CLI{Runner: f}
	p, err := c.NewWindow(context.Background(), "session:S", "DEV-1 T", "/d", nil)
	if err != nil || p.BlockID != "block:B" || p.SessionID != "session:S" {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	want := "rex window new -s session:S DEV-1 T -c /d --focus=false --json"
	if got := f.Calls()[0]; got != want {
		t.Errorf("call = %q", got)
	}
}

func TestSessionsAndBlocks(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{
		{Match: "rex ls", Out: `{"sessions":[{"session_id":"session:A","label":"Snowcap"},{"session_id":"session:B","label":null}]}`},
		{Match: "rex block ls -s session:A", Out: `{"blocks":[{"block_id":"block:1","window_id":"window:1"}]}`},
		{Match: "rex block ls -s session:gone", Err: errors.New(`rex: exit status 3: Error: session target "session:gone" not found`)},
		{Match: "rex block ls -s session:slow", Err: errors.New("rex: exit status 1: Error: timeout")},
	}}
	c := &CLI{Runner: f}
	ss, err := c.Sessions(context.Background())
	if err != nil || ss["session:A"] != "Snowcap" || len(ss) != 2 {
		t.Fatalf("sessions = %v, %v", ss, err)
	}
	bs, err := c.Blocks(context.Background(), "session:A")
	if err != nil || !bs["block:1"] {
		t.Fatalf("blocks = %v, %v", bs, err)
	}
	bs, err = c.Blocks(context.Background(), "session:gone")
	if err != nil || len(bs) != 0 {
		t.Errorf("gone session should give no blocks: %v, %v", bs, err)
	}
	if _, err = c.Blocks(context.Background(), "session:slow"); err == nil {
		t.Error("other rex errors must be returned, not read as 'no blocks'")
	}
}

func TestNewWindowNeedsWindowID(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "rex window new", Out: `{"block_ids":["block:B"]}`}}}
	if _, err := (&CLI{Runner: f}).NewWindow(context.Background(), "session:S", "n", "/d", nil); err == nil {
		t.Error("want error when the reply has no window id")
	}
}

func TestStatus(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "rex block call", Out: `{"records":[{"id":"build","state":"working"},{"state":"blocked","kind":"question","msg":"Need input","app":"claude-code"}]}`}}}
	c := &CLI{Runner: f}
	st, err := c.Status(context.Background(), "session:S", "block:B")
	if err != nil {
		t.Fatal(err)
	}
	if st != (Status{State: "blocked", Kind: "question", Msg: "Need input"}) {
		t.Errorf("status = %+v (want root record)", st)
	}
	if got := f.Calls()[0]; got != "rex block call -s session:S -b block:B program_status {}" {
		t.Errorf("call = %q", got)
	}
	f.Responses = []run.Response{{Match: "rex block call", Out: `{"records":[]}`}}
	if st, _ := c.Status(context.Background(), "session:S", "block:B"); st.State != "" {
		t.Errorf("no records: %+v", st)
	}
}

func TestFocusUsesAppClient(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{
		{Match: "rex client ls", Out: `{"clients":[{"client_id":"client:cli","info":{"kind":"cli"}},{"client_id":"client:app","info":{"kind":"app"}}]}`},
	}}
	c := &CLI{Runner: f}
	if err := c.Focus(context.Background(), "session:S", "window:W"); err != nil {
		t.Fatal(err)
	}
	want := []string{"rex client ls --json", "rex -C client:app do session.select session_id=session:S window_id=window:W"}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q", got)
	}
}

func TestFocusFallsBackWhenRemoteControlIsOff(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{
		{Match: "rex client ls", Out: `{"clients":[{"client_id":"client:app","info":{"kind":"app"}}]}`},
		{Match: "rex -C client:app do", Err: errors.New("remote control is off")},
	}}
	c := &CLI{Runner: f}
	err := c.Focus(context.Background(), "session:S", "window:W")
	if !errors.Is(err, ErrRemoteControl) {
		t.Fatalf("err = %v", err)
	}
	calls := f.Calls()
	if calls[len(calls)-1] != "rex window focus -s session:S window:W" {
		t.Errorf("no fallback: %q", calls)
	}
}

func TestCloseAndLastLine(t *testing.T) {
	f := &run.Fake{Responses: []run.Response{{Match: "rex capture", Out: "line one\n› Running tests…\n\n  \n"}}}
	c := &CLI{Runner: f}
	if err := c.CloseWindow(context.Background(), "session:S", "window:W"); err != nil {
		t.Fatal(err)
	}
	l, err := c.LastLine(context.Background(), "session:S", "block:B")
	if err != nil || l != "› Running tests…" {
		t.Errorf("last line = %q, %v", l, err)
	}
	want := []string{"rex window close -s session:S window:W", "rex capture -s session:S -b block:B --trim"}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q", got)
	}
}

// Lines captured from `rex events --all --json` on 2026-11-09.
const (
	evStatus = `{"type":"session_event","payload":{"session_id":"session:S","event":{"type":"block_event","payload":{"block_id":"block:B","creator_name":"com.superlogical.terminal","name":"program_status_changed","payload":{"id":"","record":{"state":"blocked","app":"cuddletest","effective_app":"cuddletest","msg":"Need input","updated_at":"2026-11-09T13:52:57.361527+00:00","owner":{"pid":68928,"name":"bash"}},"reason":"report"}}}}}`
	evChild  = `{"type":"session_event","payload":{"session_id":"session:S","event":{"type":"block_event","payload":{"block_id":"block:B","creator_name":"com.superlogical.terminal","name":"program_status_changed","payload":{"id":"build","record":{"state":"working"},"reason":"report"}}}}}`
	evClosed = `{"type":"session_event","payload":{"session_id":"session:S","event":{"type":"block_closed","payload":{"block_id":"block:B"}}}}`
	evOther  = `{"type":"client_joined","payload":{}}`
	evClear  = `{"type":"session_event","payload":{"session_id":"session:S","event":{"type":"block_event","payload":{"block_id":"block:B","name":"program_status_changed","payload":{"id":"","reason":"cleared"}}}}}`
)

func TestParseEvent(t *testing.T) {
	ev, ok := ParseEvent([]byte(evStatus))
	if !ok || ev != (Event{SessionID: "session:S", BlockID: "block:B", HasStatus: true, Status: Status{State: "blocked", Msg: "Need input"}}) {
		t.Errorf("status event = %+v %v", ev, ok)
	}
	if _, ok := ParseEvent([]byte(evChild)); ok {
		t.Error("child records should be ignored")
	}
	ev, ok = ParseEvent([]byte(evClosed))
	if !ok || !ev.Closed || ev.BlockID != "block:B" {
		t.Errorf("closed event = %+v %v", ev, ok)
	}
	if _, ok := ParseEvent([]byte(evOther)); ok {
		t.Error("unrelated event parsed")
	}
	ev, ok = ParseEvent([]byte(evClear))
	if !ok || !ev.HasStatus || ev.Status.State != "" {
		t.Errorf("cleared record should give empty state: %+v %v", ev, ok)
	}
	if _, ok := ParseEvent([]byte("not json")); ok {
		t.Error("bad line parsed")
	}
}
