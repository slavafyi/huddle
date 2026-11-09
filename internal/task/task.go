// Package task is huddle's source-neutral task: the type the views show,
// the Source interface each task system (ClickUp, …) implements, and the
// rules that decide which tasks show where.
package task

import (
	"context"
	"strings"
	"time"
	"unicode"
)

// Task is one task from a source. Zero times mean "not set".
type Task struct {
	Source      string    `json:"source"` // name of the Source it came from
	ID          string    `json:"id"`     // the source's own ID
	Key         string    `json:"key"`    // ID shown to people, e.g. DEV-7808
	Name        string    `json:"name"`   // title
	URL         string    `json:"url"`
	Status      string    `json:"status"`         // the source's own status word
	StatusOrder int       `json:"status_order"`   // workflow position of Status
	ListID      string    `json:"list_id"`        // where the allowed statuses come from
	Project     string    `json:"project"`        // client (ClickUp list) or repo
	Type        string    `json:"type,omitempty"` // task type, e.g. "Idea"
	Priority    string    `json:"priority,omitempty"`
	Labels      []string  `json:"labels,omitempty"` // labels other than priority
	Mine        bool      `json:"mine,omitempty"`   // assigned to me
	Lead        bool      `json:"lead,omitempty"`   // I'm the lead
	Others      []string  `json:"others,omitempty"` // other assignees' names
	Links       []string  `json:"links,omitempty"`  // lowercase words in a branch or PR title that link a PR here
	Due         time.Time `json:"due,omitzero"`
	Updated     time.Time `json:"updated,omitzero"`
	Done        time.Time `json:"done,omitzero"`
}

// Ref returns an ID unique across sources.
func Ref(source, id string) string { return source + ":" + id }

// Ref is the task's ID unique across sources.
func (t Task) Ref() string { return Ref(t.Source, t.ID) }

// KeyLower is Key in lowercase, for branch and folder names.
func (t Task) KeyLower() string { return strings.ToLower(t.Key) }

// FolderKey is KeyLower made safe for folder names: characters other than
// letters, digits, '.', '_' and '-' become '-' (shop#42 → shop-42).
func (t Task) FolderKey() string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, t.KeyLower())
}

// IsDone reports whether the task is closed or in a done status.
func (t Task) IsDone() bool { return !t.Done.IsZero() }

// With names who has the task now: "you", the assignees' names, or "nobody".
func (t Task) With() string {
	switch {
	case t.Mine:
		return "you"
	case len(t.Others) == 0:
		return "nobody"
	}
	return strings.Join(t.Others, ", ")
}

// Status is one status a task can move to.
type Status struct {
	Name  string
	Order int
	Done  bool // moving here closes the task
}

// Rules are a source's status words and view settings (SPEC §4.2–§4.4).
// Zero values give ClickUp's behavior.
type Rules struct {
	Active  []string // assigned to me: In progress
	Next    []string // assigned to me: Up next
	Waiting []string // I'm lead, someone else has it: Waiting on others
	Alert   []string // shown in red in Today's agent column
	// Startable statuses offer to move the task to MoveTo when an agent
	// starts on it.
	Startable []string
	MoveTo    string
	// Groups are the Today groups this source uses. Nil: DefaultGroups.
	Groups []string
	// Classify puts an open task into one of Groups, or "" to leave it out
	// of Today. Nil: by status (Active, Next, Waiting).
	Classify func(t Task, s Signals) string
	// FilterNames label the Tasks view's filters (all, assigned, lead).
	// Zero: "Mine + Lead", "Assigned", "Lead only".
	FilterNames [3]string
	// ListName names this source's list tab. Zero: "Tasks".
	ListName string
	// RepoInList says the task's ListID is its GitHub repo (owner/repo), so
	// the Start panel needs no repo picker.
	RepoInList bool
	// BranchTemplate and PromptTemplate replace the config's templates for
	// this source when set.
	BranchTemplate string
	PromptTemplate string
}

// Signals are what huddle knows about tasks besides their source data,
// keyed by Task.Ref.
type Signals struct {
	Live     map[string]bool // has a live agent tab
	InReview map[string]bool // has a linked open PR
}

// Source is a task system huddle reads and writes. me is the source's own
// ID for the current user, from Me.
type Source interface {
	Name() string
	Rules() Rules
	Me(ctx context.Context) (int64, error)
	// Fetch returns tasks assigned to me or where I'm lead, with Mine, Lead
	// and Others filled in. A zero since means a full fetch: open tasks plus
	// tasks done in the last DoneWindow. Otherwise only tasks updated after
	// since.
	Fetch(ctx context.Context, me int64, since, now time.Time) ([]Task, error)
	Statuses(ctx context.Context, t Task) ([]Status, error)
	SetStatus(ctx context.Context, t Task, status string) error
	AddComment(ctx context.Context, t Task, text string) error
	SetAssignee(ctx context.Context, t Task, me int64, add bool) error
}
