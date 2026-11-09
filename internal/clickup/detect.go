package clickup

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// IDs are the workspace and lead field huddle reads tasks with. An empty
// Lead turns lead features off.
type IDs struct {
	Workspace string `json:"workspace"`
	Lead      string `json:"lead,omitempty"`
}

// Detector finds the IDs the config leaves out (SPEC §5). Found IDs are
// passed to Save and come back as Saved on the next start, so a normal
// start makes no extra calls.
type Detector struct {
	HTTP  *HTTP
	Want  IDs // from the config; set values win and are never detected
	Saved IDs // found on an earlier start; zero if none
	Save  func(IDs) error
}

// Auto reports whether any ID is detected rather than configured.
func (d *Detector) Auto() bool { return d.Want.Workspace == "" || d.Want.Lead == "" }

// Setup sets the client's IDs: configured ones, else the remembered ones,
// else detected (and remembered). It returns a note for the user, or "".
func (d *Detector) Setup(ctx context.Context) (string, error) {
	if !d.Auto() {
		d.use(d.Want)
		return "", nil
	}
	if s := d.Saved; s.Workspace != "" && (d.Want.Workspace == "" || d.Want.Workspace == s.Workspace) {
		d.use(d.merge(s))
		return "", nil
	}
	ids, err := d.detect(ctx)
	if err != nil {
		return "", err
	}
	d.use(ids)
	if err := d.save(ids); err != nil {
		return "", err
	}
	if ids.Lead == "" {
		return noLeadNote, nil
	}
	return "", nil
}

const noLeadNote = `ClickUp: no people field named "lead" found; lead features are off (set clickup_lead to choose one)`

// Redetect finds the IDs again after the current ones stopped working. It
// reports whether they changed, with a warning that says how.
func (d *Detector) Redetect(ctx context.Context) (warning string, changed bool, err error) {
	ids, err := d.detect(ctx)
	if err != nil {
		return "", false, err
	}
	old := IDs{Workspace: d.HTTP.TeamID, Lead: d.HTTP.LeadFieldID}
	if ids == old {
		return "", false, nil
	}
	d.use(ids)
	if err := d.save(ids); err != nil {
		return "", false, err
	}
	var parts []string
	if ids.Workspace != old.Workspace {
		parts = append(parts, fmt.Sprintf("workspace %s → %s", old.Workspace, ids.Workspace))
	}
	if ids.Lead != old.Lead {
		lead := ids.Lead
		if lead == "" {
			lead = "none (lead features off)"
		}
		parts = append(parts, fmt.Sprintf("lead field %s → %s", old.Lead, lead))
	}
	return "ClickUp settings no longer matched: " + strings.Join(parts, ", "), true, nil
}

func (d *Detector) save(ids IDs) error {
	d.Saved = ids
	if d.Save == nil {
		return nil
	}
	return d.Save(ids)
}

func (d *Detector) merge(ids IDs) IDs {
	if d.Want.Workspace != "" {
		ids.Workspace = d.Want.Workspace
	}
	if d.Want.Lead != "" {
		ids.Lead = d.Want.Lead
	}
	return ids
}

func (d *Detector) use(ids IDs) {
	c := d.HTTP
	c.typesMu.Lock()
	if c.TeamID != ids.Workspace {
		c.types = nil // task type names are per workspace
	}
	c.typesMu.Unlock()
	c.TeamID, c.LeadFieldID = ids.Workspace, ids.Lead
}

func (d *Detector) detect(ctx context.Context) (IDs, error) {
	ids := d.Want
	if ids.Workspace == "" {
		var r struct {
			Teams []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"teams"`
		}
		if err := d.HTTP.do(ctx, "GET", "/team", nil, nil, &r); err != nil {
			return IDs{}, err
		}
		switch len(r.Teams) {
		case 0:
			return IDs{}, errors.New("no ClickUp workspace found for this token")
		case 1:
			ids.Workspace = r.Teams[0].ID
		default:
			var names []string
			for _, t := range r.Teams {
				names = append(names, fmt.Sprintf("%s (%s)", t.ID, t.Name))
			}
			return IDs{}, fmt.Errorf("the ClickUp token sees several workspaces: %s; set clickup_workspace in the profile",
				strings.Join(names, ", "))
		}
	}
	if ids.Lead == "" {
		var r struct {
			Fields []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"fields"`
		}
		if err := d.HTTP.do(ctx, "GET", "/team/"+ids.Workspace+"/field", nil, nil, &r); err != nil {
			return IDs{}, err
		}
		var found []string
		for _, f := range r.Fields {
			if f.Type == "users" && strings.Contains(strings.ToLower(f.Name), "lead") {
				ids.Lead = f.ID
				found = append(found, fmt.Sprintf("%s (%s)", f.ID, f.Name))
			}
		}
		if len(found) > 1 {
			return IDs{}, fmt.Errorf("several ClickUp lead fields: %s; set clickup_lead in the profile",
				strings.Join(found, ", "))
		}
	}
	return ids, nil
}

// isClientError reports whether err is a ClickUp 4xx answer (other than
// rate limiting), as when a workspace or field ID stopped working.
func isClientError(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Code >= 400 && e.Code < 500 && e.Code != 429
}

// APIError is a ClickUp answer with an error status.
type APIError struct {
	Method, Path string
	Code         int
	Body         string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("clickup %s %s: %d %s", e.Method, e.Path, e.Code, e.Body)
}
