package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---- styles -------------------------------------------------------------

type styleID uint16

var (
	styleTab []lipgloss.Style
	styleIdx = map[string]styleID{}
)

func init() { sty("", "", false) } // id 0 = plain

func sty(fg, bg string, bold bool) styleID {
	key := fg + "|" + bg
	if bold {
		key += "|b"
	}
	if id, ok := styleIdx[key]; ok {
		return id
	}
	s := lipgloss.NewStyle()
	if fg != "" {
		s = s.Foreground(lipgloss.Color(fg))
	}
	if bg != "" {
		s = s.Background(lipgloss.Color(bg))
	}
	if bold {
		s = s.Bold(true)
	}
	id := styleID(len(styleTab))
	styleTab = append(styleTab, s)
	styleIdx[key] = id
	return id
}

func render(id styleID, s string) string {
	if id == 0 || s == "" {
		return s
	}
	return styleTab[id].Render(s)
}

// ---- cell grid ----------------------------------------------------------

type cell struct {
	r rune
	s styleID
}

type grid struct {
	w, h int
	c    []cell
}

func newGrid(w, h int) *grid { return &grid{w: w, h: h, c: make([]cell, w*h)} }

func (g *grid) set(x, y int, r rune, s styleID) {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return
	}
	g.c[y*g.w+x] = cell{r, s}
}

func (g *grid) at(x, y int) cell {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return cell{}
	}
	return g.c[y*g.w+x]
}

func (g *grid) text(x, y int, s string, st styleID) int {
	n := 0
	for _, r := range s {
		g.set(x+n, y, r, st)
		n++
	}
	return n
}

func (g *grid) fill(x, y, w, h int, st styleID) {
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			g.set(x+i, y+j, ' ', st)
		}
	}
}

func (g *grid) box(x, y, w, h int, round bool, st styleID) {
	tl, tr, bl, br := '┌', '┐', '└', '┘'
	if round {
		tl, tr, bl, br = '╭', '╮', '╰', '╯'
	}
	for i := 1; i < w-1; i++ {
		g.set(x+i, y, '─', st)
		g.set(x+i, y+h-1, '─', st)
	}
	for j := 1; j < h-1; j++ {
		g.set(x, y+j, '│', st)
		g.set(x+w-1, y+j, '│', st)
	}
	g.set(x, y, tl, st)
	g.set(x+w-1, y, tr, st)
	g.set(x, y+h-1, bl, st)
	g.set(x+w-1, y+h-1, br, st)
}

// row renders width w starting at column x0 of row y (padded with blanks).
func (g *grid) row(y, x0, w int) string {
	var sb strings.Builder
	var run strings.Builder
	var cur styleID
	flush := func() {
		if run.Len() > 0 {
			sb.WriteString(render(cur, run.String()))
			run.Reset()
		}
	}
	for i := 0; i < w; i++ {
		c := g.at(x0+i, y)
		r := c.r
		if r == 0 {
			r = ' '
		}
		if c.s != cur {
			flush()
			cur = c.s
		}
		run.WriteRune(r)
	}
	flush()
	return sb.String()
}
