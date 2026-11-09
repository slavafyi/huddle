package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/slavafyi/huddle/internal/task"
)

// taskView is one source's list tab: Tasks for ClickUp, Issues for GitHub
// issues.
type taskView struct {
	list   list
	filter task.TaskFilter
	names  [3]string // filter labels, from the source's rules
	source string    // the source shown; "" with no sources
	title  string    // tab name
	byRepo bool      // group by repo instead of status
}

func newTaskView(source string, r task.Rules) *taskView {
	v := &taskView{source: source, title: r.ListName, byRepo: r.RepoInList,
		names: task.FilterNames(map[string]task.Rules{source: r})}
	if v.title == "" {
		v.title = "Tasks"
	}
	v.list = list{cols: []column{{"ID", 10}, {"TITLE", 0}, {"CLIENT", 18}, {"DUE", 7}}, empty: "no tasks"}
	if v.byRepo {
		v.list = list{cols: []column{{"ID", 6}, {"TITLE", 0}, {"LABELS", 18}, {"UPDATED", 7}}, empty: "no issues"}
	}
	return v
}

// own returns the tasks of the view's source.
func (v *taskView) own(tasks []task.Task) []task.Task {
	var out []task.Task
	for _, t := range tasks {
		if t.Source == v.source {
			out = append(out, t)
		}
	}
	return out
}

// openTasks returns tasks that are not done.
func openTasks(ts []task.Task) []task.Task {
	var out []task.Task
	for _, t := range ts {
		if !t.IsDone() {
			out = append(out, t)
		}
	}
	return out
}

func (v *taskView) rebuild(tasks []task.Task, now time.Time, agents agentLookup) {
	open := task.Filter(openTasks(v.own(tasks)), v.filter)
	if v.byRepo {
		v.rebuildByRepo(open, now, agents)
		return
	}
	var secs []section
	for _, g := range task.ByStatus(open) {
		s := section{title: g.Name, style: statusStyle(g.Name, false).Render}
		for _, t := range g.Tasks {
			s.rows = append(s.rows, row{key: t.Ref(), cells: []string{
				sFaint.Render(listKey(t)), prioMark(t.Priority) + t.Name + agents.badge(t.Ref()), t.Project, fmtDue(t.Due, now),
			}})
		}
		secs = append(secs, s)
	}
	v.list.setSections(secs)
}

// rebuildByRepo groups tasks by repo (Project), repos by name, newest
// update first inside.
func (v *taskView) rebuildByRepo(tasks []task.Task, now time.Time, agents agentLookup) {
	byRepo := map[string][]task.Task{}
	var repos []string
	for _, t := range tasks {
		if _, ok := byRepo[t.Project]; !ok {
			repos = append(repos, t.Project)
		}
		byRepo[t.Project] = append(byRepo[t.Project], t)
	}
	slices.Sort(repos)
	var secs []section
	for _, r := range repos {
		ts := byRepo[r]
		slices.SortStableFunc(ts, func(a, b task.Task) int { return b.Updated.Compare(a.Updated) })
		s := section{title: r}
		for _, t := range ts {
			upd := "—"
			if !t.Updated.IsZero() {
				upd = ago(now.Sub(t.Updated))
			}
			s.rows = append(s.rows, row{key: t.Ref(), cells: []string{
				sFaint.Render(listKey(t)), prioMark(t.Priority) + t.Name + agents.badge(t.Ref()),
				sFaint.Render(strings.Join(t.Labels, ", ")), sFaint.Render(upd),
			}})
		}
		secs = append(secs, s)
	}
	v.list.setSections(secs)
}

func (v *taskView) subHeader(tasks []task.Task) string {
	var parts []string
	open := openTasks(v.own(tasks))
	for f := task.FilterAll; f <= task.FilterLeadOnly; f++ {
		if v.names[f] == "" {
			continue // the sources have no such filter
		}
		label := fmt.Sprintf("%s %d", v.names[f], len(task.Filter(open, f)))
		if f == v.filter {
			parts = append(parts, sTabOn.Render(label))
		} else {
			parts = append(parts, sFaint.Render(label))
		}
	}
	return strings.Join(parts, "   ")
}

// taskDetail is the detail area for a task.
func taskDetail(t task.Task, now time.Time) []string {
	meta := []string{t.Key, statusStyle(t.Status, t.IsDone()).Render(t.Status), t.Project}
	if t.Type != "" {
		meta = append(meta, t.Type)
	}
	if t.Priority != "" {
		meta = append(meta, t.Priority)
	}
	who := "with " + t.With()
	if t.Lead {
		who += " · lead: you"
	}
	if !t.Due.IsZero() {
		who += " · due " + fmtDue(t.Due, now)
	}
	return []string{sBold.Render(t.Name), strings.Join(meta, " · "), who, sFaint.Render(t.URL)}
}
