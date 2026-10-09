package ui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/termimg"
	"github.com/0xdeafcafe/photon/theme"
)

// ctrl+k is rush's command bar as a conversation switcher, "go
// anywhere": with nothing typed it offers what needs you, where you've
// just been and what's unread; typing narrows every conversation by
// fuzzy match, rush's scorer, nudged toward the unread and the recent.
// `>` first lists every action instead (actions.go's allActions), and
// `#` first browses the channels you're not in.

type jumper struct {
	on    bool
	query []rune
	at    int
	items []jumpItem
	mn    menu   // the current message, for `>`'s message actions
	msgOK bool   // there is one
	conv  string // the current conversation, for `>`'s conversation actions
}

type jumpItem struct {
	conv, section string
	lit           []int // runes of the title the query matched
	score         int
	ws            *Model  // a workspace to switch to, instead of a conversation (workspaces.go)
	act           *action // an action to do, instead (`>`)
	label         string  // the action's, as things stand
	off           bool    // it has no message or conversation to act on
}

const jumpCap = 50

var jumpGlyph = map[store.Kind]string{store.Channel: "# ", store.Private: "⊡ ", store.IM: "● ", store.MPIM: "⁂ "}

func (m *Model) openJump() {
	msg, ok, thread := m.currentMsg()
	m.bar = jumper{on: true, mn: menu{thread: thread, conv: m.open, msg: msg}, msgOK: ok, conv: m.currentConv()}
	m.st.Read(func(v store.View) {
		if ok {
			m.bar.mn.link = len(webLinks(m.asDrawn(v, &msg))) > 0
		}
		m.buildJump(v)
	})
}

// buildActions lists the actions the query after `>` matches: with
// nothing more typed, the message's, the conversation's, then the rest.
// Those with nothing to act on are faint.
func (m *Model) buildActions(v store.View) {
	j := &m.bar
	q := strings.TrimSpace(string(j.query[1:]))
	sections := [...]string{onMessage: "Message", onConv: "Conversation", onSidebar: "Everywhere", anywhere: "Everywhere"}
	conv := &menu{conv: j.conv}
	for _, s := range []scope{onMessage, onConv, onSidebar, anywhere} {
		mn := &j.mn
		if s == onConv {
			mn = conv
		}
		for i := range allActions {
			a := &allActions[i]
			if a.on != s {
				continue
			}
			it := jumpItem{act: a, label: a.label, section: sections[s], off: (s == onMessage && !j.msgOK) || (s == onConv && j.conv == "")}
			if a.when != nil {
				if it.label = a.when(m, v, mn); it.label == "" {
					continue
				}
			}
			if q != "" {
				score, lit, ok := match(q, it.label)
				if !ok {
					continue
				}
				it.lit, it.score = lit, score
				if it.off {
					it.score -= 1000 // what can't be done goes last
				}
			}
			j.items = append(j.items, it)
		}
	}
	if q != "" {
		slices.SortStableFunc(j.items, func(a, b jumpItem) int { return b.score - a.score })
	}
}

// buildJump lists what the query finds.
func (m *Model) buildJump(v store.View) {
	j := &m.bar
	j.items, j.at = j.items[:0], 0
	if len(j.query) > 0 && j.query[0] == '>' {
		m.buildActions(v)
		return
	}
	recent := m.recent()
	if len(j.query) == 0 {
		seen := map[string]bool{m.open: true}
		add := func(section string, ok func(*store.Conv) bool, ids []string) {
			for _, id := range ids {
				if c := v.Conv(id); c != nil && !seen[id] && ok(c) && len(j.items) < jumpCap {
					seen[id] = true
					j.items = append(j.items, jumpItem{conv: id, section: section})
				}
			}
		}
		side := m.sideConvs()
		add("Needs you", needsYou, side)
		add("Recent", anyConv, recent)
		add("Unread", unreadConv, side)
		add("Conversations", anyConv, side)
		j.items = append(j.items, m.jumpWorkspaces("")...)
		return
	}
	q := string(j.query)
	for _, id := range m.sideConvs() {
		c := v.Conv(id)
		if c == nil || id == m.open {
			continue
		}
		title := jumpTitle(v, c)
		score, lit, ok := match(q, title)
		if !ok {
			continue
		}
		switch t := strings.ToLower(title); { // the name typed out beats any fuzzy hit
		case t == strings.ToLower(q):
			score += 1000
		case strings.HasPrefix(t, strings.ToLower(q)):
			score += 500
		}
		switch {
		case needsYou(c):
			score += 15
		case unreadConv(c):
			score += 8
		}
		if i := slices.Index(recent, id); i >= 0 {
			score += 12 - min(i, 10)
		}
		j.items = append(j.items, jumpItem{conv: id, section: "Conversations", lit: lit, score: score})
	}
	j.items = append(j.items, m.jumpWorkspaces(q)...)
	slices.SortStableFunc(j.items, func(a, b jumpItem) int { return b.score - a.score })
	j.items = j.items[:min(len(j.items), jumpCap)]
}

