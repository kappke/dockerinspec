package main

import (
	"container/heap"
	"math"
	"sort"
	"strconv"
	"strings"
)

// dims holds the geometry of one zoom level.
type dims struct {
	cardW, cardH       int
	cardGapX, cardGapY int
	groupPadX          int
	groupPadT          int
	groupPadB          int
	groupGapX          int
	groupGapY          int
	margin             int
}

const defaultZoom = 2

var zoomNames = [4]string{"25%", "50%", "100%", "150%"}

var zoomDims = [4]dims{
	{18, 3, 5, 2, 2, 2, 1, 7, 3, 3},
	{26, 4, 8, 2, 3, 2, 1, 10, 3, 4},
	{32, 5, 10, 3, 3, 2, 1, 12, 4, 6},
	{44, 7, 14, 4, 4, 2, 1, 16, 5, 8},
}

type pt struct{ x, y int }

type Node struct {
	C          *Container
	X, Y, W, H int
}

func (n *Node) contains(x, y int) bool {
	return x >= n.X && x < n.X+n.W && y >= n.Y && y < n.Y+n.H
}

type Group struct {
	Name       string
	Nodes      []*Node
	X, Y, W, H int
}

type Edge struct {
	*Link
	Path  []pt
	Color styleID // base colour (index into palette)
	pal   int
}

type Layout struct {
	W, H   int
	Groups []*Group
	Nodes  map[string]*Node
	Order  []*Node // reading order, used for tab navigation
	Edges  []*Edge
}

func groupCols(n int) int {
	switch {
	case n <= 3:
		return 1
	case n <= 8:
		return 2
	}
	return 3
}

func computeLayout(cs []*Container, links []*Link, d dims) *Layout {
	L := &Layout{Nodes: map[string]*Node{}}
	byGroup := map[string][]*Container{}
	for _, c := range cs {
		byGroup[c.Group()] = append(byGroup[c.Group()], c)
	}
	names := make([]string, 0, len(byGroup))
	for n := range byGroup {
		names = append(names, n)
	}
	sort.Strings(names)

	// group-level connectivity, used to put related groups next to each other
	gOf := map[string]string{}
	for _, c := range cs {
		gOf[c.ID] = c.Group()
	}
	weight := map[[2]string]int{}
	total := map[string]int{}
	for _, l := range links {
		a, b := gOf[l.From], gOf[l.To]
		if a == b {
			continue
		}
		weight[[2]string{a, b}]++
		weight[[2]string{b, a}]++
		total[a]++
		total[b]++
	}
	var ordered []string
	left := map[string]bool{}
	for _, n := range names {
		left[n] = true
	}
	for len(left) > 0 {
		best, bs := "", -1
		for _, n := range names {
			if !left[n] {
				continue
			}
			s := 0
			for _, p := range ordered {
				s += weight[[2]string{n, p}] * 100
			}
			s += total[n]
			if n == "(standalone)" {
				s = -1
			}
			if s > bs {
				best, bs = n, s
			}
		}
		ordered = append(ordered, best)
		delete(left, best)
	}

	// build groups (relative coordinates)
	for _, name := range ordered {
		list := byGroup[name]
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		g := &Group{Name: name}
		cols := groupCols(len(list))
		rows := (len(list) + cols - 1) / cols
		for i, c := range list {
			col, row := i/rows, i%rows
			n := &Node{C: c, W: d.cardW, H: d.cardH,
				X: d.groupPadX + col*(d.cardW+d.cardGapX),
				Y: d.groupPadT + row*(d.cardH+d.cardGapY)}
			g.Nodes = append(g.Nodes, n)
		}
		g.W = 2*d.groupPadX + cols*d.cardW + (cols-1)*d.cardGapX
		g.H = d.groupPadT + rows*d.cardH + (rows-1)*d.cardGapY + d.groupPadB
		L.Groups = append(L.Groups, g)
	}

	// arrange groups in a grid (shelf packing)
	ng := len(L.Groups)
	gc := int(math.Round(math.Sqrt(float64(ng) * 1.6)))
	if gc < 1 {
		gc = 1
	}
	x, y, rowH, col := d.margin, d.margin, 0, 0
	maxW := 0
	for _, g := range L.Groups {
		if col == gc {
			x, y, rowH, col = d.margin, y+rowH+d.groupGapY, 0, 0
		}
		g.X, g.Y = x, y
		for _, n := range g.Nodes {
			n.X += g.X
			n.Y += g.Y
			L.Nodes[n.C.ID] = n
			L.Order = append(L.Order, n)
		}
		x += g.W + d.groupGapX
		if g.H > rowH {
			rowH = g.H
		}
		if x > maxW {
			maxW = x
		}
		col++
	}
	L.W = maxW - d.groupGapX + d.margin
	L.H = y + rowH + d.margin
	if L.W < 20 {
		L.W = 20
	}
	if L.H < 10 {
		L.H = 10
	}

	for i, l := range links {
		if L.Nodes[l.From] == nil || L.Nodes[l.To] == nil {
			continue
		}
		L.Edges = append(L.Edges, &Edge{Link: l, pal: i})
	}
	L.route()
	return L
}

