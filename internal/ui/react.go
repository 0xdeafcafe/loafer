package ui

import (
	"errors"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/emoji"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
)

// r on a message opens a picker, a grid of emoji as Slack's is: what you
// use first, then each category, then the workspace's own. Typing
// narrows it to what matches; the arrows move in the grid, enter adds the
// reaction, or takes it away if it's already yours, and a click does the
// same. On the message itself, 1 to 9 toggle its reactions in the order
// they're drawn. Either way the store changes at once, Slack is told in
// the background, and its event, when it comes, finds it done.

type reactor struct {
	on       bool
	conv, ts string
	mine     []string // the names you've already reacted with on the message
	query    []rune
	at       int // the cell under the cursor
	top      int // the first line shown
	full     grid
	found    grid // what the query matches
	gx, gy   int  // where the grid's first cell was drawn, for clicks
	gh       int  // how many lines of it were
	cols     int  // emoji to a line
}

// grid is emoji laid out cols to a line, under their sections' headings.
type grid struct {
	secs  []emojiSection
	cols  int
	cells []string
	lines []pickLine
}

type emojiSection struct {
	title string
	names []string
}

// pickLine is a heading, or cells[from:to].
type pickLine struct {
	head     string
	from, to int
}

type reactedMsg struct{ err error }

const (
	pickCap   = 200 // matches shown
	pickCellW = 4   // the ▍ marker, the emoji, a gap
	pickCols  = 10
)

// Slack's order of the categories, iamcal's names to theirs.
var pickGroups = [...][2]string{
	{"Smileys & Emotion", "Smileys & emotion"}, {"People & Body", "People"}, {"Animals & Nature", "Nature"},
	{"Food & Drink", "Food & drink"}, {"Activities", "Activities"}, {"Travel & Places", "Travel & places"},
	{"Objects", "Objects"}, {"Symbols", "Symbols"}, {"Flags", "Flags"},
}

// reactPicker opens the picker on msg.
func (m *Model) reactPicker(msg slack.Message) {
	m.emo.pick = reactor{on: true, conv: m.open, ts: msg.TS, cols: min(pickCols, max(1, (m.w-8)/pickCellW))}
	p := &m.emo.pick
	m.st.Read(func(v store.View) {
		for _, r := range msg.Reactions {
			if slices.Contains(r.Users, v.Self()) {
				p.mine = append(p.mine, r.Name)
			}
		}
		custom := v.CustomEmoji()
		var used []string
		for _, n := range slices.Concat(m.emo.recent, quick) {
			if _, ok := emoji.Lookup(n); (ok || slices.Contains(custom, n)) && !slices.Contains(used, n) {
				used = append(used, n)
			}
		}
		secs := []emojiSection{{"Frequently used", used}}
		groups := emoji.Groups()
		for _, g := range pickGroups {
			if i := slices.IndexFunc(groups, func(x emoji.Group) bool { return x.Name == g[0] }); i >= 0 {
				secs = append(secs, emojiSection{g[1], groups[i].Names})
			}
		}
		if len(custom) > 0 {
			secs = append(secs, emojiSection{"Custom", custom})
		}
		p.full.secs = secs
	})
}

// shown is the grid on screen, laid out: what the query found, or
// everything.
func (p *reactor) shown() *grid {
	g := &p.full
	if len(p.query) > 0 {
		g = &p.found
	}
	g.lay(p.cols)
	return g
}

// lay lays g out cols to a line, if it isn't already.
func (g *grid) lay(cols int) {
	if g.cols == cols && g.lines != nil {
		return
	}
	g.cols, g.cells, g.lines = cols, g.cells[:0], g.lines[:0]
	for _, s := range g.secs {
		if len(s.names) == 0 {
			continue
		}
		if s.title != "" {
			g.lines = append(g.lines, pickLine{head: s.title, from: len(g.cells), to: len(g.cells)})
		}
		for i := 0; i < len(s.names); i += cols {
			from := len(g.cells)
			g.cells = append(g.cells, s.names[i:min(i+cols, len(s.names))]...)
			g.lines = append(g.lines, pickLine{from: from, to: len(g.cells)})
		}
	}
}

// lineOf is the line cell at is on.
func (g *grid) lineOf(at int) int {
	return sort.Search(len(g.lines), func(i int) bool { return g.lines[i].to > at })
}

// findReact narrows the picker to what the query matches.
func (m *Model) findReact(v store.View) {
	p := &m.emo.pick
	p.at, p.top = 0, 0
	if len(p.query) == 0 {
		return
	}
	hits := m.emo.find(v, string(p.query), true, pickCap)
	names := make([]string, len(hits))
	for i, h := range hits {
		names[i] = h.name
	}
	p.found = grid{secs: []emojiSection{{"", names}}, cells: p.found.cells, lines: p.found.lines}
}

// reactNth toggles the n'th reaction (1 to 9) on the selected message.
func (m *Model) reactNth(n int) tea.Cmd {
	msg, ok := m.selected()
	if !ok {
		return m.say("pick a message first (↑)", false)
	}
	if n > len(msg.Reactions) {
		return m.say("no reaction "+string(rune('0'+n))+" on that one", false)
	}
	r, mine := msg.Reactions[n-1], false
	m.st.Read(func(v store.View) { mine = slices.Contains(r.Users, v.Self()) })
	return m.react(m.open, msg.TS, r.Name, !mine)
}