// recent is where you've been, latest first, each once.
func (m *Model) recent() []string {
	var out []string
	for i := len(m.back) - 1; i >= 0; i-- {
		if !slices.Contains(out, m.back[i]) {
			out = append(out, m.back[i])
		}
	}
	return out
}

// sideConvs is every conversation in the sidebar, those folded out of
// sight too. It's rebuilt with the sidebar; don't keep or change it.
func (m *Model) sideConvs() []string { return m.every }

// jumpTitle is what the query is matched against: the name, without
// convLabel's glyph.
func jumpTitle(v store.View, c *store.Conv) string {
	if c.Kind == store.Channel || c.Kind == store.Private {
		return c.Name
	}
	return v.Title(c)
}

func (m *Model) jumpKey(k tea.KeyPressMsg) tea.Cmd {
	j := &m.bar
	switch k.String() {
	case "esc", "ctrl+c", "ctrl+k":
		j.on = false
	case "ctrl+f":
		j.on = false
		return m.searchFor(j.query)
	case "enter":
		j.on = false
		if j.at < len(j.items) {
			if a := j.items[j.at].act; a != nil {
				return m.runAction(*a)
			}
			if to := j.items[j.at].ws; to != nil {
				return func() tea.Msg { return switchMsg{to} }
			}
			m.setFocus(onCompose)
			return m.visit(j.items[j.at].conv)
		}
	case "up", "ctrl+p":
		if n := len(j.items); n > 0 {
			j.at = (j.at + n - 1) % n
		}
	case "down", "ctrl+n":
		if n := len(j.items); n > 0 {
			j.at = (j.at + 1) % n
		}
	case "backspace":
		if len(j.query) > 0 {
			j.query = j.query[:len(j.query)-1]
			m.st.Read(m.buildJump)
		}
	case "ctrl+u", "ctrl+w":
		j.query = j.query[:0]
		m.st.Read(m.buildJump)
	default:
		if k.Text == "#" && len(j.query) == 0 { // # browses the channels you're not in
			j.on = false
			return m.openBrowse()
		}
		if k.Text != "" {
			j.query = append(j.query, []rune(k.Text)...)
			m.st.Read(m.buildJump)
		}
	}
	return nil
}

// --- drawing ---