// ---- edge routing (Dijkstra over the cell grid with turn penalties) -----

type rItem struct{ cost, state int32 }
type rHeap []rItem

func (h rHeap) Len() int            { return len(h) }
func (h rHeap) Less(i, j int) bool  { return h[i].cost < h[j].cost }
func (h rHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *rHeap) Push(x interface{}) { *h = append(*h, x.(rItem)) }
func (h *rHeap) Pop() interface{} {
	o := *h
	n := len(o)
	it := o[n-1]
	*h = o[:n-1]
	return it
}

var ddx = [4]int{0, 1, 0, -1}
var ddy = [4]int{-1, 0, 1, 0}

func (L *Layout) route() {
	w, h := L.W, L.H
	blocked := make([]bool, w*h)
	near := make([]int8, w*h)
	for _, n := range L.Nodes {
		for j := -1; j <= n.H; j++ {
			for i := -1; i <= n.W; i++ {
				x, y := n.X+i, n.Y+j
				if x < 0 || y < 0 || x >= w || y >= h {
					continue
				}
				if i >= 0 && i < n.W && j >= 0 && j < n.H {
					blocked[y*w+x] = true
				} else {
					near[y*w+x] = 1
				}
			}
		}
	}
	border := make([]int8, w*h)
	for _, g := range L.Groups {
		for i := 0; i < g.W; i++ {
			for _, y := range []int{g.Y, g.Y + g.H - 1} {
				if x := g.X + i; x < w && y < h {
					border[y*w+x] = 1
				}
			}
			// keep the title row readable
			if i < len(g.Name)+14 && g.Y < h && g.X+i < w {
				blocked[g.Y*w+g.X+i] = true
			}
		}
		for j := 0; j < g.H; j++ {
			for _, x := range []int{g.X, g.X + g.W - 1} {
				if y := g.Y + j; x < w && y < h {
					border[y*w+x] = 1
				}
			}
		}
	}
	occH := make([]bool, w*h)
	occV := make([]bool, w*h)

	sort.SliceStable(L.Edges, func(i, j int) bool {
		return L.dist(L.Edges[i]) < L.dist(L.Edges[j])
	})

	const inf = int32(math.MaxInt32)
	dist := make([]int32, w*h*4)
	prev := make([]int32, w*h*4)

	for _, e := range L.Edges {
		src, dst := L.Nodes[e.From], L.Nodes[e.To]
		for i := range dist {
			dist[i] = inf
			prev[i] = -1
		}
		hp := &rHeap{}
		st := func(x, y, d int) int32 { return int32((y*w+x)*4 + d) }
		push := func(x, y, d int) {
			if x < 0 || y < 0 || x >= w || y >= h || blocked[y*w+x] {
				return
			}
			s := st(x, y, d)
			dist[s] = 0
			heap.Push(hp, rItem{0, s})
		}
		for i := 1; i < src.W-1; i++ {
			push(src.X+i, src.Y-1, 0)
			push(src.X+i, src.Y+src.H, 2)
		}
		for j := 1; j < src.H-1; j++ {
			push(src.X-1, src.Y+j, 3)
			push(src.X+src.W, src.Y+j, 1)
		}
		targets := map[int32]bool{}
		addT := func(x, y, d int) {
			if x < 0 || y < 0 || x >= w || y >= h || blocked[y*w+x] {
				return
			}
			targets[st(x, y, d)] = true
		}
		for i := 1; i < dst.W-1; i++ {
			addT(dst.X+i, dst.Y-1, 2)
			addT(dst.X+i, dst.Y+dst.H, 0)
		}
		for j := 1; j < dst.H-1; j++ {
			addT(dst.X-1, dst.Y+j, 1)
			addT(dst.X+dst.W, dst.Y+j, 3)
		}
		found := int32(-1)
		for hp.Len() > 0 {
			it := heap.Pop(hp).(rItem)
			if it.cost > dist[it.state] {
				continue
			}
			if targets[it.state] {
				found = it.state
				break
			}
			d := int(it.state) % 4
			cellIdx := int(it.state) / 4
			cx, cy := cellIdx%w, cellIdx/w
			for nd := 0; nd < 4; nd++ {
				if nd == (d+2)%4 {
					continue
				}
				nx, ny := cx+ddx[nd], cy+ddy[nd]
				if nx < 0 || ny < 0 || nx >= w || ny >= h || blocked[ny*w+nx] {
					continue
				}
				ni := ny*w + nx
				c := int32(1)
				if nd != d {
					c += 6
				}
				c += int32(near[ni]) + int32(border[ni])*2
				if nd%2 == 1 { // horizontal move
					if occH[ni] {
						c += 12
					} else if occV[ni] {
						c += 4
					}
				} else {
					if occV[ni] {
						c += 12
					} else if occH[ni] {
						c += 4
					}
				}
				ns := st(nx, ny, nd)
				if it.cost+c < dist[ns] {
					dist[ns] = it.cost + c
					prev[ns] = it.state
					heap.Push(hp, rItem{it.cost + c, ns})
				}
			}
		}
		if found < 0 {
			continue
		}
		var path []pt
		for s := found; s >= 0; s = prev[s] {
			ci := int(s) / 4
			path = append(path, pt{ci % w, ci / w})
		}
		for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
			path[i], path[j] = path[j], path[i]
		}
		for i, p := range path {
			var dd int
			if i+1 < len(path) {
				dd = dirOf(p, path[i+1])
			} else {
				dd = dirOf(path[i-1], p)
			}
			if dd%2 == 1 {
				occH[p.y*w+p.x] = true
			} else {
				occV[p.y*w+p.x] = true
			}
		}
		e.Path = path
	}
}

