package clickup

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slavafyi/huddle/internal/task"
)

// detectAPI fakes the endpoints detection uses. teams and fields are raw
// JSON arrays; fields are per team. A task query fails with 400 unless it
// uses team goodTeam and, when it filters by a custom field, goodLead.
type detectAPI struct {
	mu       sync.Mutex
	teams    string
	fields   map[string]string
	goodTeam string
	goodLead string
	paths    []string
	saves    []IDs // what the detector saved
}

func (f *detectAPI) set(teams string, fields map[string]string, team, lead string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.teams, f.fields, f.goodTeam, f.goodLead = teams, fields, team, lead
}

func (f *detectAPI) calls(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.paths {
		if strings.HasPrefix(p, prefix) {
			n++
		}
	}
	return n
}

func (f *detectAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.URL.Path)
	p := r.URL.Path
	switch {
	case p == "/team":
		io.WriteString(w, `{"teams": `+f.teams+`}`)
	case strings.HasPrefix(p, "/team/") && strings.HasSuffix(p, "/field"):
		fs, ok := f.fields[strings.TrimSuffix(strings.TrimPrefix(p, "/team/"), "/field")]
		if !ok {
			w.WriteHeader(401)
			io.WriteString(w, `{"err": "Team not authorized"}`)
			return
		}
		io.WriteString(w, `{"fields": `+fs+`}`)
	case strings.HasSuffix(p, "/custom_item"):
		io.WriteString(w, `{"custom_items": []}`)
	case strings.HasSuffix(p, "/task"):
		cf := r.URL.Query().Get("custom_fields")
		if p != "/team/"+f.goodTeam+"/task" || cf != "" && !strings.Contains(cf, `"`+f.goodLead+`"`) {
			w.WriteHeader(400)
			io.WriteString(w, `{"err": "bad"}`)
			return
		}
		io.WriteString(w, `{"tasks": [], "last_page": true}`)
	default:
		w.WriteHeader(404)
	}
}

const (
	oneTeam   = `[{"id": "77", "name": "Vertex"}]`
	leadField = `{"id": "lead-f", "name": "Lead Assignee", "type": "users"}`
	otherUser = `{"id": "rev-f", "name": "Reviewer", "type": "users"}`
	leadText  = `{"id": "lead-t", "name": "Lead notes", "type": "text"}`
)

func newDetector(t *testing.T, want IDs) (*Detector, *detectAPI) {
	api := &detectAPI{}
	api.set(oneTeam, map[string]string{"77": "[" + leadField + "," + otherUser + "," + leadText + "]"}, "77", "lead-f")
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	h := &HTTP{BaseURL: srv.URL, Token: "pk_test"}
	d := &Detector{HTTP: h, Want: want}
	d.Save = func(ids IDs) error { api.saves = append(api.saves, ids); return nil }
	return d, api
}

func TestDetectOneTeamOneLeadField(t *testing.T) {
	d, api := newDetector(t, IDs{})
	note, err := d.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.HTTP.TeamID != "77" || d.HTTP.LeadFieldID != "lead-f" || note != "" {
		t.Fatalf("team %q lead %q note %q", d.HTTP.TeamID, d.HTTP.LeadFieldID, note)
	}
	if len(api.saves) != 1 || api.saves[0] != (IDs{Workspace: "77", Lead: "lead-f"}) {
		t.Fatalf("saved %+v", api.saves)
	}
}

func TestDetectSeveralTeamsNamesThem(t *testing.T) {
	d, api := newDetector(t, IDs{})
	api.set(`[{"id": "77", "name": "Vertex"}, {"id": "88", "name": "Side"}]`, nil, "", "")
	_, err := d.Setup(context.Background())
	if err == nil {
		t.Fatal("want error")
	}
	for _, s := range []string{"77", "Vertex", "88", "Side", "clickup_workspace"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q lacks %q", err, s)
		}
	}
	if len(api.saves) != 0 {
		t.Errorf("saved %+v", api.saves)
	}
}

func TestDetectNoTeam(t *testing.T) {
	d, api := newDetector(t, IDs{})
	api.set(`[]`, nil, "", "")
	if _, err := d.Setup(context.Background()); err == nil || !strings.Contains(err.Error(), "no ClickUp workspace") {
		t.Fatalf("got %v", err)
	}
}

func TestDetectNoLeadFieldTurnsLeadOff(t *testing.T) {
	d, api := newDetector(t, IDs{})
	api.set(oneTeam, map[string]string{"77": "[" + otherUser + "," + leadText + "]"}, "77", "")
	note, err := d.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.HTTP.TeamID != "77" || d.HTTP.LeadFieldID != "" || !strings.Contains(note, "lead") {
		t.Fatalf("team %q lead %q note %q", d.HTTP.TeamID, d.HTTP.LeadFieldID, note)
	}
	// The result is remembered, so the next start has no note and no calls.
	d2 := &Detector{HTTP: &HTTP{BaseURL: d.HTTP.BaseURL, Token: "pk_test"}, Saved: api.saves[0]}
	before := api.calls("/team")
	note, err = d2.Setup(context.Background())
	if err != nil || note != "" || d2.HTTP.TeamID != "77" || d2.HTTP.LeadFieldID != "" {
		t.Fatalf("cached: note %q err %v team %q lead %q", note, err, d2.HTTP.TeamID, d2.HTTP.LeadFieldID)
	}
	if api.calls("/team") != before {
		t.Error("cached start called the API")
	}
}

