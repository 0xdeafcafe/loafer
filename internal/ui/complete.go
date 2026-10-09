package ui

import (
	"slices"
	"strings"
	"unicode"

	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
)

// Typing @ or # in the composer opens a small list above the box. An
// item is general: what to show, what it is, what goes in the box, and
// the code Slack is sent for it ("" for plain text), so other triggers
// (:emoji:) can offer through the same popup.

type item struct {
	label  string // shown, and put in the box
	detail string
	code   string
	lit    []int // runes of label the query matched
	score  int
}

type popup struct {
	on    bool
	from  int // where the trigger sits in the input
	shut  int // 1 + the trigger esc closed, so it stays closed
	at    int
	items []item
}

const popCap = 8

// wordRune is what a query can hold: names and channels have no spaces.
func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-'
}

// refreshPop looks at the word before the cursor and opens, narrows or
// closes the popup to suit. It runs after every key in the composer.
func (m *Model) refreshPop() {
	p := &m.pop
	p.on = false
	from := m.cur
	for from > 0 && wordRune(m.input[from-1]) {
		from--
	}
	if from == 0 || (m.input[from-1] != '@' && m.input[from-1] != '#' && m.input[from-1] != ':') {
		p.shut = 0
		return
	}
	from--
	if (from > 0 && !unicode.IsSpace(m.input[from-1])) || p.shut == from+1 ||
		slices.ContainsFunc(m.ments, func(mn mention) bool { return mn.from <= from && from < mn.to }) {
		return
	}
	trigger, q := m.input[from], string(m.input[from+1:m.cur])
	p.items = p.items[:0]
	m.st.Read(func(v store.View) {
		switch trigger {
		case '@':
			p.items = m.people(v, q, p.items)
		case '#':
			p.items = m.channels(v, q, p.items)
		default:
			p.items = m.shortcodes(v, q, p.items)
		}
	})
	p.items = top(p.items, popCap, func(a, b item) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return compareFold(a.label, b.label)
	})
	p.on, p.from, p.at = len(p.items) > 0, from, 0
}

// people finds who q means: the open conversation's people and your
// recent DMs first, then the rest; bots after, and only when asked for.
func (m *Model) people(v store.View, q string, out []item) []item {
	boost := map[string]int{}
	c := v.Conv(m.open)
	if c != nil && c.Kind == store.IM {
		boost[c.User] = 30
	}
	if w := v.Window(m.open); w != nil {
		for _, msg := range w.Msgs {
			boost[msg.User] = max(boost[msg.User], 30)
		}
	}
	var ims []*store.Conv
	for _, id := range m.sideConvs() {
		if c := v.Conv(id); c != nil && c.Kind == store.IM {
			ims = append(ims, c)
		}
	}
	slices.SortFunc(ims, func(a, b *store.Conv) int { return strings.Compare(b.Latest, a.Latest) })
	for i, c := range ims {
		boost[c.User] = max(boost[c.User], 20-min(i, 15))
	}
	v.EachPerson(func(p *store.Person) {
		if p.Deleted || p.ID == "" || (p.Bot && q == "") {
			return
		}
		s1, lit, ok1 := match(q, p.Name)
		s2, _, ok2 := match(q, p.Handle)
		if !ok1 && !ok2 {
			return
		}
		score := boost[p.ID]
		switch {
		case ok1 && ok2:
			score += max(s1, s2)
		case ok1:
			score += s1
		default:
			score += s2
			lit = nil
		}
		if p.Bot {
			score -= 100
		}
		it := item{label: "@" + p.Name, code: "<@" + p.ID + ">", score: score}
		for _, i := range lit {
			it.lit = append(it.lit, i+1)
		}
		if p.Handle != p.Name {
			it.detail = "@" + p.Handle
		}
		out = append(out, it)
	})
	if c != nil && c.Kind == store.IM {
		return out
	}
	for _, s := range [...][2]string{{"here", "everyone active in this channel"}, {"channel", "everyone in this channel"}, {"everyone", "everyone in the workspace"}} {
		if score, lit, ok := match(q, s[0]); ok {
			it := item{label: "@" + s[0], detail: s[1], code: "<!" + s[0] + ">", score: score + 5}
			for _, i := range lit {
				it.lit = append(it.lit, i+1)
			}
			out = append(out, it)
		}
	}
	return m.groups(v, q, out)
}