func dirOf(a, b pt) int {
	switch {
	case b.y < a.y:
		return 0
	case b.x > a.x:
		return 1
	case b.y > a.y:
		return 2
	}
	return 3
}

func (L *Layout) dist(e *Edge) int {
	a, b := L.Nodes[e.From], L.Nodes[e.To]
	dx, dy := a.X-b.X, a.Y-b.Y
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	return dx + dy
}

// ---- painting -----------------------------------------------------------

var (
	stGroupBorder = sty("60", "", false)
	stGroupTitle  = sty("111", "", true)
	stGroupTitleD = sty("244", "", false)
	stCardBorder  = sty("245", "", false)
	stCardSel     = sty("51", "", true)
	stName        = sty("255", "", true)
	stText        = sty("250", "", false)
	stDim         = sty("243", "", false)
	stRun         = sty("42", "", true)
	stBad         = sty("203", "", true)
	stWarn        = sty("214", "", true)
	stLabel       = sty("229", "", false)
	stOut         = sty("51", "", true)
	stIn          = sty("213", "", true)
)

var edgePalette = []string{"67", "72", "137", "133", "104", "108", "173", "110"}

var glyphs = [16]rune{
	0: ' ', 1: '│', 2: '─', 3: '└', 4: '│', 5: '│', 6: '┌', 7: '├',
	8: '─', 9: '┘', 10: '─', 11: '┴', 12: '┐', 13: '┤', 14: '┬', 15: '┼',
}

