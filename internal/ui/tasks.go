package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/slavafyi/cuddle/internal/clickup"
)

// taskView is the Tasks tab.
type taskView struct {
	list   list
	filter clickup.TaskFilter
}

func newTaskView() *taskView {
	return &taskView{list: list{
		cols:  []column{{"ID", 10}, {"TITLE", 0}, {"CLIENT", 18}, {"DUE", 7}, {"WITH", 12}},
		empty: "no tasks",
	}}
}

// openTasks returns tasks that are not done.
func openTasks(ts []clickup.Task) []clickup.Task {
	var out []clickup.Task
	for _, t := range ts {
		if !t.IsDone() {
			out = append(out, t)
		}
	}
	return out
}

func (v *taskView) rebuild(tasks []clickup.Task, me int64, now time.Time, agents agentLookup) {
	var secs []section
	for _, g := range clickup.ByStatus(clickup.Filter(openTasks(tasks), me, v.filter)) {
		s := section{title: g.Name, style: statusStyle(g.Name).Render}
		for _, t := range g.Tasks {
			s.rows = append(s.rows, row{key: t.ID, cells: []string{
				sFaint.Render(t.Key()), t.Name + agents.badge(t.ID), t.ListName, fmtDue(t.Due, now), fmtWith(t, me),
			}})
		}
		secs = append(secs, s)
	}
	v.list.setSections(secs)
}

func (v *taskView) subHeader(tasks []clickup.Task, me int64) string {
	var parts []string
	open := openTasks(tasks)
	for f := clickup.FilterAll; f <= clickup.FilterLeadOnly; f++ {
		label := fmt.Sprintf("%s %d", f, len(clickup.Filter(open, me, f)))
		if f == v.filter {
			parts = append(parts, sTabOn.Render(label))
		} else {
			parts = append(parts, sFaint.Render(label))
		}
	}
	return strings.Join(parts, "   ") + "\n" +
		sFaint.Render("assigned to you, or you are lead · grouped by status · t switch filter")
}

func fmtWith(t clickup.Task, me int64) string {
	w := clickup.With(t, me)
	switch w {
	case "you", "nobody":
		return sFaint.Render(w)
	}
	return sYellow.Render(w)
}

// taskDetail is the detail area for a task.
func taskDetail(t clickup.Task, me int64, now time.Time) []string {
	meta := []string{t.Key(), statusStyle(t.Status).Render(t.Status), t.ListName}
	if t.TypeName != "" {
		meta = append(meta, t.TypeName)
	}
	if t.Priority != "" {
		meta = append(meta, t.Priority)
	}
	who := "with " + clickup.With(t, me)
	if t.LeadIs(me) {
		who += " · lead: you"
	}
	if !t.Due.IsZero() {
		who += " · due " + fmtDue(t.Due, now)
	}
	return []string{sBold.Render(t.Name), strings.Join(meta, " · "), who, sFaint.Render(t.URL)}
}
