package task

import (
	"testing"
	"time"
)

var now = time.Date(2026, 11, 9, 13, 0, 0, 0, time.UTC)

// rules are the ClickUp grouping defaults, as the clickup source gives them.
var rules = map[string]Rules{"cu": {
	Active:  []string{"in progress", "feedbacks"},
	Next:    []string{"to start"},
	Waiting: []string{"to review", "pending", "to publish"},
}}

func day(d int) time.Time { return now.AddDate(0, 0, d) }

func keys(ts []Task) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Key)
	}
	return out
}

func TestRefIsUniqueAcrossSources(t *testing.T) {
	a := Task{Source: "clickup", ID: "42"}
	b := Task{Source: "github", ID: "42"}
	if a.Ref() == b.Ref() {
		t.Errorf("refs collide: %q", a.Ref())
	}
	if a.Ref() != Ref("clickup", "42") {
		t.Errorf("Ref() = %q", a.Ref())
	}
}

func TestRelevant(t *testing.T) {
	cases := []struct {
		name string
		task Task
		want bool
	}{
		{"assigned to me, open", Task{Mine: true}, true},
		{"I am lead, with Malcolm", Task{Lead: true, Others: []string{"Malcolm"}}, true},
		{"I am lead, unassigned", Task{Lead: true}, true},
		{"not mine", Task{Others: []string{"Malcolm"}}, false},
		{"mine, closed 6 days ago", Task{Mine: true, Done: day(-6)}, true},
		{"mine, closed 8 days ago", Task{Mine: true, Done: day(-8)}, false},
	}
	for _, c := range cases {
		if got := Relevant(c.task, now); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestWith(t *testing.T) {
	if w := (Task{Mine: true, Others: []string{"Malcolm"}}).With(); w != "you" {
		t.Errorf("got %q", w)
	}
	if w := (Task{Others: []string{"Malcolm"}}).With(); w != "Malcolm" {
		t.Errorf("got %q", w)
	}
	if w := (Task{}).With(); w != "nobody" {
		t.Errorf("got %q", w)
	}
}

func TestToday(t *testing.T) {
	cu := func(tk Task) Task { tk.Source = "cu"; return tk }
	tasks := []Task{
		cu(Task{Key: "A", Status: "in progress", Mine: true, Due: day(1)}),
		cu(Task{Key: "B", Status: "feedbacks", Mine: true, Due: day(-2)}),
		cu(Task{Key: "C", Status: "to start", Mine: true}),
		cu(Task{Key: "D", Status: "to start", Mine: true, Due: day(3)}),
		cu(Task{Key: "E", Status: "to review", Lead: true, Updated: day(-1)}),
		cu(Task{Key: "F", Status: "pending", Lead: true, Updated: day(0)}),
		cu(Task{Key: "G", Status: "pending", Mine: true}), // mine but pending: not shown
		cu(Task{Key: "H", Status: "to plan", Lead: true}), // lead, not waiting
		cu(Task{Key: "I", Status: "to start"}),            // not mine
		cu(Task{Key: "J", Status: "Closed", Mine: true, Done: day(-2)}),
		cu(Task{Key: "K", Status: "Closed", Mine: true, Done: day(-1)}),
		cu(Task{Key: "L", Status: "in progress", Lead: true}),          // lead, in progress: not waiting
		{Key: "M", Source: "other", Status: "in progress", Mine: true}, // no rules for its source
	}
	groups := Today(tasks, rules, now)
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
		{Key: "A", Status: "to start", StatusOrder: 1},
		{Key: "B", Status: "in progress", StatusOrder: 2},
		{Key: "C", Status: "to start", StatusOrder: 1, Due: day(1)},
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
		{Key: "A", Mine: true},
		{Key: "B", Lead: true, Others: []string{"Malcolm"}},
	}
	if got := keys(Filter(tasks, FilterAssigned)); len(got) != 1 || got[0] != "A" {
		t.Errorf("assigned: %v", got)
	}
	if got := keys(Filter(tasks, FilterLeadOnly)); len(got) != 1 || got[0] != "B" {
		t.Errorf("lead only: %v", got)
	}
	if got := Filter(tasks, FilterAll); len(got) != 2 {
		t.Errorf("all: %v", got)
	}
}

func TestMerge(t *testing.T) {
	cached := []Task{
		{Source: "cu", ID: "1", Name: "old", Mine: true},
		{Source: "cu", ID: "2", Name: "keep", Mine: true},
	}
	updates := []Task{
		{Source: "cu", ID: "1", Name: "new", Mine: true},
		{Source: "cu", ID: "3", Name: "added", Lead: true},
		{Source: "cu", ID: "4", Name: "not mine", Others: []string{"Malcolm"}},
	}
	got := Merge(cached, updates, "cu", false, now)
	names := map[string]string{}
	for _, t := range got {
		names[t.ID] = t.Name
	}
	if names["1"] != "new" || names["2"] != "keep" || names["3"] != "added" || len(got) != 3 {
		t.Errorf("incremental merge: %v", names)
	}
	if got := Merge(cached, updates[:1], "cu", true, now); len(got) != 1 {
		t.Errorf("full sync should replace: %d", len(got))
	}
}

func TestMergeKeepsOtherSources(t *testing.T) {
	cached := []Task{
		{Source: "cu", ID: "1", Name: "cu one", Mine: true},
		{Source: "cu", ID: "2", Name: "cu two", Mine: true},
		{Source: "gh", ID: "1", Name: "gh one", Mine: true},
	}
	// A full refresh of "cu" drops cu/2 but must keep gh/1, even though it
	// shares the ID "1" with a cu task.
	got := Merge(cached, []Task{{Source: "cu", ID: "1", Name: "cu one v2", Mine: true}}, "cu", true, now)
	refs := map[string]string{}
	for _, t := range got {
		refs[t.Ref()] = t.Name
	}
	want := map[string]string{Ref("cu", "1"): "cu one v2", Ref("gh", "1"): "gh one"}
	if len(refs) != len(want) {
		t.Fatalf("got %v, want %v", refs, want)
	}
	for r, n := range want {
		if refs[r] != n {
			t.Errorf("got %v, want %v", refs, want)
		}
	}
	// An incremental refresh of "gh" doesn't touch cu tasks either.
	got = Merge(got, []Task{{Source: "gh", ID: "1", Name: "gh one v2", Mine: true}}, "gh", false, now)
	if len(got) != 2 {
		t.Errorf("incremental: %+v", got)
	}
}

func TestScratchDefault(t *testing.T) {
	prefixes := []string{"research", "evaluate"}
	types := []string{"Idea"}
	if !WantsScratch(Task{Name: "Research low stock notifications"}, types, prefixes) {
		t.Error("research title")
	}
	if !WantsScratch(Task{Name: "Launch referral", Type: "Idea"}, types, prefixes) {
		t.Error("idea type")
	}
	if WantsScratch(Task{Name: "Promo: checkout", Type: "Coding"}, types, prefixes) {
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
