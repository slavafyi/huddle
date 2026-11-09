package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/slavafyi/huddle/internal/task"
)

// todayView is the Today tab.
type todayView struct {
	list   list
	counts []groupCount
}

// groupCount is one Today group's size, for the summary line.
type groupCount struct {
	name string
	n    int
}

func newTodayView() *todayView {
	return &todayView{list: list{
		cols:  []column{{"", 10}, {"", 0}, {"", 18}, {"", 11}, {"", 16}},
		empty: "nothing to do — enjoy",
	}}
}

var todayNotes = map[string]string{"Waiting on others": "you are lead, task is with someone else"}

func (v *todayView) rebuild(tasks []task.Task, rules map[string]task.Rules, sig task.Signals, now time.Time, agents agentLookup) {
	var secs []section
	v.counts = nil
	for _, g := range task.Today(tasks, rules, sig, now) {
		v.counts = append(v.counts, groupCount{g.Name, len(g.Tasks)})
		if len(g.Tasks) == 0 {
			continue
		}
		s := section{title: g.Name, note: todayNotes[g.Name]}
		for _, t := range g.Tasks {
			var when, state string
			switch g.Name {
			case task.GroupWaiting:
				when = statusStyle(t.Status, false).Render(t.Status) // who has it: see the detail area
			case task.GroupDone:
				when = sFaint.Render(t.Done.In(now.Location()).Format("Jan 2"))
				state = sGreen.Render("✓ " + t.Status)
			default:
				when = fmtDue(t.Due, now)
				state = agents.state(t.Ref())
				if slices.Contains(rules[t.Source].Alert, t.Status) {
					state = sRed.Render(t.Status)
				}
			}
			s.rows = append(s.rows, row{key: t.Ref(), cells: []string{
				sFaint.Render(listKey(t)), prioMark(t.Priority) + t.Name, t.Project, when, state,
			}})
		}
		secs = append(secs, s)
	}
	v.list.setSections(secs)
}

func (v *todayView) subHeader(now time.Time) string {
	parts := []string{now.Format("Mon, Jan 2")}
	for _, c := range v.counts {
		name := strings.ToLower(c.name)
		if c.name == task.GroupWaiting {
			name = "waiting"
		}
		parts = append(parts, fmt.Sprintf("%d %s", c.n, name))
	}
	return sFaint.Render(strings.Join(parts, " · "))
}