func TestDetectSeveralLeadFieldsAsksForOne(t *testing.T) {
	d, api := newDetector(t, IDs{})
	api.set(oneTeam, map[string]string{"77": "[" + leadField + `, {"id": "lead-2", "name": "Tech lead", "type": "users"}]`}, "77", "")
	_, err := d.Setup(context.Background())
	if err == nil {
		t.Fatal("want error")
	}
	for _, s := range []string{"lead-f", "Lead Assignee", "lead-2", "Tech lead", "clickup_lead"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q lacks %q", err, s)
		}
	}
}

func TestDetectConfigWins(t *testing.T) {
	d, api := newDetector(t, IDs{Workspace: "99", Lead: "my-lead"})
	note, err := d.Setup(context.Background())
	if err != nil || note != "" || d.HTTP.TeamID != "99" || d.HTTP.LeadFieldID != "my-lead" {
		t.Fatalf("note %q err %v team %q lead %q", note, err, d.HTTP.TeamID, d.HTTP.LeadFieldID)
	}
	if n := api.calls("/team"); n != 0 {
		t.Errorf("%d API calls, want none", n)
	}
	if d.Auto() {
		t.Error("nothing to detect")
	}
}

func TestDetectConfigWorkspaceFindsLeadInIt(t *testing.T) {
	d, api := newDetector(t, IDs{Workspace: "88"})
	api.set(`[{"id": "77", "name": "A"}, {"id": "88", "name": "B"}]`, map[string]string{"88": "[" + leadField + "]"}, "88", "lead-f")
	if _, err := d.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d.HTTP.TeamID != "88" || d.HTTP.LeadFieldID != "lead-f" {
		t.Fatalf("team %q lead %q", d.HTTP.TeamID, d.HTTP.LeadFieldID)
	}
	if n := api.calls("/team/88/field"); n != 1 || api.calls("/team") != 1 {
		t.Errorf("calls %v", api.paths)
	}
}

func TestDetectCachedValuesReused(t *testing.T) {
	d, api := newDetector(t, IDs{})
	d.Saved = IDs{Workspace: "77", Lead: "lead-f"}
	note, err := d.Setup(context.Background())
	if err != nil || note != "" || d.HTTP.TeamID != "77" || d.HTTP.LeadFieldID != "lead-f" {
		t.Fatalf("note %q err %v team %q lead %q", note, err, d.HTTP.TeamID, d.HTTP.LeadFieldID)
	}
	if n := api.calls("/team"); n != 0 {
		t.Errorf("%d API calls, want none", n)
	}
}

func TestDetectCacheForOtherWorkspaceIgnored(t *testing.T) {
	d, api := newDetector(t, IDs{Workspace: "88"})
	api.set(oneTeam, map[string]string{"88": "[" + leadField + "]"}, "88", "lead-f")
	d.Saved = IDs{Workspace: "77", Lead: "old"}
	if _, err := d.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d.HTTP.LeadFieldID != "lead-f" {
		t.Fatalf("lead %q", d.HTTP.LeadFieldID)
	}
}

func TestSourceRedetectsStaleIDs(t *testing.T) {
	d, api := newDetector(t, IDs{})
	d.Saved = IDs{Workspace: "77", Lead: "gone-f"}
	if _, err := d.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	src := &Source{Client: d.HTTP, Detector: d}
	if _, err := src.Fetch(context.Background(), me, time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	if d.HTTP.LeadFieldID != "lead-f" {
		t.Fatalf("lead %q", d.HTTP.LeadFieldID)
	}
	note, warn := src.TakeNote()
	if !warn || !strings.Contains(note, "lead-f") {
		t.Errorf("note %q warn %v", note, warn)
	}
	if note, _ := src.TakeNote(); note != "" {
		t.Errorf("note shown twice: %q", note)
	}
	if len(api.saves) != 1 || api.saves[0].Lead != "lead-f" {
		t.Errorf("saved %+v", api.saves)
	}
	// Still stale after detecting again: the fetch error stands.
	api.set(oneTeam, map[string]string{"77": "[" + leadField + "]"}, "77", "never")
	if _, err := src.Fetch(context.Background(), me, time.Time{}, now); err == nil {
		t.Error("want error")
	}
}

func TestSourceDoesNotRedetectConfiguredIDs(t *testing.T) {
	d, api := newDetector(t, IDs{Workspace: "77", Lead: "bad"})
	if _, err := d.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	src := &Source{Client: d.HTTP, Detector: d}
	if _, err := src.Fetch(context.Background(), me, time.Time{}, now); err == nil {
		t.Fatal("want error")
	}
	if api.calls("/team/77/field") != 0 || d.HTTP.LeadFieldID != "bad" {
		t.Errorf("configured lead was replaced: %q", d.HTTP.LeadFieldID)
	}
}

func TestNoLeadSkipsLeadQueryAndFeatures(t *testing.T) {
	d, api := newDetector(t, IDs{})
	api.set(oneTeam, map[string]string{"77": "[]"}, "77", "")
	if _, err := d.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.HTTP.MyTasks(context.Background(), me, time.Time{}, now); err != nil {
		t.Fatal(err) // a lead query with an empty field ID would fail (400)
	}
	r := (&Source{Client: d.HTTP, NoLead: true}).Rules()
	for _, g := range r.Groups {
		if g == task.GroupWaiting {
			t.Error("Waiting on others without a lead field")
		}
	}
	if r.FilterNames != [3]string{"Assigned", "", ""} {
		t.Errorf("filters %q", r.FilterNames)
	}
}
