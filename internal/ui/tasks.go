package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/slavafyi/huddle/internal/task"
)

// taskView is the Tasks tab.
type taskView struct {
	list   list
	filter task.TaskFilter
	names  [3]string // filter labels, from the sources' rules
}

func newTaskView() *taskView {
	return &taskView{list: list{
		cols:  []column{{"ID", 10}, {"TITLE", 0}, {"CLIENT", 18}, {"DUE", 7}},
		empty: "no tasks",
	}}
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
	var secs []section
	for _, g := range task.ByStatus(task.Filter(openTasks(tasks), v.filter)) {
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

func (v *taskView) subHeader(tasks []task.Task) string {
	var parts []string
	open := openTasks(tasks)
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
