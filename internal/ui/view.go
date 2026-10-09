package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/obs"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/cellw"
)

func (m *Model) View() tea.View {
	drawn := false
	out := m.gate.Frame(func() string {
		began, last := time.Now(), m.gate.Last()
		m.frame.Reset()
		m.frame.Grow(len(last) + len(last)/8) // one allocation, not a dozen doublings
		canvas.Emit(&m.frame, m.railed(m.render()))
		drawn = true
		obs.Frame(time.Since(began))
		return m.frame.String()
	})
	if !drawn {
		obs.SkippedFrame()
	}
	v := tea.NewView(out)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true // to know when the open conversation is worth a notification
	v.WindowTitle = "loafer"
	return v
}

const headerH = 3 // two rows and a rule

func (m *Model) render() []canvas.Row {
	if m.w < 20 || m.h < 8 {
		return []canvas.Row{{canvas.T("loafer needs a bigger window", m.pal.Main.Dim)}}
	}
	var out []canvas.Row
	m.st.Read(func(v store.View) {
		if m.st.Version() != m.sideSeen {
			m.buildSide(v)
			m.sideSeen = m.st.Version()
		}
		out = append(out, m.header(v)...)
		bodyH := m.h - headerH - 1
		sw, side := m.left(v, bodyH)
		div := make([]canvas.Row, bodyH)
		for i := range div {
			div[i] = canvas.Row{canvas.T("│", m.pal.Main.Faint)}
		}
		out = append(out, canvas.Join(side, div, m.panes(v, m.w-sw-1, bodyH))...)
		out = append(out, m.hints(v))
		if m.bar.on {
			out = m.overlayJump(v, out)
		}
		if m.emo.pick.on {
			out = m.overlayReact(out)
		}
		if m.find.on {
			out = m.overlaySearch(v, out)
		}
		if m.mg.pk.kind != pickNone {
			out = m.overlayPick(v, out)
		}
		if m.ppl.card.on {
			out = m.overlayCard(v, out)
		}
		if m.att.ask.on {
			out = m.overlayAttach(out)
		}
		out = m.overlayBlocks(v, out)
		if m.acts.menu.on {
			out = m.overlayMenu(v, out)
		}
	})
	return out
}

// --- header ---

func (m *Model) header(v store.View) []canvas.Row {
	ink := m.pal.Side
	mentions, unread := 0, 0
	for _, it := range m.side {
		if c := v.Conv(it.conv); c != nil && !c.Muted {
			mentions += c.Mentions
			if c.Unread {
				unread++
			}
		}
	}
	row1 := canvas.Row{canvas.T(" loafer", ink.Bright.With(canvas.Bold|canvas.Italic)), canvas.T("  "+v.Team().Name, ink.Text)}
	if mentions > 0 {
		row1 = append(row1, canvas.T(fmt.Sprintf("   @ %d mentions", mentions), m.pal.SideYellow.With(canvas.Bold)))
	}
	if unread > 0 {
		row1 = append(row1, canvas.T(fmt.Sprintf("   %d unread", unread), ink.Sub))
	}
	state := canvas.T("● live ", m.pal.SideGreen)
	switch m.live {
	case "connecting":
		state = canvas.T("◌ connecting ", ink.Dim)
	case "offline":
		state = canvas.T("◌ offline ", m.pal.SideYellow)
	case "signed out":
		state = canvas.T("✗ signed out · run loafer login ", ink.Text.Fg(m.pal.Red.FG))
	}
	row1 = rightAlign(row1, canvas.Row{state}, m.w, ink.Text)

	row2 := m.tabRow(v)
	rule := canvas.Row{canvas.T(strings.Repeat("─", m.w), m.pal.Main.Faint)}
	return append(m.washed(v.Team(), row1, row2), rule)
}

// rightAlign puts right at the end of left in w cells, cutting left if
// they don't both fit.
func rightAlign(left, right canvas.Row, w int, fill canvas.Style) canvas.Row {
	rw := right.Width()
	if rw >= w {
		return canvas.Fit(right, w, fill)
	}
	return append(canvas.Fit(left, w-rw, fill), right...)
}

// --- sidebar ---

type sideItem struct {
	conv    string // "" for a section heading
	section string
	emoji   string
	id      string // a heading's section
	folded  bool   // a heading's, and how many it hides
	hidden  int
}

// key is what keeps the selection on its item through a rebuild.
func (it sideItem) key() string {
	if it.conv != "" {
		return it.conv
	}
	return "#" + it.id
}

