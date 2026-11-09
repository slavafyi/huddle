package clickup

import (
	"sort"
	"strings"
	"time"
)

// DoneWindow is how long done tasks stay in "Done this week".
const DoneWindow = 7 * 24 * time.Hour

// Group is a named list of tasks.
type Group struct {
	Name  string
	Tasks []Task
}

// Relevant reports whether a task belongs in cuddle: I'm an assignee or the
// lead, and it is open or was done in the last 7 days.
func Relevant(t Task, me int64, now time.Time) bool {
	if !t.AssignedTo(me) && !t.LeadIs(me) {
		return false
	}
	return !t.IsDone() || now.Sub(t.Done) <= DoneWindow
}

// With names who has the task now: "you", the assignees' names, or "nobody".
func With(t Task, me int64) string {
	if t.AssignedTo(me) {
		return "you"
	}
	if len(t.Assignees) == 0 {
		return "nobody"
	}
	names := make([]string, len(t.Assignees))
	for i, u := range t.Assignees {
		names[i] = u.Username
	}
	return strings.Join(names, ", ")
}

var waitingStatuses = map[string]bool{"to review": true, "pending": true, "to publish": true}

// Today groups tasks for the Today view.
func Today(tasks []Task, me int64, now time.Time) []Group {
	var g [4][]Task
	for _, t := range tasks {
		switch {
		case t.IsDone():
			if now.Sub(t.Done) <= DoneWindow {
				g[3] = append(g[3], t)
			}
		case t.AssignedTo(me) && (t.Status == "in progress" || t.Status == "feedbacks"):
			g[0] = append(g[0], t)
		case t.AssignedTo(me) && t.Status == "to start":
			g[1] = append(g[1], t)
		case t.LeadIs(me) && !t.AssignedTo(me) && waitingStatuses[t.Status]:
			g[2] = append(g[2], t)
		}
	}
	sortBy(g[0], byDue)
	sortBy(g[1], byDue)
	sortBy(g[2], func(a, b Task) bool { return a.Updated.After(b.Updated) })
	sortBy(g[3], func(a, b Task) bool { return a.Done.After(b.Done) })
	return []Group{
		{"In progress", g[0]},
		{"Up next", g[1]},
		{"Waiting on others", g[2]},
		{"Done this week", g[3]},
	}
}

// ByStatus groups tasks by status in workflow order (the status order
// index), each group sorted by due date. Empty groups are left out.
func ByStatus(tasks []Task) []Group {
	idx := map[string]int{}
	var groups []Group
	order := map[string]int{}
	for _, t := range tasks {
		i, ok := idx[t.Status]
		if !ok {
			i = len(groups)
			idx[t.Status] = i
			groups = append(groups, Group{Name: t.Status})
			order[t.Status] = t.StatusOrder
		}
		groups[i].Tasks = append(groups[i].Tasks, t)
	}
	sort.SliceStable(groups, func(a, b int) bool { return order[groups[a].Name] < order[groups[b].Name] })
	for _, g := range groups {
		sortBy(g.Tasks, byDue)
	}
	return groups
}

// TaskFilter selects which of my tasks the Tasks view shows.
type TaskFilter int

// Task filters, in the order the Tasks view cycles through them.
const (
	FilterAll      TaskFilter = iota // assigned to me or I'm lead
	FilterAssigned                   // assigned to me
	FilterLeadOnly                   // I'm lead but not assigned
)

// String names the filter for the UI.
func (f TaskFilter) String() string {
	return [...]string{"Mine + Lead", "Assigned", "Lead only"}[f]
}

// Filter applies f to tasks.
func Filter(tasks []Task, me int64, f TaskFilter) []Task {
	var out []Task
	for _, t := range tasks {
		switch f {
		case FilterAssigned:
			if !t.AssignedTo(me) {
				continue
			}
		case FilterLeadOnly:
			if t.AssignedTo(me) || !t.LeadIs(me) {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// Merge combines cached tasks with fetched ones. A full fetch replaces the
// cache. An incremental fetch replaces tasks by ID and adds new ones. Either
// way, only relevant tasks are kept.
func Merge(cached, fetched []Task, full bool, me int64, now time.Time) []Task {
	byID := map[string]Task{}
	if !full {
		for _, t := range cached {
			byID[t.ID] = t
		}
	}
	for _, t := range fetched {
		byID[t.ID] = t
	}
	out := make([]Task, 0, len(byID))
	for _, t := range byID {
		if Relevant(t, me, now) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// WantsScratch reports whether a task should default to a scratch folder:
// its type is one of types, or its title starts with one of prefixes.
func WantsScratch(t Task, types, prefixes []string) bool {
	for _, ty := range types {
		if strings.EqualFold(t.TypeName, ty) {
			return true
		}
	}
	name := strings.ToLower(strings.TrimSpace(t.Name))
	for _, p := range prefixes {
		if strings.HasPrefix(name, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

func byDue(a, b Task) bool {
	switch {
	case a.Due.IsZero():
		return false
	case b.Due.IsZero():
		return true
	}
	return a.Due.Before(b.Due)
}

func sortBy(ts []Task, less func(a, b Task) bool) {
	sort.SliceStable(ts, func(i, j int) bool { return less(ts[i], ts[j]) })
}