// bit layout: N=1 E=2 S=4 W=8; 3 = N+E etc. (see glyph table)
var dirBit = [4]int{1, 2, 4, 8}

type lineCell struct {
	mask   int
	st     styleID
	dashed bool
	hl     bool
}

func stateStyle(c *Container) (rune, styleID) {
	switch {
	case c.State == "running" && (c.Health == "unhealthy"):
		return '●', stBad
	case c.State == "running" && c.Health == "starting":
		return '●', stWarn
	case c.State == "running":
		return '●', stRun
	case c.State == "paused" || c.State == "restarting":
		return '◐', stWarn
	case c.State == "created":
		return '○', stDim
	}
	return '○', stBad
}

func fit(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 1 {
		return string(r[:w])
	}
	return string(r[:w-1]) + "…"
}

// paint renders the whole canvas. sel highlights that container's links.
func (L *Layout) paint(sel string, labels bool) *grid {
	g := newGrid(L.W, L.H)

	for _, grp := range L.Groups {
		g.box(grp.X, grp.Y, grp.W, grp.H, false, stGroupBorder)
		g.text(grp.X+2, grp.Y, " ", 0)
		x := grp.X + 2
		x += g.text(x, grp.Y, "┤ ", stGroupBorder)
		x += g.text(x, grp.Y, grp.Name, stGroupTitle)
		cnt := " (" + itoa(len(grp.Nodes)) + ") "
		x += g.text(x, grp.Y, cnt, stGroupTitleD)
		g.text(x, grp.Y, "├", stGroupBorder)
	}

	lines := make([]lineCell, L.W*L.H)
	type arrow struct {
		p  pt
		r  rune
		st styleID
	}
	var arrows []arrow
	type lbl struct {
		x, y int
		s    string
	}
	var lbls []lbl

	draw := func(e *Edge, hl bool, out bool) {
		path := e.Path
		if len(path) < 2 {
			return
		}
		col := sty(edgePalette[e.pal%len(edgePalette)], "", false)
		if hl {
			if out {
				col = stOut
			} else {
				col = stIn
			}
		}
		src := L.Nodes[e.From]
		for i, p := range path {
			m := 0
			if i > 0 {
				m |= dirBit[dirOf(p, path[i-1])]
			} else {
				// connect back to the source card
				for d := 0; d < 4; d++ {
					if src.contains(p.x+ddx[d], p.y+ddy[d]) {
						m |= dirBit[d]
					}
				}
			}
			if i+1 < len(path) {
				m |= dirBit[dirOf(p, path[i+1])]
			}
			lc := &lines[p.y*L.W+p.x]
			if lc.hl && !hl {
				lc.mask |= m
				continue
			}
			lc.mask |= m
			lc.st = col
			lc.hl = lc.hl || hl
			lc.dashed = e.ViaHost && lc.mask == m && (m == 5 || m == 10)
			if lc.mask != m {
				lc.dashed = false
			}
		}
		last := path[len(path)-1]
		prev := path[len(path)-2]
		ar := map[int]rune{0: '▲', 1: '▶', 2: '▼', 3: '◀'}[dirOf(prev, last)]
		arrows = append(arrows, arrow{last, ar, col})
		if labels || hl {
			text := portLabel(e)
			if text != "" {
				// longest horizontal run
				bestLen, bx, by := 0, 0, 0
				run, sx := 0, 0
				for i := 0; i < len(path); i++ {
					if i > 0 && path[i].y == path[i-1].y {
						run++
					} else {
						run, sx = 1, path[i].x
					}
					if run > bestLen {
						bestLen, bx, by = run, sx, path[i].y
					}
					_ = sx
				}
				if bestLen >= len(text)+2 {
					lbls = append(lbls, lbl{bx + (bestLen-len(text))/2, by - 1, text})
				}
			}
		}
	}
	for _, e := range L.Edges {
		if e.Link.From == sel || e.Link.To == sel {
			continue
		}
		draw(e, false, false)
	}
	for _, e := range L.Edges {
		if e.Link.From == sel {
			draw(e, true, true)
		} else if e.Link.To == sel {
			draw(e, true, false)
		}
	}
	for i, lc := range lines {
		if lc.mask == 0 {
			continue
		}
		r := glyphs[lc.mask&15]
		if lc.dashed {
			if r == '─' {
				r = '┄'
			} else if r == '│' {
				r = '┆'
			}
		}
		g.c[i] = cell{r, lc.st}
	}
	for _, a := range arrows {
		g.set(a.p.x, a.p.y, a.r, a.st)
	}
	for _, l := range lbls {
		for i, r := range l.s {
			if g.at(l.x+i, l.y).r == 0 || g.at(l.x+i, l.y).s == stGroupBorder {
				g.set(l.x+i, l.y, r, stLabel)
			}
		}
	}

	for _, n := range L.Order {
		paintCard(g, n, n.C.ID == sel)
	}
	return g
}

