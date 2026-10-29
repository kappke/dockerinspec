package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

// logPos addresses a character of the raw (unwrapped) log buffer.
type logPos struct{ line, off int }

func (a logPos) before(b logPos) bool {
	return a.line < b.line || (a.line == b.line && a.off < b.off)
}

var stSelected = sty("255", "24", false)

func (m *model) clearSel() { m.hasSel, m.selecting = false, false }

func (m *model) selRange() (lo, hi logPos) {
	lo, hi = m.selA, m.selB
	if hi.before(lo) {
		lo, hi = hi, lo
	}
	return
}

// colToOff converts a screen column inside a wrapped row to a rune offset.
func colToOff(s string, col int) int {
	w, n := 0, 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > col {
			break
		}
		w += rw
		n++
	}
	return n
}

// posAt maps a mouse position inside the log pane to a buffer position.
func (m *model) posAt(x, y int) (logPos, bool) {
	g := m.geom()
	if g.lh < 2 {
		return logPos{}, false
	}
	rows, _, _ := m.visibleLogRows(g.lw, g.lh-1)
	r := clamp(y-(g.ly+1), 0, len(rows)-1)
	// blank filler rows: fall back to the nearest real row above
	for r > 0 && rows[r].line < 0 {
		r--
	}
	row := rows[r]
	if row.line < 0 {
		return logPos{}, false
	}
	col := max(x-g.lx, 0)
	return logPos{row.line, row.off + colToOff(row.s, col)}, true
}

func (m *model) selectedText() string {
	if !m.hasSel {
		return ""
	}
	lo, hi := m.selRange()
	var out []string
	for l := lo.line; l <= hi.line && l < len(m.logs); l++ {
		if l < 0 {
			continue
		}
		r := []rune(m.logs[l])
		a, b := 0, len(r)
		if l == lo.line {
			a = min(lo.off, len(r))
		}
		if l == hi.line {
			b = min(hi.off, len(r))
		}
		if a > b {
			a = b
		}
		out = append(out, string(r[a:b]))
	}
	return strings.Join(out, "\n")
}

// renderRow draws one wrapped log row, highlighting the selected span.
func (m *model) renderRow(r logRow, w int) string {
	if r.line < 0 || (r.s == "" && !m.hasSel) {
		return strings.Repeat(" ", w)
	}
	rs := []rune(r.s)
	a, b := 0, 0
	if m.hasSel {
		lo, hi := m.selRange()
		if r.line >= lo.line && r.line <= hi.line {
			start, end := r.off, r.off+len(rs)
			if r.line == lo.line {
				start = max(start, lo.off)
			}
			if r.line == hi.line {
				end = min(end, hi.off)
			}
			a, b = clamp(start-r.off, 0, len(rs)), clamp(end-r.off, 0, len(rs))
		}
	}
	if b <= a {
		return render(r.st, padTo(r.s, w))
	}
	out := render(r.st, string(rs[:a])) + render(stSelected, string(rs[a:b])) + render(r.st, string(rs[b:]))
	return padTo(out, w)
}

// ---- clipboard ----------------------------------------------------------

// copyText sends text to every clipboard path available and reports which.
func copyText(text string) string {
	var via []string
	pipe := func(name string, args ...string) bool {
		if _, err := exec.LookPath(name); err != nil {
			return false
		}
		cmd := exec.Command(name, args...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run() == nil
	}
	inTmux := os.Getenv("TMUX") != ""
	if inTmux && pipe("tmux", "load-buffer", "-w", "-") {
		via = append(via, "tmux")
	}
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "" && pipe("wl-copy"):
		via = append(via, "wl-copy")
	case os.Getenv("DISPLAY") != "" && (pipe("xclip", "-selection", "clipboard") || pipe("xsel", "-ib")):
		via = append(via, "x11")
	case pipe("pbcopy"):
		via = append(via, "pbcopy")
	}
	// OSC 52 reaches the local terminal even over SSH.
	if len(text) < 100000 {
		seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
		if inTmux {
			seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
		}
		os.Stdout.WriteString(seq)
		via = append(via, "osc52")
	}
	return strings.Join(via, "+")
}

func (m *model) copySelection() {
	t := m.selectedText()
	if t == "" {
		m.flash("nothing selected — drag over the logs (double-click selects a line)")
		return
	}
	n := strings.Count(t, "\n") + 1
	m.flash(fmt.Sprintf("copied %d line(s), %d chars → %s", n, len([]rune(t)), copyText(t)))
}

func (m *model) copyAll() {
	if len(m.logs) == 0 {
		return
	}
	m.flash(fmt.Sprintf("copied all %d lines → %s", len(m.logs), copyText(strings.Join(m.logs, "\n"))))
}

// finishSelect runs when the mouse button is released.
func (m *model) finishSelect() {
	if !m.selecting {
		return
	}
	m.selecting = false
	if m.hasSel && m.selA != m.selB {
		m.copySelection()
	} else {
		m.hasSel = false
	}
}

func (m *model) selectLine(x, y int) {
	p, ok := m.posAt(x, y)
	if !ok {
		return
	}
	m.selA = logPos{p.line, 0}
	m.selB = logPos{p.line, len([]rune(m.logs[p.line]))}
	m.hasSel, m.selecting = true, false
	m.copySelection()
}

var lastClick struct {
	t    time.Time
	x, y int
}
