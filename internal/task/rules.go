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

// Today group names.
const (
	GroupInProgress = "In progress"
	GroupInReview   = "In review"
	GroupUpNext     = "Up next"
	GroupOpen       = "Open"
	GroupWaiting    = "Waiting on others"
	GroupDone       = "Done this week"
)

// DefaultGroups are the Today groups of a source whose Rules set none.
var DefaultGroups = []string{GroupInProgress, GroupUpNext, GroupWaiting, GroupDone}

// groupOrder is the order of Today groups when sources are mixed.
var groupOrder = []string{GroupInProgress, GroupInReview, GroupUpNext, GroupOpen, GroupWaiting, GroupDone}

// byStatus is the default Classify: ClickUp's status rules.
func byStatus(r Rules, t Task) string {
	switch {
	case t.Mine && slices.Contains(r.Active, t.Status):
		return GroupInProgress
	case t.Mine && slices.Contains(r.Next, t.Status):
		return GroupUpNext
	case t.Lead && !t.Mine && slices.Contains(r.Waiting, t.Status):
		return GroupWaiting
	}
	return ""
}

// Today groups tasks for the Today view, using each task's source rules.
// It returns the groups of every source in rules (empty ones too), in
// groupOrder.
func Today(tasks []Task, rules map[string]Rules, s Signals, now time.Time) []Group {
	var names []string
	for _, r := range rules {
		gs := r.Groups
		if gs == nil {
			gs = DefaultGroups
		}
		for _, n := range gs {
			if !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
	}
	if len(names) == 0 {
		names = DefaultGroups
	}
	rank := func(n string) int {
		if i := slices.Index(groupOrder, n); i >= 0 {
			return i
		}
		return len(groupOrder)
	}
	sort.SliceStable(names, func(i, j int) bool { return rank(names[i]) < rank(names[j]) })
	groups := make([]Group, len(names))
	for i, n := range names {
		groups[i].Name = n
	}
	add := func(name string, t Task) {
		if i := slices.Index(names, name); i >= 0 {
			groups[i].Tasks = append(groups[i].Tasks, t)
		}
	}
	for _, t := range tasks {
		if t.IsDone() {
			if now.Sub(t.Done) <= DoneWindow {
				add(GroupDone, t)
			}
			continue
		}
		r := rules[t.Source]
		if r.Classify != nil {
			add(r.Classify(t, s), t)
		} else {
			add(byStatus(r, t), t)
		}
	}
	for _, g := range groups {
		switch g.Name {
		case GroupInProgress, GroupUpNext:
			sortBy(g.Tasks, byDue)
		case GroupDone:
			sortBy(g.Tasks, func(a, b Task) bool { return a.Done.After(b.Done) })
		default:
			sortBy(g.Tasks, func(a, b Task) bool { return a.Updated.After(b.Updated) })
		}
	}
	return groups
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

// FilterNames labels the Tasks view's filters for the sources in rules:
// their own labels when they agree, else neutral ones.
func FilterNames(rules map[string]Rules) [3]string {
	def := [3]string{FilterAll.String(), FilterAssigned.String(), FilterLeadOnly.String()}
	var out [3]string
	for _, r := range rules {
		n := r.FilterNames
		if n == ([3]string{}) {
			n = def
		}
		switch {
		case out == ([3]string{}):
			out = n
		case out != n:
			return [3]string{"All", "Assigned", "Lead / opened"}
		}
	}
	if out == ([3]string{}) {
		return def
	}
	return out
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