// react adds or takes away your reaction here, and at Slack in the
// background, putting back what Slack has if it refuses.
func (m *Model) react(conv, ts, name string, add bool) tea.Cmd {
	name = emoji.Canon(name) // or thumbsup and Slack's +1 would be two chips
	if add {
		m.emo.used(name)
	}
	m.st.React(conv, ts, name, add)
	return func() tea.Msg {
		err := m.api.React(m.ctx, conv, ts, name, add)
		var e *slack.Error
		if errors.As(err, &e) && (e.Code == "already_reacted" || e.Code == "no_reaction") {
			err = nil // already how you wanted it
		}
		if err != nil {
			_ = m.st.Refresh(m.ctx, m.api, conv)
		}
		return reactedMsg{err}
	}
}

// pickIt closes the picker and toggles the emoji in cell i.
func (m *Model) pickIt(i int) tea.Cmd {
	p := &m.emo.pick
	g := p.shown()
	p.on = false
	if i < 0 || i >= len(g.cells) {
		return nil
	}
	name := emoji.Canon(g.cells[i])
	return m.react(p.conv, p.ts, name, !slices.Contains(p.mine, name))
}

// pickMove moves the cursor d lines down (up if d < 0), keeping its
// column where the line is long enough, and scrolls it into view.
func (p *reactor) pickMove(d int) {
	g := p.shown()
	if len(g.cells) == 0 {
		return
	}
	li := g.lineOf(p.at)
	col := p.at - g.lines[li].from
	for n := max(d, -d); n > 0; n-- {
		next := li
		for {
			next += d / max(d, -d)
			if next < 0 || next >= len(g.lines) || g.lines[next].head == "" {
				break
			}
		}
		if next < 0 || next >= len(g.lines) {
			break
		}
		li = next
	}
	p.at = min(g.lines[li].from+col, g.lines[li].to-1)
	p.seen()
}

// seen scrolls the cursor's line into view, its heading too if it's the
// section's first.
func (p *reactor) seen() {
	g := p.shown()
	li := g.lineOf(p.at)
	if li > 0 && g.lines[li-1].head != "" {
		li--
	}
	if li < p.top {
		p.top = li
	}
	if p.gh > 0 && g.lineOf(p.at) >= p.top+p.gh {
		p.top = g.lineOf(p.at) - p.gh + 1
	}
}

func (m *Model) reactKey(k tea.KeyPressMsg) tea.Cmd {
	p := &m.emo.pick
	switch k.String() {
	case "esc", "ctrl+c":
		p.on = false
	case "enter":
		return m.pickIt(p.at)
	case "left":
		p.at = max(0, p.at-1)
		p.seen()
	case "right":
		p.at = min(max(0, len(p.shown().cells)-1), p.at+1)
		p.seen()
	case "up", "ctrl+p":
		p.pickMove(-1)
	case "down", "ctrl+n":
		p.pickMove(1)
	case "pgup":
		p.pickMove(-max(1, p.gh-1))
	case "pgdown":
		p.pickMove(max(1, p.gh-1))
	case "backspace":
		if len(p.query) > 0 {
			p.query = p.query[:len(p.query)-1]
			m.st.Read(m.findReact)
		}
	case "ctrl+u", "ctrl+w":
		p.query = p.query[:0]
		m.st.Read(m.findReact)
	default:
		if k.Text != "" {
			p.query = append(p.query, []rune(k.Text)...)
			m.st.Read(m.findReact)
		}
	}
	return nil
}

// pickMouse takes a click or the wheel while the picker's open: a click
// on an emoji reacts with it, one outside the picker closes it, and the
// wheel scrolls the grid.
func (m *Model) pickMouse(msg tea.MouseMsg) tea.Cmd {
	p := &m.emo.pick
	g := p.shown()
	mo := msg.Mouse()
	if _, ok := msg.(tea.MouseWheelMsg); ok {
		d := 3
		if mo.Button == tea.MouseWheelUp {
			d = -3
		}
		p.top = max(0, min(p.top+d, len(g.lines)-p.gh))
		return nil
	}
	if _, ok := msg.(tea.MouseClickMsg); !ok || mo.Button != tea.MouseLeft {
		return nil
	}
	li, col := p.top+mo.Y-p.gy, (mo.X-p.gx)/pickCellW
	if mo.Y < p.gy || mo.Y >= p.gy+p.gh || mo.X < p.gx || col >= g.cols {
		if mo.Y < p.gy-3 || mo.Y > p.gy+p.gh+2 || mo.X < p.gx-2 || mo.X >= p.gx+g.cols*pickCellW+2 {
			p.on = false // outside the box
		}
		return nil
	}
	if li < len(g.lines) && g.lines[li].head == "" && g.lines[li].from+col < g.lines[li].to {
		return m.pickIt(g.lines[li].from + col)
	}
	return nil
}

