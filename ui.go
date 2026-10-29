package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

const (
	viewCanvas = 0
	viewList   = 1
)

type dataMsg struct {
	cs  []*Container
	err error
}
type tickMsg struct{}

type actionMsg struct {
	verb, name string
	err        error
}

type hdrBtn struct {
	x0, x1 int
	act    int // 0,1 = view; 2 = zoom out; 3 = zoom in
}

type listItem struct {
	header string
	c      *Container
}

type model struct {
	w, h int
	view int

	cs    []*Container
	byID  map[string]*Container
	links []*Link
	lay   *Layout
	sig   string
	ver   int
	err   error
	ready bool

	sel string

	// canvas
	ox, oy   int
	dragging bool
	dragX    int
	dragY    int
	moved    bool
	labels   bool
	zoom     int
	resizing int // 0 none, 1 log pane height, 2 list column width
	logH     int // 0 = auto
	listW    int // 0 = auto
	hdrBtns  []hdrBtn
	cache    *grid
	cacheKey string

	// list
	items      []listItem
	listScroll int

	// logs
	logOpen    bool
	logs       []string
	selA, selB logPos
	hasSel     bool
	selecting  bool
	logScroll  int // rows scrolled up from the bottom (0 = following)
	logGen     int
	logID      string
	logStart   time.Time
	stream     *logStream

	confirmDel string // container ID awaiting delete confirmation
	busy       bool   // a start/stop/restart/rm is in flight

	msg      string
	msgUntil time.Time
	tmuxHint string
}

func newModel() *model {
	m := &model{byID: map[string]*Container{}, zoom: defaultZoom}
	if os.Getenv("TMUX") != "" {
		out, err := exec.Command("tmux", "show-option", "-gv", "mouse").Output()
		if err == nil && strings.TrimSpace(string(out)) == "off" {
			m.tmuxHint = "tmux: mouse is off — run `tmux set -g mouse on` to use the mouse"
		}
	}
	return m
}

func (m *model) Init() tea.Cmd { return tea.Batch(fetchCmd, tickCmd()) }

func fetchCmd() tea.Msg {
	cs, err := fetchContainers()
	return dataMsg{cs, err}
}

func tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// runAction runs a docker lifecycle command in the background and refreshes.
func (m *model) runAction(verb string, c *Container) tea.Cmd {
	if m.busy {
		m.flash("another action is still running…")
		return nil
	}
	m.busy = true
	past := map[string]string{"start": "starting", "stop": "stopping", "restart": "restarting", "rm": "deleting"}[verb]
	m.flash(past + " " + c.Name + "…")
	id, name := c.ID, c.Name
	return func() tea.Msg { return actionMsg{verb, name, containerAction(verb, id)} }
}

func (m *model) flash(s string) {
	m.msg, m.msgUntil = s, time.Now().Add(4*time.Second)
}

// ---- geometry -----------------------------------------------------------

type geom struct {
	cx, cy, cw, ch int // canvas viewport
	lx, ly, lw, lh int // log pane (title row included)
	leftW          int // list view: width of the list column
	listY, listH   int
}

