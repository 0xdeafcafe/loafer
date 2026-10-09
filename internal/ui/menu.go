package ui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
)

// openMenu opens the actions menu on msg. Whether it's on the Later list
// is only known once that's fetched, so the first time, it is.
func (m *Model) openMenu(msg slack.Message) tea.Cmd {
	mn := menu{on: true, thread: m.th.in, conv: m.open, msg: msg}
	var loaded bool
	m.st.Read(func(v store.View) {
		mn.link = len(webLinks(m.asDrawn(v, &msg))) > 0
		loaded = v.Loaded(store.LaterList)
	})
	m.acts.menu = mn
	if loaded {
		return nil
	}
	return func() tea.Msg {
		_ = m.st.Fetch(m.ctx, m.api, store.LaterList)
		return nil
	}
}

// menuList is what the menu offers: the actions on its message, or when
// to be reminded.
func (m *Model) menuList(v store.View) []menuItem {
	mn := &m.acts.menu
	if mn.remind {
		now := time.Now()
		out := make([]menuItem, len(remindChoices))
		for i, c := range remindChoices {
			out[i] = menuItem{key: strconv.Itoa(i + 1), label: c.label, tail: clock(c.at(now), now)}
		}
		return out
	}
	msg := &mn.msg
	var out []menuItem
	add := func(key, glyph, label string) { out = append(out, menuItem{key: key, glyph: glyph, label: label}) }
	add("r", "☺", "react")
	if !mn.thread {
		add("t", "↩", "reply in thread")
	}
	add("a", "◇", "ask Claude")
	if v.IsSaved(mn.conv, msg.TS) {
		add("s", "◆", "remove from later")
	} else {
		add("s", "◆", "save for later")
	}
	add("m", "◷", "remind me ▸")
	if slices.Contains(msg.PinnedTo, mn.conv) {
		add("p", "⚑", "unpin")
	} else {
		add("p", "⚑", "pin")
	}
	if !mn.thread {
		add("u", "●", "mark unread from here")
	}
	add("l", "⧉", "copy link")
	add("c", "⧉", "copy text")
	if mn.link {
		add("o", "↗", "open link")
	}
	if msg.User == v.Self() && msg.Subtype == "" {
		add("e", "✎", "edit")
		out = append(out, menuItem{key: "d", glyph: "✗", label: "delete", danger: true})
	}
	return out
}

// menuKey is a key while the menu is open: ↑↓ and enter, or an item's own
// key. esc closes it.
func (m *Model) menuKey(k tea.KeyPressMsg) tea.Cmd {
	mn := &m.acts.menu
	var items []menuItem
	m.st.Read(func(v store.View) { items = m.menuList(v) })
	s := k.String()
	switch s {
	case "esc", ".":
		mn.on = false
	case "up", "k", "ctrl+p":
		mn.at = (mn.at + len(items) - 1) % max(1, len(items))
	case "down", "j", "ctrl+n":
		mn.at = (mn.at + 1) % max(1, len(items))
	case "enter":
		if mn.at < len(items) {
			return m.menuDo(items[mn.at].key)
		}
	default:
		if slices.ContainsFunc(items, func(it menuItem) bool { return it.key == s }) {
			return m.menuDo(s)
		}
	}
	return nil
}

// menuDo closes the menu and does key. From the chooser, key is which of
// the times; from the list, an action, done with the thread swapped in if
// the menu was opened over it.
func (m *Model) menuDo(key string) tea.Cmd {
	mn := m.acts.menu
	m.acts.menu.on = false
	if mn.remind {
		i, _ := strconv.Atoi(key)
		return m.remindAt(mn.conv, mn.msg, i-1)
	}
	var cmd tea.Cmd
	if mn.thread {
		m.inThread(func() { cmd = m.do(key, mn.msg) })
	} else {
		cmd = m.do(key, mn.msg)
	}
	return cmd
}

// overlayMenu draws the menu over the frame, which goes faint behind it,
// on the right where the messages are.
func (m *Model) overlayMenu(v store.View, frame []canvas.Row) []canvas.Row {
	ink, mn := m.pal.Main, &m.acts.menu
	bw := min(m.w-4, 36)
	if bw < 24 {
		return frame
	}
	inner := bw - 4
	items := m.menuList(v)
	edge, fill := m.pal.Orange, ink.Text.Bg(ink.Sel.BG)
	mn.at = min(mn.at, max(0, len(items)-1))

	line := func(r canvas.Row) canvas.Row {
		return append(append(canvas.Row{canvas.T("│ ", edge.Bg(fill.BG))}, canvas.Fit(r, inner, fill)...), canvas.T(" │", edge.Bg(fill.BG)))
	}
	edgeRow := func(l, label, r string) canvas.Row {
		row := canvas.Row{canvas.T(l+"─ ", edge), canvas.T(label, ink.Sub.With(canvas.Bold))}
		return append(row, canvas.T(" "+strings.Repeat("─", max(0, bw-row.Width()-2))+r, edge))
	}
	title, foot := "Actions", "↑↓ · enter · esc"
	if mn.remind {
		title, foot = "◷ Remind me", "1-5 · esc"
	}
	box := []canvas.Row{edgeRow("╭", title, "╮")}
	listH := max(1, min(len(items), m.h-headerH-6))
	from := max(0, mn.at-listH+1)
	for i := from; i < min(len(items), from+listH); i++ {
		box = append(box, line(m.menuRow(items[i], i == mn.at, inner, fill)))
	}
	box = append(box, edgeRow("╰", foot, "╯"))

	out := make([]canvas.Row, len(frame))
	for i, r := range frame {
		out[i] = faint(r, ink.Faint.FG)
	}
	top, left := min(headerH+2, max(0, len(out)-1-len(box))), m.w-bw-2
	for i, r := range box {
		if top+i < len(out)-1 {
			out[top+i] = canvas.Splice(out[top+i], left, r)
		}
	}
	return out
}

func (m *Model) menuRow(it menuItem, sel bool, w int, fill canvas.Style) canvas.Row {
	ink := m.pal.Main
	base, mark := fill, canvas.T("  ", fill)
	if sel {
		base = fill.Bg(ink.Hover.BG)
		mark = canvas.T("▍ ", base.Fg(m.pal.Orange.FG))
	}
	text := base.Fg(ink.Text.FG)
	glyph := base.Fg(ink.Dim.FG)
	if it.danger {
		text, glyph = base.Fg(m.pal.Red.FG), base.Fg(m.pal.Red.FG)
	}
	left := canvas.Row{mark}
	if it.glyph != "" {
		left = append(left, canvas.T(it.glyph+" ", glyph))
	}
	left = append(left, canvas.T(it.label, text))
	right := canvas.Row{canvas.T(it.key+" ", base.Fg(ink.Dim.FG).With(canvas.Bold))}
	if it.tail != "" {
		right = canvas.Row{canvas.T(it.tail+"  ", base.Fg(ink.Dim.FG)), canvas.T(it.key+" ", base.Fg(ink.Text.FG).With(canvas.Bold))}
	}
	return rightAlign(left, right, w, base)
}
