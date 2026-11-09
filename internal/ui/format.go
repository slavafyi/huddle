package ui

import (
	"fmt"
	"time"
)

// fmtDue shows a due date: red when overdue, yellow for today, "—" if none.
func fmtDue(due, now time.Time) string {
	if due.IsZero() {
		return sFaint.Render("—")
	}
	d := due.In(now.Location())
	y1, m1, d1 := d.Date()
	y2, m2, d2 := now.Date()
	today := time.Date(y2, m2, d2, 0, 0, 0, 0, now.Location())
	switch day := time.Date(y1, m1, d1, 0, 0, 0, 0, now.Location()); {
	case day.Equal(today):
		return sYellow.Render("today")
	case day.Before(today):
		return sRed.Render(d.Format("Jan 2"))
	}
	return d.Format("Jan 2")
}

// ago formats a duration as "40s", "12m" or "3h".
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
