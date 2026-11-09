package ui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/slavafyi/cuddle/internal/clickup"
)

// mode is what keys currently go to.
type mode int

const (
	modeNormal mode = iota
	modeStatus
	modeComment
	modeStart
	modeConfirm
)

type (
	statusesMsg struct {
		taskID   string
		listID   string
		statuses []clickup.Status
		err      error
	}
	// writeMsg reports a finished ClickUp write for op (0: no screen change).
	writeMsg struct {
		op  uint64
		ok  string
		err error
	}
)

// picker is the status picker state.
type picker struct {
	taskID   string
	statuses []clickup.Status
	cursor   int
}

// openStatusPicker fetches the list's statuses (once per list) and opens
// the picker.
func (a *App) openStatusPicker(t clickup.Task) tea.Cmd {
	if st, ok := a.statuses[t.ListID]; ok {
		a.showPicker(t.ID, st)
		return nil
	}
	cu := a.deps.ClickUp
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		st, err := cu.Statuses(ctx, t.ListID)
		return statusesMsg{taskID: t.ID, listID: t.ListID, statuses: st, err: err}
	}
}

func (a *App) showPicker(taskID string, st []clickup.Status) {
	if len(st) == 0 {
		a.flash = sYellow.Render("this list has no statuses")
		return
	}
	t, _ := a.taskByID(taskID)
	p := picker{taskID: taskID, statuses: st}
	for i, s := range st {
		if s.Name == t.Status {
			p.cursor = i
		}
	}
	a.picker = p
	a.mode = modeStatus
}

func (a *App) pickerKey(k string) tea.Cmd {
	p := &a.picker
	switch k {
	case "esc", "q":
		a.mode = modeNormal
	case "j", "down":
		p.cursor = (p.cursor + 1) % len(p.statuses)
	case "k", "up":
		p.cursor = (p.cursor - 1 + len(p.statuses)) % len(p.statuses)
	case "enter":
		return a.applyStatus(p.statuses[p.cursor])
	default:
		if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
			if i := int(k[0] - '1'); i < len(p.statuses) {
				return a.applyStatus(p.statuses[i])
			}
		}
	}
	return nil
}

// applyStatus changes the status on screen at once, then in ClickUp.
func (a *App) applyStatus(s clickup.Status) tea.Cmd {
	a.mode = modeNormal
	t, ok := a.taskByID(a.picker.taskID)
	if !ok || t.Status == s.Name {
		return nil
	}
	id := a.newOp(t.ID, "status", setStatus(s, a.now()), setStatus(clickup.Status{Name: t.Status, Type: t.StatusType, Order: t.StatusOrder}, t.Done))
	cu := a.deps.ClickUp
	return a.write(id, fmt.Sprintf("%s → %s", t.Key(), s.Name), func(ctx context.Context) error {
		return cu.SetStatus(ctx, t.ID, s.Name)
	})
}

// setStatus returns a change that sets a task's status. done is used when
// the status is a done/closed one.
func setStatus(s clickup.Status, done time.Time) func(*clickup.Task) {
	return func(t *clickup.Task) {
		t.Status, t.StatusType, t.StatusOrder = s.Name, s.Type, s.Order
		t.Done = time.Time{}
		if s.Type == "closed" || s.Type == "done" {
			t.Done = done
		}
	}
}

// setAssigned returns a change that adds (or removes) me as assignee.
func setAssigned(me clickup.User, on bool) func(*clickup.Task) {
	return func(t *clickup.Task) {
		var rest []clickup.User
		for _, u := range t.Assignees {
			if u.ID != me.ID {
				rest = append(rest, u)
			}
		}
		if on {
			rest = append(rest, me)
		}
		t.Assignees = rest
	}
}

// toggleAssign adds or removes me as assignee, on screen at once.
func (a *App) toggleAssign(t clickup.Task) tea.Cmd {
	me := a.cache.Me
	add := !t.AssignedTo(me.ID)
	id := a.newOp(t.ID, "assign", setAssigned(me, add), setAssigned(me, !add))
	msg := t.Key() + ": assigned to you"
	if !add {
		msg = t.Key() + ": unassigned you"
	}
	cu := a.deps.ClickUp
	return a.write(id, msg, func(ctx context.Context) error {
		return cu.SetAssignee(ctx, t.ID, me.ID, add)
	})
}

func (a *App) commentKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		a.mode, a.input = modeNormal, ""
		a.flash = ""
	case "enter":
		text := a.input
		a.mode, a.input = modeNormal, ""
		a.flash = ""
		t, ok := a.taskByID(a.commentTask)
		if !ok || text == "" {
			return nil
		}
		cu := a.deps.ClickUp
		return a.write(0, t.Key()+": comment added", func(ctx context.Context) error {
			return cu.AddComment(ctx, t.ID, text)
		})
	case "backspace":
		if r := []rune(a.input); len(r) > 0 {
			a.input = string(r[:len(r)-1])
		}
	default:
		a.input += msg.Text
	}
	return nil
}

// write runs a ClickUp write in the background.
func (a *App) write(op uint64, ok string, fn func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return writeMsg{op: op, ok: ok, err: fn(ctx)}
	}
}

func (a *App) onWrite(m writeMsg) tea.Cmd {
	a.finishOp(m.op, m.err == nil)
	if m.err != nil {
		a.flash = sRed.Render(m.err.Error())
		return nil
	}
	a.flash = sGreen.Render(m.ok)
	return tea.Batch(a.saveCache(), a.refreshAfterWrite())
}

func (a *App) renderPicker() []string {
	t, _ := a.taskByID(a.picker.taskID)
	lines := []string{sBold.Render("Status") + sFaint.Render(" · "+t.Key())}
	for i, s := range a.picker.statuses {
		mark := "  "
		if i == a.picker.cursor {
			mark = "› "
		}
		line := fmt.Sprintf("%s%d %s %s", mark, i+1, statusStyle(s.Name).Render("■"), s.Name)
		if s.Name == t.Status {
			line += sFaint.Render(" (current)")
		}
		if i == a.picker.cursor {
			line = sBold.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, sFaint.Render("enter apply · 1–9 pick · esc cancel"))
	return box(lines, 44)
}
