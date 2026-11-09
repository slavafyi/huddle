package ui

import (
	"encoding/json"
	"fmt"
	"sort"

	tea "charm.land/bubbletea/v2"

	"github.com/slavafyi/cuddle/internal/clickup"
	"github.com/slavafyi/cuddle/internal/store"
)

// op is an optimistic change to one task, shown before ClickUp confirms it.
// It is re-applied over refresh results until a refresh that started after
// the write finished has come back, so an older refresh can't undo it.
type op struct {
	taskID string
	apply  func(*clickup.Task)
	undo   func(*clickup.Task)
	done   bool
	doneAt uint64 // value of App.completed when the write finished
}

// newOp applies a change on screen at once and remembers it.
func (a *App) newOp(taskID string, apply, undo func(*clickup.Task)) uint64 {
	a.opID++
	a.ops[a.opID] = &op{taskID: taskID, apply: apply, undo: undo}
	a.editTask(taskID, apply)
	return a.opID
}

// finishOp records the write's result: on failure only this change is
// undone; on success the change is kept until a newer refresh confirms it.
func (a *App) finishOp(id uint64, ok bool) {
	o := a.ops[id]
	if o == nil {
		return
	}
	if !ok {
		a.editTask(o.taskID, o.undo)
		delete(a.ops, id)
		return
	}
	a.completed++
	o.done, o.doneAt = true, a.completed
}

// reapplyOps puts pending changes back over fresh data from a refresh that
// started when a.completed was seq.
func (a *App) reapplyOps(seq uint64) {
	ids := make([]uint64, 0, len(a.ops))
	for id := range a.ops {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		o := a.ops[id]
		if o.done && o.doneAt <= seq {
			delete(a.ops, id)
			continue
		}
		a.editTask(o.taskID, o.apply)
	}
}

// editTask changes one task. It copies the task slice first, so a snapshot
// being saved in the background never sees the change.
func (a *App) editTask(id string, fn func(*clickup.Task)) {
	tasks := append([]clickup.Task(nil), a.cache.Tasks...)
	for i := range tasks {
		if tasks[i].ID == id {
			fn(&tasks[i])
		}
	}
	a.cache.Tasks = tasks
	a.rebuild()
}

// refreshAfterWrite asks for an incremental refresh, or a second one when a
// refresh is already running (its data may predate the write).
func (a *App) refreshAfterWrite() tea.Cmd {
	if a.loadingTasks {
		a.refreshAgain = true
		return nil
	}
	return a.refreshTasks(false)
}

// snapshot encodes v now, on the UI goroutine, and writes it in the
// background without letting an older snapshot win.
func (a *App) snapshot(path, what string, v any) tea.Cmd {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return func() tea.Msg { return errMsg{fmt.Errorf("save %s: %w", what, err)} }
	}
	a.saveGen++
	gen, w := a.saveGen, a.writer
	return func() tea.Msg {
		if err := w.Write(path, data, gen); err != nil {
			return errMsg{fmt.Errorf("save %s: %w", what, err)}
		}
		return savedMsg{}
	}
}

func (a *App) saveCache() tea.Cmd {
	c := a.cache
	c.Version = store.CacheVersion
	return a.snapshot(a.deps.CachePath, "cache", c)
}

func (a *App) saveState() tea.Cmd {
	s := a.state
	s.Version = store.StateVersion
	return a.snapshot(a.deps.StatePath, "state", s)
}