// overlayJump draws the switcher over the frame, which goes faint
// behind it, as rush's bar does.
func (m *Model) overlayJump(v store.View, frame []canvas.Row) []canvas.Row {
	ink := m.pal.Main
	bw := min(m.w-4, 72)
	if bw < 24 {
		return frame
	}
	inner := bw - 4
	top := min(m.h/8, 4)
	listH := max(1, min(len(m.bar.items)+3, m.h*2/3-4))
	edge, fill := m.pal.Orange, ink.Text.Bg(ink.Sel.BG)

	line := func(r canvas.Row) canvas.Row {
		return append(append(canvas.Row{canvas.T("│ ", edge.Bg(fill.BG))}, canvas.Fit(r, inner, fill)...), canvas.T(" │", edge.Bg(fill.BG)))
	}
	edgeRow := func(l, label, r string) canvas.Row {
		row := canvas.Row{canvas.T(l+"─ ", edge), canvas.T(label, ink.Sub.With(canvas.Bold))}
		return append(row, canvas.T(" "+strings.Repeat("─", max(0, bw-row.Width()-2))+r, edge))
	}
	acting := len(m.bar.query) > 0 && m.bar.query[0] == '>'
	title, foot := "⌕ Jump", "↑↓ · enter go · > actions · # browse · ctrl+f messages · esc close"
	if acting {
		title, foot = "▸ Actions", "↑↓ choose · enter do · esc close"
	}
	box := []canvas.Row{edgeRow("╭", title, "╮")}
	input := canvas.Row{canvas.T("❯ ", fill.Fg(m.pal.Orange.FG).With(canvas.Bold)), canvas.T(string(m.bar.query), fill.Fg(ink.Bright.FG)), canvas.T("▏", fill.Fg(m.pal.Orange.FG))}
	if len(m.bar.query) == 0 {
		input = append(input, canvas.T("type > for actions, # to browse channels", fill.Fg(ink.Dim.FG)))
	}
	box = append(box, line(input))
	box = append(box, line(canvas.Row{canvas.T(strings.Repeat("─", inner), fill.Fg(ink.Faint.FG))}))

	// The rows that fit, scrolled to keep the selection in view, with a
	// heading wherever the section changes.
	var rows []canvas.Row
	at := -1
	section := ""
	for i, it := range m.bar.items {
		if it.section != section && (len(m.bar.query) == 0 || string(m.bar.query) == ">") {
			section = it.section
			head := canvas.Row{canvas.T("▾ "+section+" ", fill.Fg(ink.Sub.FG).With(canvas.Bold))}
			rows = append(rows, append(head, canvas.T(strings.Repeat("─", max(0, inner-head.Width())), fill.Fg(ink.Faint.FG))))
		}
		if i == m.bar.at {
			at = len(rows)
		}
		rows = append(rows, m.jumpRow(v, it, inner, i == m.bar.at, fill))
	}
	if len(rows) == 0 {
		rows = append(rows, canvas.Row{canvas.T("  nothing matches", fill.Fg(ink.Dim.FG))})
	}
	from := 0
	if at >= listH {
		from = at - listH + 1
	}
	for _, r := range rows[from:min(len(rows), from+listH)] {
		box = append(box, line(r))
	}
	box = append(box, edgeRow("╰", foot, "╯"))

	out := make([]canvas.Row, len(frame))
	for i, r := range frame {
		out[i] = faint(r, ink.Faint.FG)
	}
	left := (m.w - bw) / 2
	for i, r := range box {
		if top+i < len(out)-1 {
			out[top+i] = canvas.Splice(out[top+i], left, r)
		}
	}
	return out
}

func (m *Model) jumpRow(v store.View, it jumpItem, w int, sel bool, fill canvas.Style) canvas.Row {
	if it.ws != nil {
		return m.jumpWsRow(it, w, sel, fill)
	}
	if a := it.act; a != nil {
		row := m.menuRow(menuItem{key: a.key, glyph: a.glyph, label: it.label, danger: a.danger}, it.lit, sel, w, fill)
		if it.off {
			row = faint(row, m.pal.Main.Faint.FG)
		}
		return row
	}
	ink := m.pal.Main
	c := v.Conv(it.conv)
	if c == nil {
		return nil
	}
	base := fill
	if sel {
		base = fill.Bg(ink.Hover.BG)
	}
	mark := canvas.T("  ", base)
	if sel {
		mark = canvas.T("▍ ", base.Fg(m.pal.Orange.FG))
	}
	glyph := jumpGlyph[c.Kind]
	row := canvas.Row{mark, canvas.T(glyph, base.Fg(ink.Dim.FG))}
	name := base.Fg(ink.Text.FG)
	if c.Unread {
		name = base.Fg(ink.Bright.FG).With(canvas.Bold)
	}
	lit := base.Fg(m.pal.Orange.FG).With(canvas.Bold)
	for i, r := range []rune(jumpTitle(v, c)) {
		st := name
		if slices.Contains(it.lit, i) {
			st = lit
		}
		row = append(row, canvas.T(string(r), st)) // ponytail: a seg a rune; titles are short
	}
	var tail canvas.Row
	if c.Mentions > 0 {
		tail = canvas.Row{canvas.T("@"+strconv.Itoa(c.Mentions)+" ", base.Fg(m.pal.Yellow.FG).With(canvas.Bold))}
	} else if c.Topic != "" && c.Kind <= store.Private {
		tail = canvas.Row{canvas.T(firstLine(c.Topic)+" ", base.Fg(ink.Dim.FG))}
	}
	tail = canvas.Cut(tail, max(0, w-row.Width()-2))
	return rightAlign(row, tail, w, base)
}

// faint is r with its text dimmed to fg, keeping its ground.
func faint(r canvas.Row, fg theme.RGB) canvas.Row {
	out := make(canvas.Row, len(r))
	for i, s := range r {
		s.St = s.St.Fg(fg)
		s.St.A &^= canvas.Bold
		s.St.Link = ""
		s.Text = termimg.Blank(s.Text) // a picture's id is its colour, gone here
		out[i] = s
	}
	return out
}
