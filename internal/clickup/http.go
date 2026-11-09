package clickup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// DefaultBaseURL is the ClickUp REST API v2.
const DefaultBaseURL = "https://api.clickup.com/api/v2"

// HTTP is the ClickUp client over net/http.
type HTTP struct {
	BaseURL     string
	Token       string
	TeamID      string
	LeadFieldID string
	Client      *http.Client

	typesMu sync.Mutex
	types   map[int]string // nil until loaded
}

func (c *HTTP) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.Client
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("clickup %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("clickup %s %s: %d %s", method, path, resp.StatusCode, bytes.TrimSpace(data))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("clickup %s %s: decode: %w", method, path, err)
	}
	return nil
}

// Me returns the token's user.
func (c *HTTP) Me(ctx context.Context) (User, error) {
	var r struct {
		User User `json:"user"`
	}
	err := c.do(ctx, "GET", "/user", nil, nil, &r)
	return r.User, err
}

// MyTasks implements Client.
func (c *HTTP) MyTasks(ctx context.Context, me int64, since, now time.Time) ([]Task, error) {
	c.loadTypes(ctx)
	lead, err := json.Marshal([]map[string]any{{
		"field_id": c.LeadFieldID, "operator": "ANY", "value": []string{strconv.FormatInt(me, 10)},
	}})
	if err != nil {
		return nil, err
	}
	who := []url.Values{
		{"assignees[]": {strconv.FormatInt(me, 10)}},
		{"custom_fields": {string(lead)}},
	}
	var queries []url.Values
	for _, w := range who {
		if since.IsZero() {
			open := clone(w)
			open.Set("include_closed", "false")
			done := clone(w)
			done.Set("include_closed", "true")
			done.Set("date_done_gt", ms(now.Add(-DoneWindow)))
			queries = append(queries, open, done)
		} else {
			q := clone(w)
			q.Set("include_closed", "true")
			q.Set("date_updated_gt", ms(since))
			queries = append(queries, q)
		}
	}
	seen := map[string]bool{}
	var out []Task
	for _, q := range queries {
		q.Set("subtasks", "true")
		for page := 0; ; page++ {
			q.Set("page", strconv.Itoa(page))
			var r struct {
				Tasks    []apiTask `json:"tasks"`
				LastPage bool      `json:"last_page"`
			}
			if err := c.do(ctx, "GET", "/team/"+c.TeamID+"/task", q, nil, &r); err != nil {
				return nil, err
			}
			for _, at := range r.Tasks {
				if !seen[at.ID] {
					seen[at.ID] = true
					out = append(out, c.convert(at))
				}
			}
			if r.LastPage || len(r.Tasks) == 0 {
				break
			}
		}
	}
	return out, nil
}

// Statuses returns the statuses a list allows, in workflow order.
func (c *HTTP) Statuses(ctx context.Context, listID string) ([]Status, error) {
	var r struct {
		Statuses []Status `json:"statuses"`
	}
	err := c.do(ctx, "GET", "/list/"+listID, nil, nil, &r)
	return r.Statuses, err
}

// SetStatus changes a task's status. taskID is ClickUp's own ID.
func (c *HTTP) SetStatus(ctx context.Context, taskID, status string) error {
	return c.do(ctx, "PUT", "/task/"+taskID, nil, map[string]string{"status": status}, nil)
}

// AddComment adds a comment to a task without notifying everyone.
func (c *HTTP) AddComment(ctx context.Context, taskID, text string) error {
	return c.do(ctx, "POST", "/task/"+taskID+"/comment", nil,
		map[string]any{"comment_text": text, "notify_all": false}, nil)
}

// SetAssignee adds or removes one assignee.
func (c *HTTP) SetAssignee(ctx context.Context, taskID string, user int64, add bool) error {
	change := map[string][]int64{"add": {}, "rem": {}}
	if add {
		change["add"] = []int64{user}
	} else {
		change["rem"] = []int64{user}
	}
	return c.do(ctx, "PUT", "/task/"+taskID, nil, map[string]any{"assignees": change}, nil)
}

// loadTypes fetches task type names until it succeeds once. A failure only
// leaves names empty for this fetch.
func (c *HTTP) loadTypes(ctx context.Context) {
	c.typesMu.Lock()
	defer c.typesMu.Unlock()
	if c.types != nil {
		return
	}
	var r struct {
		Items []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"custom_items"`
	}
	if err := c.do(ctx, "GET", "/team/"+c.TeamID+"/custom_item", nil, nil, &r); err != nil {
		return
	}
	c.types = map[int]string{}
	for _, it := range r.Items {
		c.types[it.ID] = it.Name
	}
}

func (c *HTTP) typeName(id int) string {
	c.typesMu.Lock()
	defer c.typesMu.Unlock()
	return c.types[id]
}

type apiTask struct {
	ID       string `json:"id"`
	CustomID string `json:"custom_id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Status   struct {
		Status     string `json:"status"`
		Type       string `json:"type"`
		OrderIndex int    `json:"orderindex"`
	} `json:"status"`
	CustomItemID int `json:"custom_item_id"`
	List         struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"list"`
	Assignees []User          `json:"assignees"`
	Priority  json.RawMessage `json:"priority"`
	Due       *string         `json:"due_date"`
	Updated   *string         `json:"date_updated"`
	DateDone  *string         `json:"date_done"`
	Closed    *string         `json:"date_closed"`
	Fields    []struct {
		ID    string          `json:"id"`
		Value json.RawMessage `json:"value"`
	} `json:"custom_fields"`
}

func (c *HTTP) convert(a apiTask) Task {
	t := Task{
		ID: a.ID, CustomID: a.CustomID, Name: a.Name, URL: a.URL,
		Status: a.Status.Status, StatusType: a.Status.Type, StatusOrder: a.Status.OrderIndex,
		ListID: a.List.ID, ListName: a.List.Name, TypeName: c.typeName(a.CustomItemID),
		Assignees: a.Assignees,
		Due:       parseMS(a.Due), Updated: parseMS(a.Updated),
		Done: parseMS(a.Closed),
	}
	if d := parseMS(a.DateDone); !d.IsZero() {
		t.Done = d
	}
	var pr struct {
		Priority string `json:"priority"`
	}
	if json.Unmarshal(a.Priority, &pr) == nil {
		t.Priority = pr.Priority
	} else {
		var s string
		if json.Unmarshal(a.Priority, &s) == nil {
			t.Priority = s
		}
	}
	for _, f := range a.Fields {
		if f.ID == c.LeadFieldID {
			json.Unmarshal(f.Value, &t.Leads)
		}
	}
	return t
}

func parseMS(s *string) time.Time {
	if s == nil || *s == "" {
		return time.Time{}
	}
	n, err := strconv.ParseInt(*s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(n)
}

func ms(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }

func clone(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}