// --- drawing ---

// overlayReact draws the picker over the frame, which goes faint behind
// it, as overlayJump does. Only the lines on screen are drawn.
func (m *Model) overlayReact(v store.View, frame []canvas.Row) []canvas.Row {
	ink, p := m.pal.Main, &m.emo.pick
	if p.cols = min(pickCols, (m.w-8)/pickCellW); p.cols < 5 || m.h < 16 {
		return frame
	}
	g := p.shown()
	inner := p.cols * pickCellW
	bw := inner + 4
	top := min(m.h/8, 4)
	p.gh = max(3, min(m.h/2-4, len(g.lines)))
	p.top = max(0, min(p.top, len(g.lines)-p.gh))
	edge, fill := m.pal.Orange, ink.Text.Bg(ink.Sel.BG)

	line := func(r canvas.Row) canvas.Row {
		return append(append(canvas.Row{canvas.T("│ ", edge.Bg(fill.BG))}, canvas.Fit(r, inner, fill)...), canvas.T(" │", edge.Bg(fill.BG)))
	}
	edgeRow := func(l, label, r string) canvas.Row {
		row := canvas.Row{canvas.T(l+"─ ", edge), canvas.T(label, ink.Sub.With(canvas.Bold))}
		return append(row, canvas.T(" "+strings.Repeat("─", max(0, bw-row.Width()-2))+r, edge))
	}
	rule := line(canvas.Row{canvas.T(strings.Repeat("─", inner), fill.Fg(ink.Faint.FG))})
	box := []canvas.Row{edgeRow("╭", "☺ React", "╮")}
	input := canvas.Row{canvas.T("❯ ", fill.Fg(m.pal.Orange.FG).With(canvas.Bold)), canvas.T(string(p.query), fill.Fg(ink.Bright.FG)), canvas.T("▏", fill.Fg(m.pal.Orange.FG))}
	if len(p.query) == 0 {
		input = append(input, canvas.T("search emoji", fill.Fg(ink.Dim.FG)))
	}
	box = append(box, line(input), rule)

	p.gy = top + len(box)
	for i := p.top; i < p.top+p.gh; i++ {
		switch {
		case i < len(g.lines) && g.lines[i].head != "":
			head := canvas.Row{canvas.T(g.lines[i].head+" ", fill.Fg(ink.Sub.FG).With(canvas.Bold))}
			box = append(box, line(append(head, canvas.T(strings.Repeat("─", max(0, inner-head.Width())), fill.Fg(ink.Faint.FG)))))
		case i < len(g.lines):
			box = append(box, line(m.emojiRow(v, g, g.lines[i], fill)))
		case i == 0:
			box = append(box, line(canvas.Row{canvas.T("  nothing matches", fill.Fg(ink.Dim.FG))}))
		default:
			box = append(box, line(nil))
		}
	}

	// The emoji under the cursor, by name.
	box = append(box, rule)
	var name canvas.Row
	if p.at < len(g.cells) {
		n := g.cells[p.at]
		name = canvas.Row{m.pickGlyph(v, n, fill), canvas.T(" :"+n+":", fill.Fg(ink.Text.FG))}
		if slices.Contains(p.mine, emoji.Canon(n)) {
			name = rightAlign(name, canvas.Row{canvas.T("✓ yours, enter takes it off", fill.Fg(m.pal.Green.FG))}, inner, fill)
		}
	}
	box = append(box, line(name), edgeRow("╰", "↑↓←→ choose · enter react · esc close", "╯"))

	out := make([]canvas.Row, len(frame))
	for i, r := range frame {
		out[i] = faint(r, ink.Faint.FG)
	}
	left := (m.w - bw) / 2
	p.gx = left + 2
	for i, r := range box {
		if top+i < len(out)-1 {
			out[top+i] = canvas.Splice(out[top+i], left, r)
		}
	}
	return out
}

// emojiRow is a line of the grid's cells, the cursor's with the orange ▍.
func (m *Model) emojiRow(v store.View, g *grid, l pickLine, fill canvas.Style) canvas.Row {
	p := &m.emo.pick
	row := make(canvas.Row, 0, 3*(l.to-l.from))
	for i := l.from; i < l.to; i++ {
		base, mark := fill, " "
		if i == p.at {
			base, mark = fill.Bg(m.pal.Main.Hover.BG), "▍"
		}
		seg := m.pickGlyph(v, g.cells[i], base)
		row = append(row, canvas.T(mark, base.Fg(m.pal.Orange.FG)), seg, canvas.T("   "[:max(0, pickCellW-1-seg.W)], base))
	}
	return row
}

// pickGlyph is an emoji as the terminal draws it: its character, a
// custom one's picture where pictures are drawn, else ◌.
func (m *Model) pickGlyph(v store.View, name string, st canvas.Style) canvas.Seg {
	if c, ok := emoji.Lookup(name); ok {
		return canvas.T(c, st)
	}
	if pic, ok := inlinePic(v.Emoji(name)); ok {
		return pic
	}
	return canvas.T("◌", st.Fg(m.pal.Main.Dim.FG))
}