func (m *Model) buildSide(v store.View) {
	sel, was := "", m.sideAt
	if m.sideAt < len(m.side) && m.open != "" { // until something's open, the first conversation
		sel = m.side[m.sideAt].key()
	}
	m.side, m.every = m.side[:0], m.every[:0]
	for _, sec := range v.Sidebar() {
		ids, hidden := m.shown(v, sec)
		m.side = append(m.side, sideItem{section: sec.Name, emoji: sec.Emoji, id: sec.ID, folded: sec.Collapsed, hidden: hidden})
		m.every = append(m.every, sec.Convs...)
		for _, id := range ids {
			m.side = append(m.side, sideItem{conv: id, section: sec.Name})
		}
	}
	// Where it was, else the same place in the list, else the first conversation.
	m.sideAt = slices.IndexFunc(m.side, func(it sideItem) bool { return it.key() == sel })
	if m.sideAt < 0 {
		m.sideAt = max(0, min(was, len(m.side)-1))
		if sel == "" {
			m.sideAt = max(0, slices.IndexFunc(m.side, func(it sideItem) bool { return it.conv != "" }))
		}
	}
}

func (m *Model) sidebar(v store.View, w, h int) []canvas.Row {
	ink := m.pal.Side
	rows := make([]canvas.Row, 0, h)
	// Keep the selection in view, a few rows from either edge.
	if m.sideAt < m.sideTop+2 {
		m.sideTop = max(0, m.sideAt-2)
	}
	if m.sideAt > m.sideTop+h-3 {
		m.sideTop = m.sideAt - h + 3
	}
	for i := m.sideTop; i < len(m.side) && len(rows) < h; i++ {
		it := m.side[i]
		if it.conv == "" {
			if i > 0 {
				rows = append(rows, canvas.Fit(nil, w, ink.Text))
				if len(rows) == h {
					break
				}
			}
			rows = append(rows, m.sideHead(it, w, i == m.sideAt))
			continue
		}
		c := v.Conv(it.conv)
		if c == nil {
			continue
		}
		rows = append(rows, m.sideRow(v, c, w, i == m.sideAt, c.ID == m.open))
	}
	for len(rows) < h {
		rows = append(rows, canvas.Fit(nil, w, ink.Text))
	}
	return rows
}

func (m *Model) sideRow(v store.View, c *store.Conv, w int, selected, open bool) canvas.Row {
	ink := m.pal.Side
	base, name := ink.Text, ink.Sub
	if open {
		base = ink.Sel
		name = name.Bg(base.BG)
	}
	if selected && m.focus == onSide {
		base = ink.Hover
		if open {
			base = ink.Sel
		}
		name = name.Bg(base.BG)
	}
	glyph, glyphFG := "#", ink.Dim.FG
	switch c.Kind {
	case store.Private:
		glyph = "⊡"
	case store.IM:
		glyph, glyphFG = dmMark(v, c, ink, m.pal.SideGreen)
	case store.MPIM:
		glyph = "⁂"
	}
	if c.Unread && !c.Muted {
		name = ink.Bright.Bg(base.BG).With(canvas.Bold)
	}
	if c.Muted {
		name = ink.Faint.Bg(base.BG)
	}
	mark := canvas.T(" ", base)
	if selected {
		mark = canvas.T("▍", base.Fg(m.pal.Orange.FG))
	}
	left := canvas.Row{mark, canvas.T(" "+glyph+" ", name.With(0).Fg(glyphFG)), canvas.T(v.Title(c), name)}
	if c.Kind == store.IM {
		if g := statusGlyph(v, c.User); g != "" {
			left = append(left, canvas.T(" "+g, name.With(0)))
		}
	}
	var right canvas.Row
	if c.Mentions > 0 {
		right = canvas.Row{canvas.T("@"+strconv.Itoa(c.Mentions)+" ", m.pal.SideYellow.Bg(base.BG).With(canvas.Bold))}
	}
	return rightAlign(left, right, w, base)
}

// --- the conversation ---

func (m *Model) main(v store.View, w, h int) []canvas.Row {
	ink := m.pal.Main
	if m.tabs.on == tabClaude {
		return m.claudePane(v, w, h)
	}
	c := v.Conv(m.open)
	if c == nil {
		rows := make([]canvas.Row, h)
		for i := range rows {
			rows[i] = canvas.Fit(nil, w, ink.Text)
		}
		if h > 2 {
			msg := "pick a conversation"
			if m.live == "connecting" && len(m.side) == 0 {
				msg = "loading…"
			}
			rows[h/2] = canvas.Fit(canvas.Row{canvas.T(strings.Repeat(" ", max(0, (w-cellw.String(msg))/2))+msg, ink.Dim)}, w, ink.Text)
		}
		return rows
	}
	title := canvas.Row{canvas.T(" ", m.pal.Panel), canvas.T(convLabel(v, c), m.pal.Panel.Fg(ink.Bright.FG).With(canvas.Bold))}
	if c.Topic != "" {
		title = append(title, canvas.T("   "+firstLine(c.Topic), m.pal.Panel.Fg(ink.Dim.FG)))
	}
	if c.Kind == store.IM {
		title = m.dmTitle(v, c)
	}
	head := []canvas.Row{canvas.Fit(title, w, m.pal.Panel), canvas.Fit(nil, w, ink.Text)}

	box := m.composer(v, c, w, min(6, max(1, h-len(head)-3)))
	if c.Preview {
		box = m.joinBar(c, w)
	}
	if t := m.typingRow(v, w); t != nil {
		box = append(t, box...)
	}
	listH := max(0, h-len(head)-len(box))
	list := m.overlayPop(m.newerPill(v, c, m.messages(v, c, w, listH), w), w)
	return append(append(head, list...), box...)
}

