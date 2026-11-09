package ui

import (
	"strings"
	"testing"
)

// Blue is grey in some themes (koda-dark), so the active tab and filter
// must stand out by bold alone, never by color.
func TestActiveTabAndFilterAreBoldNotColored(t *testing.T) {
	e := newEnv(t)
	e.load(t)
	bar := e.app.tabBar()
	if !strings.Contains(bar, sBold.Render("1 Today")) {
		t.Errorf("active tab not plain bold: %q", bar)
	}
	e.press("2")
	sub := e.app.tasks.subHeader(e.app.cache.Tasks)
	if !strings.Contains(sub, "\x1b[1mMine + Lead") {
		t.Errorf("active filter not bold: %q", sub)
	}
	for _, s := range []string{bar, sub} {
		if strings.Contains(s, "34m") || strings.Contains(s, "94m") {
			t.Errorf("blue used: %q", s)
		}
	}
}
