package ui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/slavafyi/huddle/internal/task"
)

// mode is what keys currently go to.
type mode int

const (
	modeNormal mode = iota
	modeStatus
	modeComment
	modeStart
	modeConfirm
	modeHelp
	modeProfile
)

type (
	statusesMsg struct {
		ref      string // task.Ref of the task
		list     string // key in App.statuses
		statuses []task.Status
		err      error
	}
	// writeMsg reports a finished task write for op (0: no screen change).
	writeMsg struct {
		op  uint64
		ok  string
		err error
	}
)

// picker is the status picker state.
type picker struct {
	ref      string // task.Ref of the task
	statuses []task.Status
	cursor   int
}

// openStatusPicker fetches the list's statuses (once per list) and opens
// the picker.
func (a *App) openStatusPicker(t task.Task, src task.Source) tea.Cmd {
	list := task.Ref(t.Source, t.ListID)
	if st, ok := a.statuses[list]; ok {
		a.showPicker(t.Ref(), st)
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		st, err := src.Statuses(ctx, t)
		return statusesMsg{ref: t.Ref(), list: list, statuses: st, err: err}
	}
}

func (a *App) showPicker(ref string, st []task.Status) {
	if len(st) == 0 {
		a.flash = sYellow.Render("this list has no statuses")
		return
	}
	t, _ := a.taskByRef(ref)
	p := picker{ref: ref, statuses: st}
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

// applyStatus changes the status on screen at once, then in the source.
func (a *App) applyStatus(s task.Status) tea.Cmd {
	a.mode = modeNormal
	t, ok := a.taskByRef(a.picker.ref)
	src := a.source(t.Source)
	if !ok || src == nil || t.Status == s.Name {
		return nil
	}
	id := a.newOp(t.Ref(), "status", setStatus(s, a.now()), setStatus(currentStatus(t), t.Done))
	return a.write(id, fmt.Sprintf("%s → %s", t.Key, s.Name), func(ctx context.Context) error {
		return src.SetStatus(ctx, t, s.Name)
	})
}

// currentStatus is a task's status, for undoing a change.
func currentStatus(t task.Task) task.Status {
	return task.Status{Name: t.Status, Order: t.StatusOrder, Done: t.IsDone()}
}

// setStatus returns a change that sets a task's status. done is used when
// the status is a done/closed one.
func setStatus(s task.Status, done time.Time) func(*task.Task) {
	return func(t *task.Task) {
		t.Status, t.StatusOrder = s.Name, s.Order
		t.Done = time.Time{}
		if s.Done {
			t.Done = done
		}
	}
}

// setAssigned returns a change that adds (or removes) me as assignee.
func setAssigned(on bool) func(*task.Task) {
	return func(t *task.Task) { t.Mine = on }
}

// toggleAssign adds or removes me as assignee, on screen at once.
func (a *App) toggleAssign(t task.Task, src task.Source) tea.Cmd {
	me := a.cache.Me[t.Source]
	add := !t.Mine
	id := a.newOp(t.Ref(), "assign", setAssigned(add), setAssigned(!add))
	msg := t.Key + ": assigned to you"
	if !add {
		msg = t.Key + ": unassigned you"
	}
	return a.write(id, msg, func(ctx context.Context) error {
		return src.SetAssignee(ctx, t, me, add)
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
		t, ok := a.taskByRef(a.commentTask)
		src := a.source(t.Source)
		if !ok || src == nil || text == "" {
			return nil
		}
		return a.write(0, t.Key+": comment added", func(ctx context.Context) error {
			return src.AddComment(ctx, t, text)
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

// write runs a task write in the background.
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
	t, _ := a.taskByRef(a.picker.ref)
	lines := []string{sBold.Render("Status") + sFaint.Render(" · "+t.Key)}
	for i, s := range a.picker.statuses {
		mark := "  "
		if i == a.picker.cursor {
			mark = "› "
		}
		line := fmt.Sprintf("%s%d %s %s", mark, i+1, statusStyle(s.Name, s.Done).Render("■"), s.Name)
		if s.Name == t.Status {
			line += sFaint.Render(" (current)")
		}
		if i == a.picker.cursor {
			line = selected(line, 44-4)
		}
		lines = append(lines, line)
	}
	lines = append(lines, sFaint.Render("enter apply · 1–9 pick · esc cancel"))
	return box(lines, 44)
}