func (m *model) geom() geom {
	var g geom
	body := m.h - 3
	if body < 1 {
		body = 1
	}
	if m.view == viewCanvas {
		g.cx, g.cy, g.cw = 0, 1, m.w
		lh := 0
		if m.logOpen && body >= 12 {
			lh = body * 2 / 5
			if m.logH > 0 {
				lh = m.logH
			}
			lh = clamp(lh, 5, body-4)
		}
		g.ch = body - lh
		g.lx, g.ly, g.lw, g.lh = 0, 1+g.ch, m.w, lh
	} else {
		g.leftW = m.w / 3
		if g.leftW < 26 {
			g.leftW = 26
		}
		if g.leftW > 46 {
			g.leftW = 46
		}
		if m.listW > 0 {
			g.leftW = m.listW
		}
		g.leftW = clamp(g.leftW, 16, m.w-20)
		g.listY, g.listH = 1, body
		g.lx, g.ly, g.lw, g.lh = g.leftW+1, 1, m.w-g.leftW-1, body
	}
	return g
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (m *model) clampPan() {
	if m.lay == nil {
		return
	}
	g := m.geom()
	m.ox = clamp(m.ox, 0, m.lay.W-g.cw)
	m.oy = clamp(m.oy, 0, m.lay.H-g.ch)
}

// setZoom changes the zoom level keeping the canvas point under (px,py)
// (viewport coordinates) roughly in place.
func (m *model) setZoom(z, px, py int) {
	z = clamp(z, 0, len(zoomDims)-1)
	if z == m.zoom || m.lay == nil {
		return
	}
	fx := float64(m.ox+px) / float64(m.lay.W)
	fy := float64(m.oy+py) / float64(m.lay.H)
	m.zoom = z
	m.lay = computeLayout(m.cs, m.links, zoomDims[z])
	m.ox = int(fx*float64(m.lay.W)) - px
	m.oy = int(fy*float64(m.lay.H)) - py
	m.ver++
	m.clampPan()
	m.flash("zoom " + zoomNames[z])
}

func (m *model) zoomBy(d int) {
	g := m.geom()
	m.setZoom(m.zoom+d, g.cw/2, g.ch/2)
}

// resizeLogs grows (d>0) or shrinks the log pane by d steps.
func (m *model) resizeLogs(d int) {
	g := m.geom()
	if m.view == viewCanvas {
		if !m.logOpen {
			return
		}
		m.logH = g.lh + d*3
	} else {
		m.listW = g.leftW - d*4
	}
	m.clampPan()
}

func (m *model) centerOn(id string) {
	if m.lay == nil {
		return
	}
	n := m.lay.Nodes[id]
	if n == nil {
		return
	}
	g := m.geom()
	// only move if the card isn't fully visible
	if n.X >= m.ox && n.X+n.W <= m.ox+g.cw && n.Y >= m.oy && n.Y+n.H <= m.oy+g.ch {
		return
	}
	m.ox = n.X + n.W/2 - g.cw/2
	m.oy = n.Y + n.H/2 - g.ch/2
	m.clampPan()
}

// ---- data ---------------------------------------------------------------

func (m *model) applyData(cs []*Container) {
	m.cs = cs
	m.byID = map[string]*Container{}
	ids := make([]string, 0, len(cs))
	for _, c := range cs {
		m.byID[c.ID] = c
		ids = append(ids, c.ID+"@"+c.Group())
	}
	sort.Strings(ids)
	links := buildLinks(cs)
	lk := make([]string, 0, len(links))
	for _, l := range links {
		lk = append(lk, fmt.Sprint(l.From, l.To, l.ViaHost, l.Ports))
	}
	sort.Strings(lk)
	sig := strings.Join(ids, ",") + "|" + strings.Join(lk, ",")
	m.links = links
	if sig != m.sig || m.lay == nil {
		m.sig = sig
		m.lay = computeLayout(cs, links, zoomDims[m.zoom])
	} else {
		for id, n := range m.lay.Nodes {
			n.C = m.byID[id]
		}
		// links are re-created each refresh; keep the layout's edges in sync
		for _, e := range m.lay.Edges {
			for _, l := range links {
				if l.From == e.From && l.To == e.To && l.ViaHost == e.ViaHost {
					e.Link = l
				}
			}
		}
	}
	m.ver++
	m.clampPan()

	// list items
	m.items = nil
	groups := map[string][]*Container{}
	var names []string
	for _, c := range cs {
		if _, ok := groups[c.Group()]; !ok {
			names = append(names, c.Group())
		}
		groups[c.Group()] = append(groups[c.Group()], c)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := names[i], names[j]
		if (a == "(standalone)") != (b == "(standalone)") {
			return b == "(standalone)"
		}
		return a < b
	})
	for _, n := range names {
		l := groups[n]
		sort.Slice(l, func(i, j int) bool { return l[i].Name < l[j].Name })
		m.items = append(m.items, listItem{header: n})
		for _, c := range l {
			m.items = append(m.items, listItem{c: c})
		}
	}

	if m.sel != "" && m.byID[m.sel] == nil {
		m.sel = ""
		m.logOpen = false
	}
	if m.sel == "" && m.view == viewList {
		m.selectFirst()
	}
	// restart logs if the container was restarted
	if c := m.byID[m.logID]; c != nil && m.stream != nil && !c.StartedAt.Equal(m.logStart) {
		m.stopLogs()
	}
	m.syncLogs()
}