// channels finds the channels you're in that q means.
func (m *Model) channels(v store.View, q string, out []item) []item {
	for _, id := range m.sideConvs() {
		c := v.Conv(id)
		if c == nil || (c.Kind != store.Channel && c.Kind != store.Private) || c.Archived {
			continue
		}
		score, lit, ok := match(q, c.Name)
		if !ok {
			continue
		}
		it := item{label: "#" + c.Name, detail: firstLine(c.Topic), code: "<#" + c.ID + ">", score: score}
		for _, i := range lit {
			it.lit = append(it.lit, i+1)
		}
		out = append(out, it)
	}
	return out
}

// popKey takes the keys the open popup owns.
func (m *Model) popKey(s string) bool {
	p := &m.pop
	switch s {
	case "up", "ctrl+p":
		p.at = (p.at + len(p.items) - 1) % len(p.items)
	case "down":
		p.at = (p.at + 1) % len(p.items)
	case "tab", "enter":
		m.accept()
	case "esc":
		p.on, p.shut = false, p.from+1
	default:
		return false
	}
	return true
}

// accept puts the chosen item where the query was, and a space after.
func (m *Model) accept() {
	p := &m.pop
	it := p.items[p.at]
	r := []rune(it.label)
	m.splice(p.from, m.cur, r)
	if it.code != "" {
		m.ments = append(m.ments, mention{p.from, p.from + len(r), it.code})
		slices.SortFunc(m.ments, func(a, b mention) int { return a.from - b.from })
	}
	if m.cur < len(m.input) && m.input[m.cur] == ' ' {
		m.cur++
	} else {
		m.splice(m.cur, m.cur, []rune{' '})
	}
	p.on = false
}

// overlayPop draws the popup on the bottom rows of list, just above
// the composer.
func (m *Model) overlayPop(list []canvas.Row, w int) []canvas.Row {
	p := &m.pop
	if !p.on || m.focus != onCompose {
		return list
	}
	n := min(len(p.items), len(list)-2)
	bw := min(w-4, 48)
	if n < 1 || bw < 44 {
		return list
	}
	ink := m.pal.Main
	inner := bw - 4
	edge, fill := m.pal.Orange, ink.Text.Bg(ink.Sel.BG)
	side := canvas.T("│ ", edge.Bg(fill.BG))
	box := []canvas.Row{{canvas.T("╭"+strings.Repeat("─", bw-2)+"╮", edge)}}
	for i, it := range p.items[:n] {
		base := fill
		mark := canvas.T("  ", base)
		if i == p.at {
			base = fill.Bg(ink.Hover.BG)
			mark = canvas.T("▍ ", base.Fg(m.pal.Orange.FG))
		}
		row := canvas.Row{mark}
		for j, r := range []rune(it.label) {
			st := base.Fg(ink.Text.FG)
			if slices.Contains(it.lit, j) {
				st = base.Fg(m.pal.Orange.FG).With(canvas.Bold)
			}
			row = append(row, canvas.T(string(r), st)) // ponytail: a seg a rune; labels are short
		}
		if it.detail != "" {
			row = append(row, canvas.T("  "+it.detail, base.Fg(ink.Dim.FG)))
		}
		box = append(box, append(append(canvas.Row{side}, canvas.Fit(row, inner, base)...), canvas.T(" │", edge.Bg(fill.BG))))
	}
	hint := canvas.Row{canvas.T("╰─ ", edge), canvas.T("↑↓ choose · tab accept · esc dismiss", ink.Dim)}
	hint = append(hint, canvas.T(" "+strings.Repeat("─", max(0, bw-hint.Width()-2))+"╯", edge))
	box = append(box, hint)

	out := slices.Clone(list) // the rows are cached; don't draw on them
	first := len(out) - len(box)
	if first < 0 {
		return list
	}
	for i, r := range box {
		out[first+i] = canvas.Splice(out[first+i], 2, r)
	}
	return out
}
