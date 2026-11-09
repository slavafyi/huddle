package ui

import (
	"fmt"
	"time"

	"github.com/slavafyi/cuddle/internal/clickup"
)

// todayView is the Today tab.
type todayView struct {
	list   list
	counts [4]int
}

func newTodayView() *todayView {
	return &todayView{list: list{
		cols:  []column{{"", 10}, {"", 0}, {"", 18}, {"", 11}, {"", 16}},
		empty: "nothing to do — enjoy",
	}}
}

var todayNotes = map[string]string{"Waiting on others": "you are lead, task is with someone else"}

func (v *todayView) rebuild(tasks []clickup.Task, me int64, now time.Time, agents agentLookup) {
	var secs []section
	for i, g := range clickup.Today(tasks, me, now) {
		v.counts[i] = len(g.Tasks)
		if len(g.Tasks) == 0 {
			continue
		}
		s := section{title: g.Name, note: todayNotes[g.Name]}
		for _, t := range g.Tasks {
			var when, state string
			switch g.Name {
			case "Waiting on others":
				when = statusStyle(t.Status).Render(t.Status)
				state = sYellow.Render("with " + clickup.With(t, me))
			case "Done this week":
				when = sFaint.Render(t.Done.In(now.Location()).Format("Jan 2"))
				state = sGreen.Render("✓ " + t.Status)
			default:
				when = fmtDue(t.Due, now)
				state = agents.state(t.ID)
				if t.Status == "feedbacks" {
					state = sRed.Render("feedbacks")
				}
			}
			s.rows = append(s.rows, row{key: t.ID, cells: []string{
				sFaint.Render(t.Key()), t.Name, t.ListName, when, state,
			}})
		}
		secs = append(secs, s)
	}
	v.list.setSections(secs)
}

func (v *todayView) subHeader(now time.Time) string {
	return sFaint.Render(fmt.Sprintf("%s · %d in progress · %d up next · %d waiting · %d done this week",
		now.Format("Mon, Jan 2"), v.counts[0], v.counts[1], v.counts[2], v.counts[3]))
}