func (m *model) selectFirst() {
	for _, it := range m.items {
		if it.c != nil {
			m.sel = it.c.ID
			return
		}
	}
}

// ---- logs ---------------------------------------------------------------

func (m *model) stopLogs() {
	m.stream.stop()
	m.stream = nil
	m.logID = ""
}

func (m *model) syncLogs() {
	want := ""
	if m.sel != "" && (m.view == viewList || m.logOpen) {
		want = m.sel
	}
	if want == m.logID && (want == "" || m.stream != nil) {
		return
	}
	m.stream.stop()
	m.stream = nil
	m.logID = want
	m.logs = nil
	m.clearSel()
	m.logScroll = 0
	m.logGen++
	if want != "" {
		m.stream = startLogs(want, m.logGen)
		if c := m.byID[want]; c != nil {
			m.logStart = c.StartedAt
		}
	}
}

func wrapText(s string, w int) []string {
	if w < 1 {
		return []string{""}
	}
	if runewidth.StringWidth(s) <= w {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	cw := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if cw+rw > w {
			out = append(out, cur.String())
			cur.Reset()
			cw = 0
		}
		cur.WriteRune(r)
		cw += rw
	}
	out = append(out, cur.String())
	return out
}

type logRow struct {
	s    string
	st   styleID
	line int // index into m.logs (-1 = filler)
	off  int // rune offset of this chunk within the line
}

// visibleLogRows returns the h rows to display. exhausted reports whether
// all lines were wrapped (so total is exact) and total the row count then.
func (m *model) visibleLogRows(w, h int) (rows []logRow, total int, exhausted bool) {
	need := m.logScroll + h
	var rev []logRow
	i := len(m.logs) - 1
	for ; i >= 0 && len(rev) < need; i-- {
		ws := wrapText(m.logs[i], w)
		st := logStyle(m.logs[i])
		offs := make([]int, len(ws))
		o := 0
		for j, c := range ws {
			offs[j] = o
			o += len([]rune(c))
		}
		for j := len(ws) - 1; j >= 0; j-- {
			rev = append(rev, logRow{ws[j], st, i, offs[j]})
		}
	}
	exhausted = i < 0
	total = len(rev)
	if exhausted && m.logScroll > total-h {
		m.logScroll = total - h
		if m.logScroll < 0 {
			m.logScroll = 0
		}
	}
	end := m.logScroll
	for k := end + h - 1; k >= end; k-- {
		if k < len(rev) {
			rows = append(rows, rev[k])
		} else {
			rows = append(rows, logRow{line: -1})
		}
	}
	// rows currently bottom→top reversed; make top→bottom
	// (loop above already walks from the topmost row down)
	return
}

func (m *model) scrollLogs(delta int) {
	g := m.geom()
	m.logScroll += delta
	if m.logScroll < 0 {
		m.logScroll = 0
	}
	m.visibleLogRows(g.lw, max(g.lh-1, 1)) // clamps
}

