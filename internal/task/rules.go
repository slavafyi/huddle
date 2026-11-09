package task

import (
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

// DoneWindow is how long done tasks stay in "Done this week".
const DoneWindow = 7 * 24 * time.Hour

// Group is a named list of tasks.
type Group struct {
	Name  string
	Tasks []Task
}

// Relevant reports whether a task belongs in huddle: I'm an assignee or the
// lead, and it is open or was done in the last 7 days.
func Relevant(t Task, now time.Time) bool {
	if !t.Mine && !t.Lead {
		return false
	}
	return !t.IsDone() || now.Sub(t.Done) <= DoneWindow
}

// Today groups tasks for the Today view, using each task's source rules.
func Today(tasks []Task, rules map[string]Rules, now time.Time) []Group {
	var g [4][]Task
	for _, t := range tasks {
		r := rules[t.Source]
		switch {
		case t.IsDone():
			if now.Sub(t.Done) <= DoneWindow {
				g[3] = append(g[3], t)
			}
		case t.Mine && slices.Contains(r.Active, t.Status):
			g[0] = append(g[0], t)
		case t.Mine && slices.Contains(r.Next, t.Status):
			g[1] = append(g[1], t)
		case t.Lead && !t.Mine && slices.Contains(r.Waiting, t.Status):
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
func Filter(tasks []Task, f TaskFilter) []Task {
	var out []Task
	for _, t := range tasks {
		switch f {
		case FilterAssigned:
			if !t.Mine {
				continue
			}
		case FilterLeadOnly:
			if t.Mine || !t.Lead {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// Merge combines cached tasks with tasks fetched from one source. A full
// fetch replaces that source's cached tasks. An incremental fetch replaces
// tasks by ID and adds new ones. Other sources' tasks are kept as they are.
// Either way, only relevant tasks of the source are kept.
func Merge(cached, fetched []Task, source string, full bool, now time.Time) []Task {
	var out []Task
	byID := map[string]Task{}
	for _, t := range cached {
		switch {
		case t.Source != source:
			out = append(out, t)
		case !full:
			byID[t.ID] = t
		}
	}
	for _, t := range fetched {
		byID[t.ID] = t
	}
	for _, t := range byID {
		if Relevant(t, now) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Source != out[b].Source {
			return out[a].Source < out[b].Source
		}
		return out[a].ID < out[b].ID
	})
	return out
}

// WantsScratch reports whether a task should default to a scratch folder:
// its type is one of types, or its title starts with one of prefixes.
func WantsScratch(t Task, types, prefixes []string) bool {
	for _, ty := range types {
		if strings.EqualFold(t.Type, ty) {
			return true
		}
	}
	words := strings.FieldsFunc(strings.ToLower(t.Name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) == 0 {
		return false
	}
	for _, p := range prefixes {
		if words[0] == strings.ToLower(p) {
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
