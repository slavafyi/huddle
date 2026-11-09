// Package clickup reads and writes ClickUp tasks and holds the rules that
// decide which tasks show where.
package clickup

import (
	"context"
	"strings"
	"time"
)

// User is a ClickUp member.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Task is the part of a ClickUp task cuddle uses. Zero times mean "not set".
type Task struct {
	ID          string    `json:"id"`
	CustomID    string    `json:"custom_id,omitempty"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Status      string    `json:"status"`
	StatusType  string    `json:"status_type"`
	StatusOrder int       `json:"status_order"`
	ListID      string    `json:"list_id"`
	ListName    string    `json:"list_name"`
	TypeName    string    `json:"type_name,omitempty"`
	Priority    string    `json:"priority,omitempty"`
	Assignees   []User    `json:"assignees,omitempty"`
	Leads       []User    `json:"leads,omitempty"`
	Due         time.Time `json:"due,omitzero"`
	Updated     time.Time `json:"updated,omitzero"`
	Done        time.Time `json:"done,omitzero"`
}

// Key is the task ID shown to people: the custom ID (e.g. DEV-7808) when the
// workspace uses them, else ClickUp's own ID. Never assume a prefix.
func (t Task) Key() string {
	if t.CustomID != "" {
		return t.CustomID
	}
	return t.ID
}

// KeyLower is Key in lowercase, for branch and folder names.
func (t Task) KeyLower() string { return strings.ToLower(t.Key()) }

// AssignedTo reports whether user id is an assignee.
func (t Task) AssignedTo(id int64) bool { return hasUser(t.Assignees, id) }

// LeadIs reports whether user id is in the Lead Assignee field.
func (t Task) LeadIs(id int64) bool { return hasUser(t.Leads, id) }

// IsDone reports whether the task is closed or in a done status.
func (t Task) IsDone() bool { return !t.Done.IsZero() }

func hasUser(us []User, id int64) bool {
	for _, u := range us {
		if u.ID == id {
			return true
		}
	}
	return false
}

// Status is one status a list allows.
type Status struct {
	Name  string `json:"status"`
	Type  string `json:"type"`
	Order int    `json:"orderindex"`
}

// Client is what the rest of cuddle needs from ClickUp.
type Client interface {
	Me(ctx context.Context) (User, error)
	// MyTasks returns tasks assigned to me or where I'm lead. A zero since
	// means a full fetch: open tasks plus tasks done in the last 7 days.
	// Otherwise it returns only tasks updated after since.
	MyTasks(ctx context.Context, me int64, since, now time.Time) ([]Task, error)
	Statuses(ctx context.Context, listID string) ([]Status, error)
	SetStatus(ctx context.Context, taskID, status string) error
	AddComment(ctx context.Context, taskID, text string) error
	SetAssignee(ctx context.Context, taskID string, user int64, add bool) error
}
