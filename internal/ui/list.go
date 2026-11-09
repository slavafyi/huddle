package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// column is a list column. Width 0 means "take the space left".
type column struct {
	title string
	width int
}

// row is one selectable line. Cells may contain styles.
type row struct {
	key   string
	cells []string
}

// section is a titled group of rows.
type section struct {
	title string
	style func(...string) string
	note  string
	rows  []row
}

// list is a grouped, scrollable list with one selected row.
type list struct {
	cols     []column
	sections []section
	sel      int
	empty    string
	touched  bool // the user moved the cursor; keep it on the same row
}

func (l *list) rows() []row {
	var out []row
	for _, s := range l.sections {
		out = append(out, s.rows...)
	}
	return out
}

// setSections replaces the content. Once the user has moved the cursor, it
// stays on the same row if that row is still there; before that it stays on
// the first row.
func (l *list) setSections(secs []section) {
	var key string
	if r, ok := l.selected(); ok && l.touched {
		key = r.key
	}
	l.sections = secs
	if key == "" {
		l.sel = 0
		return
	}
	for i, r := range l.rows() {
		if r.key == key {
			l.sel = i
			return
		}
	}
	l.clamp()
}

func (l *list) clamp() {
	n := len(l.rows())
	if l.sel >= n {
		l.sel = n - 1
	}
	if l.sel < 0 {
		l.sel = 0
	}
}

// move changes the selection by d rows, wrapping at the ends.
func (l *list) move(d int) {
	n := len(l.rows())
	if n == 0 {
		return
	}
	l.sel = ((l.sel+d)%n + n) % n
	l.touched = true
}

func (l *list) selected() (row, bool) {
	rs := l.rows()
	if l.sel < 0 || l.sel >= len(rs) {
		return row{}, false
	}
	return rs[l.sel], true
}

// render draws the list in width × height cells.
func (l *list) render(width, height int) string {
	if len(l.rows()) == 0 {
		msg := l.empty
		if msg == "" {
			msg = "nothing here"
		}
		return sFaint.Render(msg)
	}
	widths := l.widths(width)
	var lines []string
	selLine := 0
	hasHeader := false
	for _, c := range l.cols {
		if c.title != "" {
			hasHeader = true
		}
	}
	if hasHeader {
		var cells []string
		for _, c := range l.cols {
			cells = append(cells, c.title)
		}
		lines = append(lines, sFaint.Render(l.line(cells, widths, " ")))
	}
	i := 0
	for si, s := range l.sections {
		if s.title != "" {
			if si > 0 || hasHeader {
				lines = append(lines, "")
			}
			st := s.style
			if st == nil {
				st = sBold.Render
			}
			title := "  " + st(s.title) + sFaint.Render(fmt.Sprintf(" %d", len(s.rows)))
			if s.note != "" {
				title += sFaint.Render(" · " + s.note)
			}
			lines = append(lines, ansi.Truncate(title, width, "…"))
		}
		for _, r := range s.rows {
			if i == l.sel {
				selLine = len(lines)
				lines = append(lines, selected(l.line(r.cells, widths, "›"), width))
			} else {
				lines = append(lines, l.line(r.cells, widths, " "))
			}
			i++
		}
	}
	// Scroll so the selected line is visible.
	start := 0
	if len(lines) > height && height > 0 {
		start = selLine - height/2
		start = max(0, min(start, len(lines)-height))
		lines = lines[start : start+height]
	}
	return strings.Join(lines, "\n")
}

func (l *list) widths(total int) []int {
	ws := make([]int, len(l.cols))
	used := 4 + 2*(len(l.cols)-1) // left margin (› column + indent) + gaps
	flex := -1
	for i, c := range l.cols {
		if c.width == 0 && flex < 0 {
			flex = i
			continue
		}
		ws[i] = c.width
		used += c.width
	}
	// When space is short, shrink the widest fixed columns (not below 4)
	// so the flexible column keeps at least minFlex cells.
	const minFlex = 16
	for flex >= 0 && used+minFlex > total {
		widest := -1
		for i, w := range ws {
			if i != flex && w > 4 && (widest < 0 || w > ws[widest]) {
				widest = i
			}
		}
		if widest < 0 {
			break
		}
		ws[widest]--
		used--
	}
	if flex >= 0 {
		ws[flex] = max(4, total-used)
	}
	return ws
}

// line is one row: mark (› or space) in column 0, cells from column 4,
// indented under the section titles at column 2.
func (l *list) line(cells []string, widths []int, mark string) string {
	var b strings.Builder
	b.WriteString(mark + "   ")
	for i, w := range widths {
		if w <= 0 {
			continue
		}
		c := ""
		if i < len(cells) {
			c = cells[i]
		}
		c = ansi.Truncate(c, w, "…")
		b.WriteString(c)
		if i < len(widths)-1 {
			b.WriteString(strings.Repeat(" ", w-ansi.StringWidth(c)+2))
		}
	}
	return b.String()
}
