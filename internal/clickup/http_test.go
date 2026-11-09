package clickup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const taskJSON = `{
  "id": "%s", "custom_id": "%s", "name": "%s", "url": "https://app.clickup.com/t/%s",
  "status": {"status": "to start", "type": "unstarted", "orderindex": 1},
  "custom_item_id": 1005,
  "list": {"id": "900", "name": "Snowcap"},
  "assignees": [{"id": 1, "username": "Alexa"}],
  "due_date": "1794196800000", "date_updated": "1794218322647",
  "date_done": null, "date_closed": null,
  "priority": {"priority": "high"},
  "custom_fields": [
    {"id": "lead-f", "type": "users", "value": [{"id": 1, "username": "Alexa"}]},
    {"id": "other", "type": "url", "value": null}
  ]
}`

func jsonTask(id, cid, name string) string {
	return sprintf(taskJSON, id, cid, name, id)
}

func sprintf(f string, a ...any) string {
	out := f
	for _, v := range a {
		out = strings.Replace(out, "%s", v.(string), 1)
	}
	return out
}

type fakeAPI struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   []string
}

func (f *fakeAPI) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, r)
		f.bodies = append(f.bodies, string(body))
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "pk_test" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/user":
			io.WriteString(w, `{"user": {"id": 1, "username": "Alexa"}}`)
		case r.URL.Path == "/team/77/custom_item":
			io.WriteString(w, `{"custom_items": [{"id": 1005, "name": "Idea"}, {"id": 1006, "name": "Coding"}]}`)
		case r.URL.Path == "/team/77/task":
			q := r.URL.Query()
			page := q.Get("page")
			switch {
			case q.Get("custom_fields") != "" && page == "0":
				io.WriteString(w, `{"tasks": [`+jsonTask("t2", "DEV-2", "Lead task")+`], "last_page": false}`)
			case q.Get("custom_fields") != "" && page == "1":
				io.WriteString(w, `{"tasks": [`+jsonTask("t3", "DEV-3", "Lead task 2")+`], "last_page": true}`)
			default:
				io.WriteString(w, `{"tasks": [`+jsonTask("t1", "DEV-1", "Mine")+`,`+jsonTask("t2", "DEV-2", "Lead task")+`], "last_page": true}`)
			}
		case r.URL.Path == "/list/900":
			io.WriteString(w, `{"statuses": [{"status": "to start", "type": "unstarted", "orderindex": 1}, {"status": "in progress", "type": "custom", "orderindex": 2}]}`)
		case strings.HasPrefix(r.URL.Path, "/task/"):
			io.WriteString(w, `{}`)
		default:
			w.WriteHeader(404)
		}
	})
}

func newTestClient(t *testing.T) (*HTTP, *fakeAPI) {
	f := &fakeAPI{}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return &HTTP{BaseURL: srv.URL, Token: "pk_test", TeamID: "77", LeadFieldID: "lead-f"}, f
}

func TestMe(t *testing.T) {
	c, _ := newTestClient(t)
	u, err := c.Me(context.Background())
	if err != nil || u.ID != 1 || u.Username != "Alexa" {
		t.Fatalf("got %+v, %v", u, err)
	}
}

