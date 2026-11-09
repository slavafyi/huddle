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
)

type (
	statusesMsg struct {
		taskID   string
		listID   string
		statuses []clickup.Status
		err      error
	}
	// writeMsg reports a finished ClickUp write. On error, prev (if set) is
	// put back.
	writeMsg struct {
		prev *clickup.Task
		ok   string
		err  error
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
	prev := t
	t.Status, t.StatusType, t.StatusOrder = s.Name, s.Type, s.Order
	t.Done = time.Time{}
	if s.Type == "closed" || s.Type == "done" {
		t.Done = a.now()
	}
	a.putTask(t)
	cu := a.deps.ClickUp
	return a.write(&prev, fmt.Sprintf("%s → %s", t.Key(), s.Name), func(ctx context.Context) error {
		return cu.SetStatus(ctx, t.ID, s.Name)
	})
}

// toggleAssign adds or removes me as assignee, on screen at once.
func (a *App) toggleAssign(t clickup.Task) tea.Cmd {
	prev := t
	me := a.cache.Me
	add := !t.AssignedTo(me.ID)
	if add {
		t.Assignees = append(append([]clickup.User(nil), t.Assignees...), me)
	} else {
		var rest []clickup.User
		for _, u := range t.Assignees {
			if u.ID != me.ID {
				rest = append(rest, u)
			}
		}
		t.Assignees = rest
	}
	a.putTask(t)
	msg := t.Key() + ": assigned to you"
	if !add {
		msg = t.Key() + ": unassigned you"
	}
	cu := a.deps.ClickUp
	return a.write(&prev, msg, func(ctx context.Context) error {
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
		return a.write(nil, t.Key()+": comment added", func(ctx context.Context) error {
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
func (a *App) write(prev *clickup.Task, ok string, fn func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return writeMsg{prev: prev, ok: ok, err: fn(ctx)}
	}
}

func (a *App) onWrite(m writeMsg) tea.Cmd {
	if m.err != nil {
		if m.prev != nil {
			a.putTask(*m.prev)
		}
		a.flash = sRed.Render(m.err.Error())
		return nil
	}
	a.flash = sGreen.Render(m.ok)
	return a.saveCache()
}

// putTask replaces a task in the cache by ID and rebuilds the views.
func (a *App) putTask(t clickup.Task) {
	for i := range a.cache.Tasks {
		if a.cache.Tasks[i].ID == t.ID {
			a.cache.Tasks[i] = t
		}
	}
	a.rebuild()
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
