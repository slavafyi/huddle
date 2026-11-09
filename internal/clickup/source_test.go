package clickup

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/slavafyi/huddle/internal/task"
)

const me int64 = 1

var (
	malcolm = User{ID: 2, Username: "Malcolm"}
	owner   = User{ID: me, Username: "Alexa"}
)

func TestToTask(t *testing.T) {
	due := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	got := ToTask(Task{
		ID: "869f", CustomID: "DEV-1", Name: "Fix it", URL: "u", Status: "to review", StatusType: "custom",
		StatusOrder: 3, ListID: "L1", ListName: "Snowcap", TypeName: "Coding", Priority: "high",
		Assignees: []User{malcolm, owner}, Leads: []User{owner}, Due: due,
	}, me)
	want := task.Task{
		Source: "clickup", ID: "869f", Key: "DEV-1", Name: "Fix it", URL: "u", Status: "to review",
		StatusOrder: 3, ListID: "L1", Project: "Snowcap", Type: "Coding", Priority: "high",
		Mine: true, Lead: true, Others: []string{"Malcolm"}, Links: []string{"869f", "cu-869f", "dev-1"}, Due: due,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if k := ToTask(Task{ID: "869f"}, me).Key; k != "869f" {
		t.Errorf("key without custom ID = %q", k)
	}
}

func TestSourceRulesAreTheClickUpDefaults(t *testing.T) {
	r := (&Source{InProgress: "doing"}).Rules()
	want := task.Rules{
		Active:    []string{"in progress", "feedbacks"},
		Next:      []string{"to start"},
		Waiting:   []string{"to review", "pending", "to publish"},
		Alert:     []string{"feedbacks"},
		Startable: []string{"to plan", "to start"},
		MoveTo:    "doing",
	}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("got %+v", r)
	}
}

func TestSourceStatusesMarkDone(t *testing.T) {
	s := &Source{Client: statusClient{}}
	got, err := s.Statuses(context.Background(), task.Task{ListID: "L1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []task.Status{{Name: "to do", Order: 0}, {Name: "complete", Order: 1, Done: true}, {Name: "Closed", Order: 2, Done: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}

type statusClient struct{ Client }

func (statusClient) Statuses(context.Context, string) ([]Status, error) {
	return []Status{{"to do", "open", 0}, {"complete", "done", 1}, {"Closed", "closed", 2}}, nil
}

var now = time.Date(2026, 11, 9, 13, 0, 0, 0, time.UTC)

func keys(ts []Task) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Key())
	}
	return out
}