// ---- update -------------------------------------------------------------

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.clampPan()
	case dataMsg:
		m.ready = true
		m.err = msg.err
		if msg.err == nil {
			m.applyData(msg.cs)
		}
	case tickMsg:
		return m, tea.Batch(fetchCmd, tickCmd())
	case actionMsg:
		m.busy = false
		if msg.err != nil {
			m.flash(msg.verb + " " + msg.name + " failed: " + msg.err.Error())
		} else {
			m.flash(msg.verb + " " + msg.name + ": ok")
		}
		return m, fetchCmd
	case logBatchMsg:
		if msg.gen == m.logGen {
			m.logs = append(m.logs, msg.lines...)
			if drop := len(m.logs) - maxLogLines; drop > 0 {
				m.logs = m.logs[drop:]
				m.selA.line -= drop
				m.selB.line -= drop
				if m.selA.line < 0 || m.selB.line < 0 {
					m.clearSel()
				}
			}
			if m.logScroll > 0 {
				m.logScroll += len(msg.lines)
			}
		}
	case tea.KeyMsg:
		return m.onKey(msg)
	case tea.MouseMsg:
		m.onMouse(msg)
	}
	return m, nil
}

func (m *model) setView(v int) {
	m.view = v
	if v == viewList && m.sel == "" {
		m.selectFirst()
	}
	m.clampPan()
	m.syncLogs()
	if v == viewCanvas && m.sel != "" {
		m.centerOn(m.sel)
	}
}

func (m *model) selectID(id string) {
	m.sel = id
	m.logScroll = 0
	m.syncLogs()
}

