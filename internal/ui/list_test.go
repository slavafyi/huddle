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
	out := l.render(30, 20)
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > 30 {
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