func portLabel(e *Edge) string {
	var p []string
	for _, x := range e.Ports {
		p = append(p, ":"+itoa(x))
	}
	return strings.Join(p, ",")
}

func paintCard(g *grid, n *Node, selected bool) {
	c := n.C
	bg := ""
	bs := stCardBorder
	if selected {
		bs, bg = sty("51", "236", true), "236"
	} else if !c.Running() {
		bs = stDim
	}
	if selected {
		g.fill(n.X, n.Y, n.W, n.H, sty("", bg, false))
	} else {
		g.fill(n.X, n.Y, n.W, n.H, 0)
	}
	g.box(n.X, n.Y, n.W, n.H, true, bs)
	inner := n.W - 4
	icon, ist := stateStyle(c)
	if selected {
		ist = sty(map[styleID]string{stRun: "42", stBad: "203", stWarn: "214", stDim: "243"}[ist], bg, true)
	}
	nameSt, txt, dim := stName, stText, stDim
	if selected {
		nameSt, txt, dim = sty("255", bg, true), sty("250", bg, false), sty("245", bg, false)
	} else if !c.Running() {
		nameSt = sty("245", "", true)
	}
	g.set(n.X+2, n.Y+1, icon, ist)
	g.text(n.X+4, n.Y+1, fit(c.Name, inner-2), nameSt)

	status := c.StatusText()
	ports := c.PortsText()
	statusPorts := status
	if ports != "" {
		statusPorts += "  ⇆ " + ports
	}
	var nets []string
	for nn := range c.Nets {
		nets = append(nets, nn)
	}
	sort.Strings(nets)
	type ln struct {
		s  string
		st styleID
	}
	var lines []ln
	switch n.H {
	case 3:
	case 4:
		lines = []ln{{statusPorts, dim}}
	case 5:
		lines = []ln{{c.Image, txt}, {statusPorts, dim}}
	default:
		lines = []ln{{c.Image, txt}, {status, dim}, {"⇆ " + ports, dim}, {"⌁ " + strings.Join(nets, ", "), dim}}
		if ports == "" {
			lines[2] = ln{"(no published ports)", dim}
		}
	}
	for i, l := range lines {
		g.text(n.X+4, n.Y+2+i, fit(l.s, inner-2), l.st)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
