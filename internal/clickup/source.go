package clickup

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/slavafyi/huddle/internal/task"
)

// SourceName is ClickUp's task.Source name.
const SourceName = "clickup"

// Source is ClickUp as a task source.
type Source struct {
	Client Client
	// InProgress is the status a started task moves to.
	InProgress string
	// NoLead turns lead features off: no lead field was found.
	NoLead bool
	// Detector, when its IDs are detected, finds them again after a fetch
	// fails with a client error, and the fetch is tried once more.
	Detector *Detector

	mu   sync.Mutex
	note string
	warn bool
}

var _ task.Source = (*Source)(nil)

// Name implements task.Source.
func (s *Source) Name() string { return SourceName }

// Rules are ClickUp's status words (SPEC §4.2, §4.4).
func (s *Source) Rules() task.Rules {
	r := task.Rules{
		Active:    []string{"in progress", "feedbacks"},
		Next:      []string{"to start"},
		Waiting:   []string{"to review", "pending", "to publish"},
		Alert:     []string{"feedbacks"},
		Startable: []string{"to plan", "to start"},
		MoveTo:    s.InProgress,
	}
	if s.NoLead {
		r.Waiting = nil
		r.Groups = []string{task.GroupInProgress, task.GroupUpNext, task.GroupDone}
		r.FilterNames = [3]string{"Assigned", "", ""}
	}
	return r
}

// Me returns my ClickUp user ID.
func (s *Source) Me(ctx context.Context) (int64, error) {
	u, err := s.Client.Me(ctx)
	return u.ID, err
}

// Fetch implements task.Source.
func (s *Source) Fetch(ctx context.Context, me int64, since, now time.Time) ([]task.Task, error) {
	ts, err := s.Client.MyTasks(ctx, me, since, now)
	if err != nil && s.Detector != nil && s.Detector.Auto() && isClientError(err) {
		warning, changed, derr := s.Detector.Redetect(ctx)
		if derr != nil {
			return nil, fmt.Errorf("%w (finding the ClickUp workspace and lead field again: %v)", err, derr)
		}
		if changed {
			s.SetNote(warning, true)
			ts, err = s.Client.MyTasks(ctx, me, since, now)
		}
	}
	if err != nil {
		return nil, err
	}
	out := make([]task.Task, len(ts))
	for i, t := range ts {
		out[i] = ToTask(t, me)
	}
	return out, nil
}

// SetNote keeps a message for the flash line until TakeNote; warn marks a
// warning rather than a faint note.
func (s *Source) SetNote(text string, warn bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.note, s.warn = text, warn
}

// TakeNote returns the kept message once.
func (s *Source) TakeNote() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text, warn := s.note, s.warn
	s.note, s.warn = "", false
	return text, warn
}

// Statuses returns the statuses of the task's list.
func (s *Source) Statuses(ctx context.Context, t task.Task) ([]task.Status, error) {
	st, err := s.Client.Statuses(ctx, t.ListID)
	if err != nil {
		return nil, err
	}
	out := make([]task.Status, len(st))
	for i, x := range st {
		out[i] = task.Status{Name: x.Name, Order: x.Order, Done: x.Type == "closed" || x.Type == "done"}
	}
	return out, nil
}

// SetStatus implements task.Source.
func (s *Source) SetStatus(ctx context.Context, t task.Task, status string) error {
	return s.Client.SetStatus(ctx, t.ID, status)
}

// AddComment implements task.Source.
func (s *Source) AddComment(ctx context.Context, t task.Task, text string) error {
	return s.Client.AddComment(ctx, t.ID, text)
}

// SetAssignee implements task.Source.
func (s *Source) SetAssignee(ctx context.Context, t task.Task, me int64, add bool) error {
	return s.Client.SetAssignee(ctx, t.ID, me, add)
}

// ToTask converts a ClickUp task, as seen by user me.
func ToTask(t Task, me int64) task.Task {
	out := task.Task{
		Source: SourceName, ID: t.ID, Key: t.Key(), Name: t.Name, URL: t.URL,
		Status: t.Status, StatusOrder: t.StatusOrder, ListID: t.ListID, Project: t.ListName,
		Type: t.TypeName, Priority: t.Priority, Mine: t.AssignedTo(me), Lead: t.LeadIs(me),
		Due: t.Due, Updated: t.Updated, Done: t.Done,
	}
	for _, u := range t.Assignees {
		if u.ID != me {
			out.Others = append(out.Others, u.Username)
		}
	}
	// A PR links to the task by its ID, also as CU-<id>, or its custom ID.
	id := strings.ToLower(t.ID)
	out.Links = []string{id, "cu-" + id}
	if t.CustomID != "" {
		out.Links = append(out.Links, strings.ToLower(t.CustomID))
	}
	return out
}
