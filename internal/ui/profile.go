package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/slavafyi/huddle/internal/config"
)

// profilePicker is the P popup: profiles, most recently used first.
type profilePicker struct {
	profiles []config.Profile // in picker order
	filter   string
	cursor   int
}

// shown returns the profiles that match the filter.
func (p *profilePicker) shown() []config.Profile {
	var out []config.Profile
	for _, pr := range p.profiles {
		if strings.Contains(strings.ToLower(pr.Name), strings.ToLower(p.filter)) {
			out = append(out, pr)
		}
	}
	return out
}

// recentFirst orders profiles by the recent list, then the rest in file
// order.
func recentFirst(profiles []config.Profile, recent []string) []config.Profile {
	var out []config.Profile
	for _, name := range recent {
		if i := slices.IndexFunc(profiles, func(p config.Profile) bool { return p.Name == name }); i >= 0 {
			out = append(out, profiles[i])
		}
	}
	for _, p := range profiles {
		if !slices.Contains(recent, p.Name) {
			out = append(out, p)
		}
	}
	return out
}

func (a *App) openProfilePicker() {
	a.profilePicker = profilePicker{profiles: recentFirst(a.deps.Profiles, a.deps.Recent)}
	a.mode = modeProfile
}

func (a *App) profileKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.profilePicker
	shown := p.shown()
	k := msg.String()
	switch k {
	case "esc":
		a.mode = modeNormal
	case "down", "ctrl+n":
		if len(shown) > 0 {
			p.cursor = (p.cursor + 1) % len(shown)
		}
	case "up", "ctrl+p":
		if len(shown) > 0 {
			p.cursor = (p.cursor - 1 + len(shown)) % len(shown)
		}
	case "enter":
		if p.cursor < len(shown) {
			return a.switchProfile(shown[p.cursor].Name)
		}
	case "backspace":
		if r := []rune(p.filter); len(r) > 0 {
			p.filter, p.cursor = string(r[:len(r)-1]), 0
		}
	default:
		if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
			if i := int(k[0] - '1'); i < len(shown) {
				return a.switchProfile(shown[i].Name)
			}
			return nil
		}
		if msg.Text != "" {
			p.filter, p.cursor = p.filter+msg.Text, 0
		}
	}
	return nil
}

// switchProfile ends the program so main can start it again in another
// profile. Picking the current profile only closes the picker.
func (a *App) switchProfile(name string) tea.Cmd {
	a.mode = modeNormal
	if name == a.deps.Profile {
		return nil
	}
	a.switchTo = name
	return tea.Quit
}

// SwitchTo is the profile chosen in the picker when the program ended, or
// "" when the user quit.
func (a *App) SwitchTo() string { return a.switchTo }

func (a *App) renderProfilePicker() []string {
	p := &a.profilePicker
	lines := []string{sBold.Render("Profile"), sFaint.Render("› ") + p.filter + "▏", ""}
	shown := p.shown()
	if len(shown) == 0 {
		lines = append(lines, sFaint.Render("no match"))
	}
	for i, pr := range shown {
		mark := "  "
		if i == p.cursor {
			mark = "› "
		}
		cur := "  "
		if pr.Name == a.deps.Profile {
			cur = sGreen.Render("●") + " "
		}
		line := mark + cur + fmt.Sprintf("%d %-12s ", i+1, pr.Name) + sFaint.Render(pr.Summary())
		if i == p.cursor {
			line = sBold.Render(line)
		}
		lines = append(lines, line)
	}
	return box(lines, 52)
}