func convLabel(v store.View, c *store.Conv) string {
	switch c.Kind {
	case store.Channel:
		return "# " + c.Name
	case store.Private:
		return "⊡ " + c.Name
	}
	return v.Title(c)
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func day(t, now time.Time) string {
	switch {
	case sameDay(t, now):
		return "today"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "yesterday"
	case t.Year() == now.Year():
		return strings.ToLower(t.Format("Monday, 2 January"))
	}
	return strings.ToLower(t.Format("Monday, 2 January 2006"))
}

// composer is rush's message box (docs/ui.md): a rounded edge, orange
// when it has focus, labels set into it, ❯ and the text inside.
func (m *Model) composer(v store.View, c *store.Conv, w, most int) []canvas.Row {
	ink := m.pal.Main
	edge := ink.Edge
	if m.focus == onCompose {
		edge = m.pal.Orange
	}
	label := func(left, right string, l, r string) canvas.Row {
		row := canvas.Row{canvas.T(l, edge)}
		if left != "" {
			row = append(row, canvas.T(" "+left+" ", ink.Dim))
		}
		tail := canvas.Row{}
		if right != "" {
			tail = append(tail, canvas.T(" "+right+" ", ink.Dim))
		}
		tail = append(tail, canvas.T(r, edge))
		mid := w - row.Width() - tail.Width()
		if mid < 0 {
			return canvas.Fit(canvas.Row{canvas.T(l+strings.Repeat("─", max(0, w-2))+r, edge)}, w, ink.Text)
		}
		return append(append(row, canvas.T(strings.Repeat("─", mid), edge)), tail...)
	}
	top := label("to "+convLabel(v, c), "enter sends · shift+enter new line", "╭", "╮")
	if m.editing != "" {
		top = label("editing your message", "enter saves · esc cancels", "╭", "╮")
	}
	if l, r := m.attachNote(); l != "" {
		top = label(l, r, "╭", "╮")
	}
	bottom := label("", "", "╰", "╯")
	if m.th.in && m.editing == "" {
		top, bottom = label("reply in thread", m.also(v, c), "╭", "╮"), label("ctrl+b also send to channel", "", "╰", "╯")
	}

	inner := max(4, w-6) // "│ ❯ " … " │"
	field := m.pal.Input
	var text canvas.Row
	if len(m.input) == 0 {
		if m.focus == onCompose {
			text = canvas.Row{canvas.T(" ", field.Bg(ink.Text.FG))}
		}
		text = append(text, canvas.T(m.placeholder(v, c), field.Fg(ink.Faint.FG)))
	} else {
		text = m.inputRow(field, field.Fg(m.pal.Blue.FG), field.Bg(ink.Text.FG).Fg(field.BG), m.focus == onCompose)
	}
	var lines []canvas.Row
	for _, l := range splitLines(text) {
		lines = append(lines, canvas.Wrap(l, inner)...)
	}
	if len(m.input) == 0 {
		lines = lines[:1] // the placeholder never wraps
	}
	lines = inView(lines, most, field.Bg(ink.Text.FG).Fg(field.BG))
	out := append([]canvas.Row{top}, m.attachBox(edge, field, inner)...)
	for i, l := range lines {
		lead := "  "
		if i == 0 {
			lead = "❯ "
		}
		row := canvas.Row{canvas.T("│", edge), canvas.T(" ", field), canvas.T(lead, field.Fg(m.pal.Orange.FG).With(canvas.Bold))}
		row = append(row, canvas.Fit(l, inner, field)...)
		out = append(out, append(row, canvas.T(" ", field), canvas.T("│", edge)))
	}
	return append(out, bottom)
}

// splitLines splits a row at its newlines, keeping styles.
func splitLines(r canvas.Row) []canvas.Row {
	out := []canvas.Row{nil}
	for _, s := range r {
		parts := strings.Split(s.Text, "\n")
		for i, p := range parts {
			if i > 0 {
				out = append(out, nil)
			}
			if p != "" {
				out[len(out)-1] = append(out[len(out)-1], canvas.T(p, s.St))
			}
		}
	}
	return out
}

// --- the hint line ---

func (m *Model) hints(v store.View) canvas.Row {
	ink := m.pal.Main
	if m.showDebug {
		d := m.debug
		text := fmt.Sprintf("  heap %.1fM · rss %.0fM · cpu %.1f%% · %d goroutines · %d gc · frame %s avg %s max %s · %d drawn %d skipped · api %d in flight · dropped %d",
			float64(d.Heap)/1e6, float64(d.RSS)/1e6, d.CPU, d.Goroutines, d.GCs,
			d.LastFrame.Round(time.Microsecond), d.AvgFrame.Round(time.Microsecond), d.MaxFrame.Round(time.Microsecond),
			d.Frames, d.Skipped, d.Gauges["api.inflight"], d.Dropped)
		return canvas.Fit(canvas.Row{canvas.T(text, ink.Sub)}, m.w, ink.Text)
	}
	if m.flash != "" {
		st := ink.Text
		if m.flashErr {
			st = m.pal.Red
		}
		return canvas.Fit(canvas.Row{canvas.T("  "+m.flash, st)}, m.w, ink.Text)
	}
	var pairs [][2]string
	switch {
	case m.previewing(v) && m.focus != onSide:
		pairs = m.previewHints()
	case m.focus >= onThread:
		pairs = m.threadHints()
	case m.tabs.on != tabHome && (m.focus == onSide || m.tabs.on == tabClaude):
		pairs = m.tabHints()
	case m.focus == onSide:
		pairs = [][2]string{{"↑↓", "move"}, {"enter", "open"}, {"b N", "browse, new dm"}, {"z m s x", "fold mute move leave"}, {"n", "next unread"}, {"tab", "messages"}, {"q", "quit"}}
	case m.focus == onMsgs && m.sel != "":
		pairs = [][2]string{{"↑↓", "move"}, {".", "actions"}, {"t", "thread"}, {"{}", "by author"}, {"n", "new"}, {"@", "mentions"}, {"e", "edit"}, {"r", "react"}, {"b", "buttons"}, {"a", "ask Claude"}, {"p", "profile"}, {"dd", "delete"}, {"c l", "copy text, link"}, {"o", "open link"}, {"D O", "download, open file"}, {"esc", "newest"}}
	case m.focus == onMsgs:
		pairs = [][2]string{{"↑", "pick a message"}, {"n", "new"}, {"@", "mentions"}, {"g", "oldest"}, {"i", "write"}, {"esc", "sidebar"}}
	case m.editing != "":
		pairs = [][2]string{{"enter", "save"}, {"shift+enter", "new line"}, {"esc", "cancel"}}
	default:
		pairs = [][2]string{{"enter", "send"}, {"ctrl+o", "attach"}, {"↑", "edit last"}, {"alt+↑↓", "channels"}, {"alt+shift+↑↓", "unread"}, {"esc", "messages"}}
	}
	needs := 0
	for _, it := range m.side {
		if c := v.Conv(it.conv); c != nil && needsYou(c) {
			needs++
		}
	}
	var lead canvas.Row
	if needs > 0 {
		lead = canvas.Row{canvas.T(" ctrl+n", m.pal.Yellow.With(canvas.Bold)), canvas.T(fmt.Sprintf(" %d need you", needs), m.pal.Yellow), canvas.T("  ·  ", ink.Faint)}
	}
	// As rush's keysFit: drop pairs from the end, but keep the last
	// (the way out), until the line fits.
	for {
		row := append(canvas.Row{canvas.T(" ", ink.Text)}, lead...)
		for i, p := range pairs {
			if i > 0 {
				row = append(row, canvas.T("  ·  ", ink.Faint))
			}
			row = append(row, canvas.T(" "+p[0], ink.Text.With(canvas.Bold)), canvas.T(" "+p[1], ink.Dim))
		}
		if row.Width() <= m.w || len(pairs) <= 2 {
			return canvas.Fit(row, m.w, ink.Text)
		}
		pairs = append(pairs[:len(pairs)-2], pairs[len(pairs)-1])
	}
}

// picked is a message's rows as the cursor shows it: lifted, with
// rush's orange bar in the gutter. Chips keep their own ground.
func (m *Model) picked(rows []canvas.Row) []canvas.Row {
	ink := m.pal.Main
	out := make([]canvas.Row, len(rows))
	for i, r := range rows {
		nr := make(canvas.Row, len(r))
		for j, s := range r {
			if s.St.BG == ink.Text.BG {
				s.St = s.St.Bg(ink.Sel.BG)
			}
			nr[j] = s
		}
		nr[0] = canvas.T("▍", ink.Sel.Fg(m.pal.Orange.FG))
		out[i] = nr
	}
	return out
}