func TestMyTasksFullMergesQueriesAndPages(t *testing.T) {
	c, api := newTestClient(t)
	tasks, err := c.MyTasks(context.Background(), 1, time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Task{}
	for _, tk := range tasks {
		got[tk.Key()] = tk
	}
	if len(got) != 3 {
		t.Fatalf("want 3 unique tasks, got %v", keys(tasks))
	}
	tk := got["DEV-1"]
	if tk.ID != "t1" || tk.Status != "to start" || tk.StatusType != "unstarted" || tk.StatusOrder != 1 ||
		tk.ListID != "900" || tk.ListName != "Snowcap" || tk.TypeName != "Idea" || tk.Priority != "high" {
		t.Errorf("fields: %+v", tk)
	}
	if tk.Due.UnixMilli() != 1794196800000 || tk.Updated.IsZero() || !tk.Done.IsZero() {
		t.Errorf("dates: %+v", tk)
	}
	if len(tk.Leads) != 1 || tk.Leads[0].ID != 1 {
		t.Errorf("lead field not parsed: %+v", tk.Leads)
	}
	// 4 task queries (assigned/lead × open/recently closed) + 1 extra page + custom items.
	var taskQueries []string
	for _, r := range api.requests {
		if r.URL.Path == "/team/77/task" {
			taskQueries = append(taskQueries, r.URL.RawQuery)
		}
	}
	if len(taskQueries) != 6 {
		t.Errorf("task queries = %d: %v", len(taskQueries), taskQueries)
	}
	var sawLeadFilter, sawDoneFilter bool
	for _, q := range taskQueries {
		if strings.Contains(q, "custom_fields=") && strings.Contains(q, "lead-f") {
			sawLeadFilter = true
		}
		if strings.Contains(q, "date_done_gt=") {
			sawDoneFilter = true
		}
	}
	if !sawLeadFilter || !sawDoneFilter {
		t.Errorf("filters missing: lead=%v done=%v", sawLeadFilter, sawDoneFilter)
	}
}

func TestTaskTypesRetryAfterFailure(t *testing.T) {
	fail := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/team/77/custom_item":
			if fail {
				w.WriteHeader(503)
				return
			}
			io.WriteString(w, `{"custom_items": [{"id": 1005, "name": "Idea"}]}`)
		default:
			io.WriteString(w, `{"tasks": [`+jsonTask("t1", "DEV-1", "x")+`], "last_page": true}`)
		}
	}))
	defer srv.Close()
	c := &HTTP{BaseURL: srv.URL, Token: "pk", TeamID: "77", LeadFieldID: "lead-f"}
	ts, _ := c.MyTasks(context.Background(), 1, time.Time{}, now)
	if ts[0].TypeName != "" {
		t.Fatal("no types expected while the endpoint fails")
	}
	fail = false
	ts, _ = c.MyTasks(context.Background(), 1, time.Time{}, now)
	if ts[0].TypeName != "Idea" {
		t.Errorf("types not loaded after the endpoint recovered: %q", ts[0].TypeName)
	}
}

func TestMyTasksIncrementalUsesUpdatedSince(t *testing.T) {
	c, api := newTestClient(t)
	since := now.Add(-time.Hour)
	if _, err := c.MyTasks(context.Background(), 1, since, now); err != nil {
		t.Fatal(err)
	}
	for _, r := range api.requests {
		if r.URL.Path != "/team/77/task" {
			continue
		}
		q := r.URL.Query()
		if q.Get("date_updated_gt") == "" || q.Get("include_closed") != "true" {
			t.Errorf("incremental query: %s", r.URL.RawQuery)
		}
	}
}

func TestStatuses(t *testing.T) {
	c, _ := newTestClient(t)
	st, err := c.Statuses(context.Background(), "900")
	if err != nil || len(st) != 2 || st[1].Name != "in progress" {
		t.Fatalf("got %+v, %v", st, err)
	}
}

func TestWrites(t *testing.T) {
	c, api := newTestClient(t)
	ctx := context.Background()
	if err := c.SetStatus(ctx, "t1", "in progress"); err != nil {
		t.Fatal(err)
	}
	if err := c.AddComment(ctx, "t1", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetAssignee(ctx, "t1", 1, true); err != nil {
		t.Fatal(err)
	}
	if err := c.SetAssignee(ctx, "t1", 1, false); err != nil {
		t.Fatal(err)
	}
	want := []struct{ method, path, body string }{
		{"PUT", "/task/t1", `{"status":"in progress"}`},
		{"POST", "/task/t1/comment", `{"comment_text":"hello","notify_all":false}`},
		{"PUT", "/task/t1", `{"assignees":{"add":[1],"rem":[]}}`},
		{"PUT", "/task/t1", `{"assignees":{"add":[],"rem":[1]}}`},
	}
	if len(api.requests) != len(want) {
		t.Fatalf("requests = %d", len(api.requests))
	}
	for i, w := range want {
		r := api.requests[i]
		var gotBody, wantBody any
		json.Unmarshal([]byte(api.bodies[i]), &gotBody)
		json.Unmarshal([]byte(w.body), &wantBody)
		gb, _ := json.Marshal(gotBody)
		wb, _ := json.Marshal(wantBody)
		if r.Method != w.method || r.URL.Path != w.path || string(gb) != string(wb) {
			t.Errorf("request %d: %s %s %s, want %s %s %s", i, r.Method, r.URL.Path, gb, w.method, w.path, wb)
		}
	}
}

func TestAPIErrorIncludesStatus(t *testing.T) {
	c, _ := newTestClient(t)
	c.Token = "bad"
	_, err := c.Me(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "bad") {
		t.Error("token leaked into error")
	}
}