func (m *model) cycle(d int) {
	var ids []string
	if m.view == viewCanvas {
		if m.lay == nil {
			return
		}
		for _, n := range m.lay.Order {
			ids = append(ids, n.C.ID)
		}
	} else {
		for _, it := range m.items {
			if it.c != nil {
				ids = append(ids, it.c.ID)
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	idx := -1
	for i, id := range ids {
		if id == m.sel {
			idx = i
		}
	}
	idx = (idx + d + len(ids)) % len(ids)
	if idx < 0 {
		idx = 0
	}
	m.selectID(ids[idx])
	if m.view == viewCanvas {
		m.logOpen = m.logOpen || false
		m.centerOn(m.sel)
		m.syncLogs()
	} else {
		m.ensureListVisible()
	}
}

func (m *model) ensureListVisible() {
	g := m.geom()
	for i, it := range m.items {
		if it.c != nil && it.c.ID == m.sel {
			if i < m.listScroll {
				m.listScroll = i
				if i > 0 && m.items[i-1].c == nil {
					m.listScroll = i - 1
				}
			}
			if i >= m.listScroll+g.listH {
				m.listScroll = i - g.listH + 1
			}
		}
	}
}

func (m *model) onKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := m.geom()
	pageLog := max(g.lh-2, 3)
	if m.confirmDel != "" {
		id := m.confirmDel
		m.confirmDel = ""
		if c := m.byID[id]; c != nil && (k.String() == "y" || k.String() == "enter") {
			return m, m.runAction("rm", c)
		}
		m.flash("delete cancelled")
		return m, nil
	}
	switch s := k.String(); s {
	case "ctrl+c", "q":
		m.stream.stop()
		return m, tea.Quit
	case "1":
		m.setView(viewCanvas)
	case "2":
		m.setView(viewList)
	case "tab", "v":
		m.setView(1 - m.view)
	case "r":
		m.flash("refreshing…")
		return m, fetchCmd
	case "+", "=":
		if m.view == viewCanvas {
			m.zoomBy(1)
		}
	case "-", "_":
		if m.view == viewCanvas {
			m.zoomBy(-1)
		}
	case "0":
		if m.view == viewCanvas {
			m.zoomBy(defaultZoom - m.zoom)
		}
	case "]":
		m.resizeLogs(1)
	case "[":
		m.resizeLogs(-1)
	case "e":
		m.labels = !m.labels
		m.ver++
	case "n":
		m.cycle(1)
	case "p":
		m.cycle(-1)
	case "enter":
		if m.view == viewCanvas && m.sel != "" {
			m.logOpen = !m.logOpen
			m.clampPan()
			m.syncLogs()
		}
	case "s":
		if c := m.byID[m.sel]; c != nil {
			if c.Running() {
				return m, m.runAction("stop", c)
			}
			return m, m.runAction("start", c)
		}
	case "R":
		if c := m.byID[m.sel]; c != nil {
			return m, m.runAction("restart", c)
		}
	case "d", "delete":
		if c := m.byID[m.sel]; c != nil {
			m.confirmDel = c.ID
		}
	case "y":
		m.copySelection()
	case "Y":
		m.copyAll()
	case "esc":
		if m.hasSel {
			m.clearSel()
		} else if m.view == viewCanvas && m.logOpen {
			m.logOpen = false
			m.clampPan()
			m.syncLogs()
		}
	case "pgup", "ctrl+u":
		m.scrollLogs(pageLog)
	case "pgdown", "ctrl+d":
		m.scrollLogs(-pageLog)
	case "end", "G", "f":
		m.logScroll = 0
	case "home", "g":
		if m.view == viewCanvas && (s == "home" || s == "g") {
			m.ox, m.oy = 0, 0
		}
	case "up", "k", "down", "j", "left", "h", "right", "l",
		"K", "J", "H", "L", "shift+up", "shift+down", "shift+left", "shift+right":
		dx, dy := 0, 0
		step, big := 2, 10
		switch s {
		case "up", "k":
			dy = -step
		case "down", "j":
			dy = step
		case "left", "h":
			dx = -step * 2
		case "right", "l":
			dx = step * 2
		case "K", "shift+up":
			dy = -big
		case "J", "shift+down":
			dy = big
		case "H", "shift+left":
			dx = -big * 3
		case "L", "shift+right":
			dx = big * 3
		}
		if m.view == viewCanvas {
			m.ox += dx
			m.oy += dy
			m.clampPan()
		} else if dy != 0 {
			if dy < 0 {
				m.cycle(-1)
			} else {
				m.cycle(1)
			}
		}
	}
	return m, nil
}

func (m *model) onMouse(ev tea.MouseMsg) {
	g := m.geom()
	x, y := ev.X, ev.Y
	inLog := g.lh > 0 && x >= g.lx && x < g.lx+g.lw && y >= g.ly && y < g.ly+g.lh
	inCanvas := m.view == viewCanvas && x >= g.cx && x < g.cx+g.cw && y >= g.cy && y < g.cy+g.ch
	inList := m.view == viewList && x < g.leftW && y >= g.listY && y < g.listY+g.listH

	switch ev.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown, tea.MouseButtonWheelLeft, tea.MouseButtonWheelRight:
		if ev.Action != tea.MouseActionPress {
			return
		}
		up := ev.Button == tea.MouseButtonWheelUp
		sgn := 1
		if up {
			sgn = -1
		}
		switch {
		case inLog:
			m.scrollLogs(-sgn * 3)
		case inList:
			m.listScroll = clamp(m.listScroll+sgn*3, 0, max(len(m.items)-g.listH, 0))
		case inCanvas:
			switch {
			case ev.Button == tea.MouseButtonWheelLeft:
				m.ox -= 8
			case ev.Button == tea.MouseButtonWheelRight:
				m.ox += 8
			case ev.Ctrl:
				m.setZoom(m.zoom-sgn, x-g.cx, y-g.cy)
			case ev.Shift || ev.Alt:
				m.ox += sgn * 8
			default:
				m.oy += sgn * 3
			}
			m.clampPan()
		}
		return
	case tea.MouseButtonLeft:
	default:
		if ev.Action == tea.MouseActionRelease {
			m.dragging, m.resizing = false, 0
			m.finishSelect()
		}
		return
	}

	switch ev.Action {
	case tea.MouseActionPress:
		m.dragging, m.resizing = false, 0
		m.moved = false
		switch {
		case m.view == viewList && x == g.leftW && y > 0:
			m.resizing = 2
		case y == 0:
			m.headerClick(x)
		case inCanvas:
			cx, cy := m.ox+x-g.cx, m.oy+y-g.cy
			if n := m.nodeAt(cx, cy); n != nil {
				m.selectID(n.C.ID)
				m.logOpen = true
				m.clampPan()
				m.syncLogs()
			} else {
				m.dragging = true
				m.dragX, m.dragY = x, y
			}
		case inLog:
			if m.view == viewCanvas && y == g.ly {
				if x >= g.lx+g.lw-5 {
					m.logOpen = false
					m.clampPan()
					m.syncLogs()
				} else {
					m.resizing = 1
				}
			} else if y > g.ly {
				now := time.Now()
				if now.Sub(lastClick.t) < 400*time.Millisecond && lastClick.x == x && lastClick.y == y {
					m.selectLine(x, y)
					lastClick.t = time.Time{}
				} else if p, ok := m.posAt(x, y); ok {
					m.selA, m.selB = p, p
					m.hasSel, m.selecting = false, true
					lastClick.t, lastClick.x, lastClick.y = now, x, y
				}
			}
		case inList:
			idx := m.listScroll + (y - g.listY)
			if idx >= 0 && idx < len(m.items) && m.items[idx].c != nil {
				m.selectID(m.items[idx].c.ID)
			}
		}
	case tea.MouseActionMotion:
		if m.selecting {
			if p, ok := m.posAt(x, y); ok {
				m.selB = p
				m.hasSel = true
			}
			if y <= g.ly {
				m.scrollLogs(1)
			} else if y >= g.ly+g.lh-1 {
				m.scrollLogs(-1)
			}
		} else if m.resizing == 1 {
			m.logH = (m.h - 2) - y
			m.clampPan()
		} else if m.resizing == 2 {
			m.listW = x
		} else if m.dragging {
			m.ox -= x - m.dragX
			m.oy -= y - m.dragY
			m.dragX, m.dragY = x, y
			m.clampPan()
		}
	case tea.MouseActionRelease:
		m.dragging, m.resizing = false, 0
		m.finishSelect()
	}
}

func (m *model) nodeAt(x, y int) *Node {
	if m.lay == nil {
		return nil
	}
	for _, n := range m.lay.Order {
		if n.contains(x, y) {
			return n
		}
	}
	return nil
}

const titleText = " ▣ dockerinspec "

var tabNames = []string{" 1 Canvas ", " 2 List "}

func (m *model) headerClick(x int) {
	for _, b := range m.hdrBtns {
		if x >= b.x0 && x < b.x1 {
			switch b.act {
			case 0, 1:
				m.setView(b.act)
			case 2:
				m.zoomBy(-1)
			case 3:
				m.zoomBy(1)
			}
			return
		}
	}
}

// ---- view ---------------------------------------------------------------

func padTo(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw > w {
		return ansi.Truncate(s, w, "")
	}
	return s + strings.Repeat(" ", w-sw)
}

func seg(text, fg, bg string, bold bool) string {
	return render(sty(fg, bg, bold), text)
}

func (m *model) View() string {
	if m.w == 0 || m.h == 0 {
		return ""
	}
	if m.w < 30 || m.h < 8 {
		return "terminal too small"
	}
	var lines []string
	lines = append(lines, m.headerLine())
	g := m.geom()
	if m.view == viewCanvas {
		lines = append(lines, m.canvasLines(g)...)
		if g.lh > 0 {
			lines = append(lines, m.logLines(g.lw, g.lh, true)...)
		}
	} else {
		left := m.listLines(g)
		right := m.logLines(g.lw, g.lh, false)
		sep := seg("│", "240", "", false)
		for i := 0; i < g.lh; i++ {
			lines = append(lines, padTo(left[i], g.leftW)+sep+right[i])
		}
	}
	lines = append(lines, m.infoLine(), m.footerLine())
	for len(lines) < m.h {
		lines = append(lines, "")
	}
	return strings.Join(lines[:m.h], "\n")
}

func (m *model) headerLine() string {
	var sb strings.Builder
	bg := "236"
	sb.WriteString(seg(titleText, "255", "24", true))
	sb.WriteString(seg(" ", "", bg, false))
	m.hdrBtns = m.hdrBtns[:0]
	for i, t := range tabNames {
		x0 := ansi.StringWidth(sb.String())
		m.hdrBtns = append(m.hdrBtns, hdrBtn{x0, x0 + len([]rune(t)), i})
		if i == m.view {
			sb.WriteString(seg(t, "16", "51", true))
		} else {
			sb.WriteString(seg(t, "250", "238", false))
		}
		sb.WriteString(seg(" ", "", bg, false))
	}
	if m.view == viewCanvas {
		x0 := ansi.StringWidth(sb.String())
		sb.WriteString(seg(" − ", "255", "238", true))
		sb.WriteString(seg(" "+zoomNames[m.zoom]+" ", "250", bg, false))
		x1 := ansi.StringWidth(sb.String())
		sb.WriteString(seg(" + ", "255", "238", true))
		m.hdrBtns = append(m.hdrBtns, hdrBtn{x0, x0 + 3, 2}, hdrBtn{x1, x1 + 3, 3})
		sb.WriteString(seg(" ", "", bg, false))
	}
	running := 0
	for _, c := range m.cs {
		if c.Running() {
			running++
		}
	}
	var stats string
	switch {
	case m.err != nil:
		stats = "docker: " + m.err.Error()
	case !m.ready:
		stats = "loading…"
	default:
		ngroups := 0
		if m.lay != nil {
			ngroups = len(m.lay.Groups)
		}
		stats = fmt.Sprintf("%d containers (%d up) · %d groups · %d links", len(m.cs), running, ngroups, len(m.links))
		if m.view == viewCanvas && m.lay != nil {
			stats += fmt.Sprintf(" · view %d,%d/%dx%d", m.ox, m.oy, m.lay.W, m.lay.H)
		}
	}
	used := ansi.StringWidth(sb.String())
	sb.WriteString(seg(padTo(" "+stats, m.w-used), "250", bg, false))
	return sb.String()
}

func (m *model) canvasLines(g geom) []string {
	out := make([]string, g.ch)
	if m.lay == nil {
		for i := range out {
			out[i] = strings.Repeat(" ", g.cw)
		}
		if len(out) > 0 {
			msg := "no containers"
			if !m.ready {
				msg = "loading…"
			}
			out[len(out)/2] = padTo(strings.Repeat(" ", max((g.cw-len(msg))/2, 0))+msg, g.cw)
		}
		return out
	}
	key := fmt.Sprint(m.ver, m.sel, m.labels)
	if m.cache == nil || m.cacheKey != key {
		m.cache = m.lay.paint(m.sel, m.labels)
		m.cacheKey = key
	}
	for i := range out {
		out[i] = m.cache.row(m.oy+i, m.ox, g.cw)
	}
	return out
}

func (m *model) listLines(g geom) []string {
	out := make([]string, g.listH)
	for i := range out {
		idx := m.listScroll + i
		if idx >= len(m.items) {
			out[i] = ""
			continue
		}
		it := m.items[idx]
		w := g.leftW
		if it.c == nil {
			cnt := 0
			for _, x := range m.items[idx+1:] {
				if x.c == nil {
					break
				}
				cnt++
			}
			out[i] = padTo(seg(" ▾ "+it.header, "111", "", true)+seg(fmt.Sprintf(" (%d)", cnt), "244", "", false), w)
			continue
		}
		c := it.c
		icon, st := stateStyle(c)
		selected := c.ID == m.sel
		bg := ""
		if selected {
			bg = "238"
		}
		fg := map[styleID]string{stRun: "42", stBad: "203", stWarn: "214", stDim: "243"}[st]
		nameFg := "252"
		if !c.Running() {
			nameFg = "245"
		}
		right := c.PortsText()
		nameW := w - 5
		if right != "" {
			rw := min(len([]rune(right))+1, w/2)
			right = fit(right, rw-1)
			nameW -= rw
		}
		line := seg("  ", "", bg, false) + seg(string(icon), fg, bg, true) + seg(" ", "", bg, false) +
			seg(padTo(fit(c.Name, nameW), nameW), nameFg, bg, selected)
		if right != "" {
			line += seg(" "+right, "244", bg, false)
		}
		line = padTo(line, w-0)
		if selected {
			line = padTo(line, w)
		}
		out[i] = line
	}
	return out
}

func (m *model) logLines(w, h int, closable bool) []string {
	out := make([]string, 0, h)
	c := m.byID[m.sel]
	bg := "236"
	title := seg(" Logs ", "16", "75", true)
	if c == nil {
		title += seg(padTo("  select a container (click a card or press n/p)", w-6), "250", bg, false)
		out = append(out, title)
		for len(out) < h {
			out = append(out, strings.Repeat(" ", w))
		}
		return out
	}
	status := seg(" following ", "42", bg, false)
	if m.logScroll > 0 {
		status = seg(fmt.Sprintf(" paused ↑%d (End to follow) ", m.logScroll), "214", bg, false)
	}
	right := ""
	if closable {
		right = " ✕ "
	}
	left := title + seg(" "+c.Name+" ", "255", bg, true) + seg("· "+c.StatusText(), "250", bg, false) + status
	pad := w - ansi.StringWidth(left) - len([]rune(right))
	if pad < 0 {
		left = padTo(left, w-len([]rune(right)))
		pad = 0
	}
	out = append(out, left+seg(strings.Repeat(" ", pad), "", bg, false)+seg(right, "203", bg, true))
	rows, _, _ := m.visibleLogRows(w, h-1)
	for _, r := range rows {
		out = append(out, m.renderRow(r, w))
	}
	if len(m.logs) == 0 && h > 2 {
		out[2] = padTo(seg("  (no logs yet)", "243", "", false), w)
	}
	return out
}

func (m *model) infoLine() string {
	c := m.byID[m.sel]
	if c == nil {
		return padTo(seg(" click a container or press n/p to select · links: ", "243", "", false)+
			seg("→ talks to", "51", "", true)+seg("  ", "", "", false)+seg("← used by", "213", "", true)+
			seg("   ┄ via localhost/host.docker.internal", "243", "", false), m.w)
	}
	var sb strings.Builder
	sb.WriteString(seg(" "+c.Name, "255", "", true))
	sb.WriteString(seg(" · "+c.Image, "244", "", false))
	var outs, ins []string
	for _, l := range m.links {
		desc := ""
		if len(l.Ports) > 0 {
			desc = ":" + itoa(l.Ports[0])
		}
		if l.ViaHost {
			desc += "↗"
		}
		if l.From == c.ID {
			if t := m.byID[l.To]; t != nil {
				outs = append(outs, t.Name+desc+" ["+strings.Join(l.Keys, ",")+"]")
			}
		}
		if l.To == c.ID {
			if f := m.byID[l.From]; f != nil {
				ins = append(ins, f.Name)
			}
		}
	}
	if len(outs) > 0 {
		sb.WriteString(seg("  → "+strings.Join(outs, ", "), "51", "", false))
	}
	if len(ins) > 0 {
		sb.WriteString(seg("  ← "+strings.Join(ins, ", "), "213", "", false))
	}
	return padTo(sb.String(), m.w)
}

func (m *model) footerLine() string {
	var text, fg string
	switch {
	case m.confirmDel != "":
		name := ""
		if c := m.byID[m.confirmDel]; c != nil {
			name = c.Name
		}
		text, fg = " delete "+name+" (force, removes it for good)? y/enter confirm · any other key cancels", "203"
	case m.msg != "" && time.Now().Before(m.msgUntil):
		text, fg = " "+m.msg, "229"
	case m.tmuxHint != "":
		text, fg = " "+m.tmuxHint, "214"
	case m.view == viewCanvas:
		text, fg = " drag/wheel/arrows pan · +/- or ctrl+wheel zoom · [ ] or drag log border: resize logs · click card: logs · drag logs: copy · n/p next · s start/stop · R restart · d delete · e labels · tab list · q quit", "244"
	default:
		text, fg = " click/↑↓/j k select · wheel scroll · drag logs: copy · PgUp/PgDn scroll · [ ] resize · End follow · s start/stop · R restart · d delete · tab canvas · r refresh · q quit", "244"
	}
	return seg(padTo(text, m.w), fg, "", false)
}
