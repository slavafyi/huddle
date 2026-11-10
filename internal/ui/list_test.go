package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func sampleList() *list {
	l := &list{cols: []column{{title: "ID", width: 4}, {title: "TITLE"}, {title: "WHO", width: 5}}}
	l.setSections([]section{
		{title: "a", rows: []row{{key: "1", cells: []string{"1", "first", "you"}}, {key: "2", cells: []string{"2", "second", "you"}}}},
		{title: "b", rows: []row{{key: "3", cells: []string{"3", "third task with a long title", "Malcolm"}}}},
	})
	return l
}

func TestListMoveWraps(t *testing.T) {
	l := sampleList()
	l.move(-1)
	if r, _ := l.selected(); r.key != "3" {
		t.Errorf("wrap up: %s", r.key)
	}
	l.move(1)
	if r, _ := l.selected(); r.key != "1" {
		t.Errorf("wrap down: %s", r.key)
	}
}

func TestListKeepsSelectionByKey(t *testing.T) {
	l := sampleList()
	l.move(2) // "3"
	l.setSections([]section{{title: "x", rows: []row{{key: "3"}, {key: "9"}}}})
	if r, _ := l.selected(); r.key != "3" {
		t.Errorf("selection lost: %s", r.key)
	}
	l.setSections([]section{{title: "x", rows: []row{{key: "9"}}}})
	if r, _ := l.selected(); r.key != "9" {
		t.Errorf("fallback: %s", r.key)
	}
}

func TestListRenderFitsWidth(t *testing.T) {
	l := sampleList()
	out := l.render(40, 20)
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > 40 {
			t.Errorf("line too wide (%d): %q", w, ansi.Strip(line))
		}
	}
	plain := ansi.Strip(out)
	for _, want := range []string{"ID", "TITLE", "a 2", "b 1", "first", "Malc…"} { // WHO is 5 cells wide, "…" included
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in:\n%s", want, plain)
		}
	}
}

func TestListScrollsToSelection(t *testing.T) {
	l := &list{cols: []column{{title: "N"}}}
	var rows []row
	for i := 0; i < 50; i++ {
		rows = append(rows, row{key: string(rune('A'+i%26)) + string(rune('0'+i/26)), cells: []string{"row" + string(rune('A'+i%26)) + string(rune('0'+i/26))}})
	}
	l.setSections([]section{{title: "all", rows: rows}})
	l.move(45)
	out := ansi.Strip(l.render(40, 10))
	if !strings.Contains(out, "rowT1") { // row 45
		t.Errorf("selected row not visible:\n%s", out)
	}
	if n := len(strings.Split(out, "\n")); n > 10 {
		t.Errorf("too many lines: %d", n)
	}
}

func TestNarrowWidthShrinksFixedColumns(t *testing.T) {
	l := &list{cols: []column{{title: "#", width: 6}, {title: "TITLE"}, {title: "REPO", width: 22}, {title: "TASK", width: 10}, {title: "CI", width: 9}, {title: "AGE", width: 4}}}
	l.setSections([]section{{title: "x", rows: []row{{key: "1", cells: []string{"#46", "feat(subscriptions): storefront", "snowcap", "none", "—", "17d"}}}}})
	out := l.render(60, 10)
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > 60 {
			t.Errorf("line too wide (%d): %q", w, ansi.Strip(line))
		}
	}
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "17d") || !strings.Contains(plain, "feat(subscr") {
		t.Errorf("columns not shrunk sensibly:\n%s", plain)
	}
}

// Titles at column 2, rows from column 4, › on the selected row; the
// selection is a full-width grey (ANSI 8) background that keeps colors
// and turns faint text normal (SPEC §4.8).
func TestListLayoutAndSelection(t *testing.T) {
	l := &list{cols: []column{{width: 4}, {}, {width: 6}}}
	l.setSections([]section{{title: "a", rows: []row{
		{key: "1", cells: []string{sFaint.Render("1"), "first", sRed.Render("Nov 2")}},
		{key: "2", cells: []string{sFaint.Render("2"), "second", "x"}},
	}}})
	lines := strings.Split(l.render(40, 10), "\n")
	plain := make([]string, len(lines))
	for i, s := range lines {
		plain[i] = ansi.Strip(s)
	}
	if !strings.HasPrefix(plain[0], "  a 1") && !strings.HasPrefix(plain[0], "  a 2") {
		t.Errorf("title not at column 2: %q", plain[0])
	}
	if !strings.HasPrefix(plain[1], "›   1") || !strings.HasPrefix(plain[2], "    2") {
		t.Errorf("rows not at column 4 / no ›: %q %q", plain[1], plain[2])
	}
	sel := lines[1]
	if !strings.Contains(sel, "100m") || strings.Contains(sel, "7m") {
		t.Errorf("selection not grey: %q", sel)
	}
	if !strings.Contains(sel, "31m") || strings.Contains(sel, "\x1b[2m") {
		t.Errorf("selection must keep red and drop faint: %q", sel)
	}
	if w := ansi.StringWidth(sel); w != 40 {
		t.Errorf("selection width %d, want full 40", w)
	}
}

func TestListTopBottom(t *testing.T) {
	l := sampleList()
	l.bottom()
	if r, _ := l.selected(); r.key != "3" || !l.touched {
		t.Errorf("bottom: %s touched=%v", r.key, l.touched)
	}
	l.top()
	if r, _ := l.selected(); r.key != "1" {
		t.Errorf("top: %s", r.key)
	}
}

// page moves by the height last drawn, and stops at the ends (no wrap).
func TestListPage(t *testing.T) {
	l := &list{cols: []column{{title: "N"}}}
	var rows []row
	for i := 0; i < 30; i++ {
		rows = append(rows, row{key: string(rune('a' + i)), cells: []string{"x"}})
	}
	l.setSections([]section{{title: "all", rows: rows}})
	l.render(40, 10)
	l.page(1)
	if l.sel != 10 {
		t.Errorf("page down: %d", l.sel)
	}
	l.page(1)
	l.page(1)
	if l.sel != 29 {
		t.Errorf("page down at end: %d", l.sel)
	}
	l.page(-1)
	if l.sel != 19 {
		t.Errorf("page up: %d", l.sel)
	}
	l.page(-1)
	l.page(-1)
	if l.sel != 0 {
		t.Errorf("page up at top: %d", l.sel)
	}
}
