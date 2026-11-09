package clickup

import (
	"testing"
	"time"
)

const me int64 = 1

var (
	malcolm = User{ID: 2, Username: "Malcolm"}
	owner   = User{ID: me, Username: "Alexa"}
	now     = time.Date(2026, 11, 9, 13, 0, 0, 0, time.UTC)
)

func day(d int) time.Time { return now.AddDate(0, 0, d) }

func keys(ts []Task) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Key())
	}
	return out
}

func TestKeyPrefersCustomID(t *testing.T) {
	if k := (Task{ID: "869f", CustomID: "DEV-1"}).Key(); k != "DEV-1" {
		t.Errorf("key = %q", k)
	}
	if k := (Task{ID: "869f"}).Key(); k != "869f" {
		t.Errorf("key = %q", k)
	}
}

func TestRelevant(t *testing.T) {
	cases := []struct {
		name string
		task Task
		want bool
	}{
		{"assigned to me, open", Task{Assignees: []User{owner}}, true},
		{"I am lead, with Malcolm", Task{Assignees: []User{malcolm}, Leads: []User{owner}}, true},
		{"I am lead, unassigned", Task{Leads: []User{owner}}, true},
		{"not mine", Task{Assignees: []User{malcolm}}, false},
		{"mine, closed 6 days ago", Task{Assignees: []User{owner}, Done: day(-6)}, true},
		{"mine, closed 8 days ago", Task{Assignees: []User{owner}, Done: day(-8)}, false},
	}
	for _, c := range cases {
		if got := Relevant(c.task, me, now); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestWith(t *testing.T) {
	if w := With(Task{Assignees: []User{malcolm, owner}}, me); w != "you" {
		t.Errorf("got %q", w)
	}
	if w := With(Task{Assignees: []User{malcolm}}, me); w != "Malcolm" {
		t.Errorf("got %q", w)
	}
	if w := With(Task{}, me); w != "nobody" {
		t.Errorf("got %q", w)
	}
}

func TestToday(t *testing.T) {
	tasks := []Task{
		{CustomID: "A", Status: "in progress", Assignees: []User{owner}, Due: day(1)},
		{CustomID: "B", Status: "feedbacks", Assignees: []User{owner}, Due: day(-2)},
		{CustomID: "C", Status: "to start", Assignees: []User{owner}},
		{CustomID: "D", Status: "to start", Assignees: []User{owner}, Due: day(3)},
		{CustomID: "E", Status: "to review", Assignees: []User{malcolm}, Leads: []User{owner}, Updated: day(-1)},
		{CustomID: "F", Status: "pending", Assignees: []User{malcolm}, Leads: []User{owner}, Updated: day(0)},
		{CustomID: "G", Status: "pending", Assignees: []User{owner}},    // mine but pending: not shown
		{CustomID: "H", Status: "to plan", Leads: []User{owner}},        // lead, not waiting
		{CustomID: "I", Status: "to start", Assignees: []User{malcolm}}, // not mine
		{CustomID: "J", Status: "Closed", Assignees: []User{owner}, Done: day(-2)},
		{CustomID: "K", Status: "Closed", Assignees: []User{owner}, Done: day(-1)},
		{CustomID: "L", Status: "in progress", Leads: []User{owner}, Assignees: []User{malcolm}}, // lead, in progress: not waiting
	}
	groups := Today(tasks, me, now)
	want := map[string][]string{
		"In progress":       {"B", "A"},
		"Up next":           {"D", "C"},
		"Waiting on others": {"F", "E"},
		"Done this week":    {"K", "J"},
	}
	if len(groups) != 4 {
		t.Fatalf("groups = %d", len(groups))
	}
	for _, g := range groups {
		got := keys(g.Tasks)
		if len(got) != len(want[g.Name]) {
			t.Errorf("%s: got %v want %v", g.Name, got, want[g.Name])
			continue
		}
		for i := range got {
			if got[i] != want[g.Name][i] {
				t.Errorf("%s: got %v want %v", g.Name, got, want[g.Name])
				break
			}
		}
	}
}

func TestByStatusUsesOrderIndexAndHidesEmpty(t *testing.T) {
	tasks := []Task{
		{CustomID: "A", Status: "to start", StatusOrder: 1},
		{CustomID: "B", Status: "in progress", StatusOrder: 2},
		{CustomID: "C", Status: "to start", StatusOrder: 1, Due: day(1)},
	}
	groups := ByStatus(tasks)
	if len(groups) != 2 || groups[0].Name != "to start" || groups[1].Name != "in progress" {
		t.Fatalf("groups: %+v", groups)
	}
	if got := keys(groups[0].Tasks); got[0] != "C" {
		t.Errorf("due date first: %v", got)
	}
}

func TestFilter(t *testing.T) {
	tasks := []Task{
		{CustomID: "A", Assignees: []User{owner}},
		{CustomID: "B", Assignees: []User{malcolm}, Leads: []User{owner}},
	}
	if got := keys(Filter(tasks, me, FilterAssigned)); len(got) != 1 || got[0] != "A" {
		t.Errorf("assigned: %v", got)
	}
	if got := keys(Filter(tasks, me, FilterLeadOnly)); len(got) != 1 || got[0] != "B" {
		t.Errorf("lead only: %v", got)
	}
	if got := Filter(tasks, me, FilterAll); len(got) != 2 {
		t.Errorf("all: %v", got)
	}
}

func TestMerge(t *testing.T) {
	cached := []Task{
		{ID: "1", Name: "old", Assignees: []User{owner}},
		{ID: "2", Name: "keep", Assignees: []User{owner}},
	}
	updates := []Task{
		{ID: "1", Name: "new", Assignees: []User{owner}},
		{ID: "3", Name: "added", Leads: []User{owner}},
		{ID: "4", Name: "not mine", Assignees: []User{malcolm}},
	}
	got := Merge(cached, updates, false, me, now)
	names := map[string]string{}
	for _, t := range got {
		names[t.ID] = t.Name
	}
	if names["1"] != "new" || names["2"] != "keep" || names["3"] != "added" || len(got) != 3 {
		t.Errorf("incremental merge: %v", names)
	}
	if got := Merge(cached, updates[:1], true, me, now); len(got) != 1 {
		t.Errorf("full sync should replace: %d", len(got))
	}
}

func TestScratchDefault(t *testing.T) {
	prefixes := []string{"research", "evaluate"}
	types := []string{"Idea"}
	if !WantsScratch(Task{Name: "Research low stock notifications"}, types, prefixes) {
		t.Error("research title")
	}
	if !WantsScratch(Task{Name: "Launch referral", TypeName: "Idea"}, types, prefixes) {
		t.Error("idea type")
	}
	if WantsScratch(Task{Name: "Promo: checkout", TypeName: "Coding"}, types, prefixes) {
		t.Error("coding task")
	}
	for _, name := range []string{"Checkout: add upsell", "Evaluated options already", "Researching nothing"} {
		if WantsScratch(Task{Name: name}, types, []string{"check", "evaluate", "research"}) {
			t.Errorf("%q: prefix must match the whole first word", name)
		}
	}
	if !WantsScratch(Task{Name: "Check: promo code report"}, types, []string{"check"}) {
		t.Error("first word followed by punctuation")
	}
}
