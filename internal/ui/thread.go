package ui

import (
	"log/slog"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/rows"
)

// The thread pane (docs/ui.md, Thread open): the parent and its replies
// on the right, with a box of its own, or in the conversation's place
// when there isn't room for both. It's the conversation's list and box
// over again: swap trades the model's list and composer state for the
// thread's, so the cursor, the keys, the drawing and sending all serve
// both, and only what differs (where the messages come from, where a
// message goes) asks which it is.

type threadPane struct {
	conv, ts string // the thread open, by its parent; "" when closed
	in       bool   // its state is swapped into the model's
	also     bool   // also send to the channel
	read     string // the newest reply marked read

	// While swapped out: the thread's list and box. While swapped in: the
	// conversation's.
	newAt, sel, editing string
	scroll, cur, rowsW  int
	follow              bool
	index               rows.Index
	indexOf             indexKey
	rowsNames           uint64
	heights             map[string]int
	input               []rune
	ments               []mention
	pop                 popup
	stash               *draft // what was being written when an edit began
}

type threadMsg struct{ err error }

// swap trades the conversation's list and composer for the thread's, and
// focus with them: in the thread, onMsgs and onCompose mean its own. It's
// its own undoing.
func (m *Model) swap() {
	t := &m.th
	t.in = !t.in
	m.newAt, t.newAt, m.sel, t.sel, m.editing, t.editing = t.newAt, m.newAt, t.sel, m.sel, t.editing, m.editing
	m.scroll, t.scroll, m.cur, t.cur, m.rowsW, t.rowsW = t.scroll, m.scroll, t.cur, m.cur, t.rowsW, m.rowsW
	m.follow, t.follow, m.index, t.index, m.indexOf, t.indexOf = t.follow, m.follow, t.index, m.index, t.indexOf, m.indexOf
	m.rowsNames, t.rowsNames, m.heights, t.heights = t.rowsNames, m.rowsNames, t.heights, m.heights
	m.input, t.input, m.ments, t.ments, m.pop, t.pop = t.input, m.input, t.ments, m.ments, t.pop, m.pop
	// An edit keeps what was being written in drafts[open]; each has its own.
	d, had := m.drafts[m.open]
	if t.stash != nil {
		m.drafts[m.open] = *t.stash
	} else {
		delete(m.drafts, m.open)
	}
	t.stash = nil
	if had {
		t.stash = &d
	}
	switch m.focus {
	case onMsgs:
		m.focus = onThread
	case onThread:
		m.focus = onMsgs
	case onCompose:
		m.focus = onReply
	case onReply:
		m.focus = onCompose
	}
}

// inThread runs f with the thread swapped in.
func (m *Model) inThread(f func()) {
	m.swap()
	f()
	m.swap()
}

// window is what the list and the cursor are over: the open conversation,
// or the thread while it's swapped in.
func (m *Model) window(v store.View) *store.Window {
	if m.th.in {
		return v.Thread(m.th.conv, m.th.ts)
	}
	return v.Window(m.open)
}

// narrow says the thread takes the conversation's place.
func (m *Model) narrow() bool { return m.w < 100 }

// threadAt opens the selected message's thread: t on any message, to
// start one; enter and → only where there is one, enter writing otherwise.
func (m *Model) threadAt(s string) tea.Cmd {
	msg, ok := m.selected()
	has := ok && (msg.ReplyCount > 0 || msg.ThreadTS != "")
	switch {
	case m.th.in || (!has && s == "enter"):
		if s == "enter" {
			m.setFocus(onCompose)
		}
		return nil
	case !ok:
		return m.say("pick a message first (↑)", false)
	case !has && s == "right":
		return nil
	}
	ts := msg.TS
	if msg.ThreadTS != "" {
		ts = msg.ThreadTS
	}
	return m.openThread(ts)
}

// openThread opens the thread at ts in the open conversation, in its box.
func (m *Model) openThread(ts string) tea.Cmd {
	if m.th.ts == ts && m.th.conv == m.open {
		m.setFocus(onReply)
		return nil
	}
	m.dropThread()
	k := m.open + "/" + ts
	d := m.drafts[k]
	delete(m.drafts, k)
	m.th = threadPane{conv: m.open, ts: ts, heights: map[string]int{}, input: d.text, ments: d.ments, cur: len(d.text)}
	m.setFocus(onReply)
	conv := m.open
	return func() tea.Msg { return threadMsg{m.st.OpenThread(m.ctx, m.api, conv, ts)} }
}

// openReply opens the thread at ts with the cursor on its reply.
func (m *Model) openReply(ts, reply string) tea.Cmd {
	cmd := m.openThread(ts)
	m.th.sel, m.th.follow = reply, true
	m.focus = onThread
	return cmd
}

// dropThread closes the pane, keeping what was being written in it, and
// moves focus to the conversation's like.
func (m *Model) dropThread() {
	t := &m.th
	if t.ts == "" {
		return
	}
	if t.editing != "" {
		m.inThread(m.cancelEdit)
	}
	if len(t.input) > 0 {
		m.drafts[t.conv+"/"+t.ts] = draft{t.input, t.ments}
	}
	t.ts, t.conv = "", ""
	switch m.focus {
	case onThread:
		m.focus = onMsgs
	case onReply:
		m.focus = onCompose
	}
}

// closeThread closes the pane, with the cursor back on its parent.
func (m *Model) closeThread() tea.Cmd {
	ts := m.th.ts
	m.dropThread()
	held := false
	m.st.Read(func(v store.View) { held = v.Holds(m.open, ts) })
	if held {
		m.sel, m.follow = ts, true
	}
	return nil
}

// threadFocus starts the thread's cursor on its newest message when focus
// comes to it, and lets it go when focus leaves.
func (m *Model) threadFocus(f focus) {
	if m.th.in {
		return
	}
	if f == onThread && m.focus != onThread && m.th.sel == "" {
		m.inThread(func() { m.pick(by(-1)) })
	}
	if f != onThread {
		m.th.sel = ""
	}
}

// next is the focus d steps round from here: the sidebar, the
// conversation and its box, then the thread and its box when it's open
// (in the conversation's place, when it's narrow).
func (m *Model) next(d int) focus {
	stops := []focus{onSide, onMsgs, onCompose}
	switch {
	case m.th.ts == "":
	case m.narrow():
		stops = []focus{onSide, onThread, onReply}
	default:
		stops = append(stops, onThread, onReply)
	}
	i := max(0, slices.Index(stops, m.focus))
	n := len(stops)
	return stops[((i+d)%n+n)%n]
}

// threadKey is a key in the pane: esc closes it, ctrl+b ticks "also send
// to the channel", and the rest are the conversation's keys over the
// thread.
func (m *Model) threadKey(k tea.KeyPressMsg, s string) tea.Cmd {
	switch {
	case m.focus == onThread && s == "esc":
		return m.closeThread()
	case m.focus == onReply && s == "ctrl+b":
		m.th.also = !m.th.also
		return nil
	}
	var cmd tea.Cmd
	m.inThread(func() {
		if m.focus == onMsgs {
			cmd = m.msgsKey(s)
		} else if !(m.pop.on && m.popKey(s)) {
			cmd = m.composeKey(k, s)
			m.refreshPop()
		}
	})
	if m.focus == onSide && m.tabs.on != tabClaude { // ← from the thread is the conversation
		m.setFocus(onMsgs)
	}
	return cmd
}

// reply sends text to the open thread, and to the channel if it's ticked.
func (m *Model) reply(conv, text string) tea.Cmd {
	ts, also := m.th.ts, m.th.also
	m.th.also = false
	return func() tea.Msg {
		msg, err := m.api.Reply(m.ctx, conv, ts, text, also)
		if err == nil {
			m.st.Add(conv, msg) // before the websocket's copy, if it's slow
		}
		return sentMsg{err}
	}
}

// markThread moves the open thread's read marker to its newest reply.
func (m *Model) markThread() tea.Cmd {
	t := &m.th
	if t.ts == "" {
		return nil
	}
	ts := ""
	m.st.Read(func(v store.View) {
		if w := v.Thread(t.conv, t.ts); w != nil && len(w.Msgs) > 1 {
			ts = w.Msgs[len(w.Msgs)-1].TS
		}
	})
	if ts <= t.read {
		return nil
	}
	t.read = ts
	conv, thread := t.conv, t.ts
	return func() tea.Msg {
		if err := m.api.MarkThread(m.ctx, conv, thread, ts); err != nil {
			slog.Warn("mark thread", "conv", conv, "err", err)
		}
		return nil
	}
}

// threaded takes the thread's fetch.
func (m *Model) threaded(msg threadMsg) tea.Cmd {
	if msg.err != nil {
		return m.say("couldn't open the thread: "+msg.err.Error(), true)
	}
	m.th.follow = m.th.sel != "" // a reply searched for is there now
	return m.markThread()
}

// --- drawing ---

// panes is the conversation, and the thread beside it when it's open.
func (m *Model) panes(v store.View, w, h int) []canvas.Row {
	switch {
	case m.th.ts == "" || m.tabs.on == tabClaude:
		return m.main(v, w, h)
	case m.narrow():
		return m.thread(v, w, h)
	}
	tw := min(64, max(30, m.w*28/100))
	div := make([]canvas.Row, h)
	for i := range div {
		div[i] = canvas.Row{canvas.T("│", m.pal.Main.Faint)}
	}
	return canvas.Join(m.main(v, w-tw-1, h), div, m.thread(v, tw, h))
}

// thread draws the pane as main draws the conversation: a title, the
// messages, the box.
func (m *Model) thread(v store.View, w, h int) []canvas.Row {
	ink, panel := m.pal.Main, m.pal.Panel
	c := v.Conv(m.th.conv)
	if c == nil {
		out := make([]canvas.Row, h)
		for i := range out {
			out[i] = canvas.Fit(nil, w, ink.Text)
		}
		return out
	}
	title := canvas.Row{canvas.T(" ", panel), canvas.T("thread", panel.Fg(ink.Bright.FG).With(canvas.Bold)), canvas.T("  "+convLabel(v, c), panel.Fg(ink.Dim.FG))}
	head := []canvas.Row{rightAlign(title, canvas.Row{canvas.T("esc close ", panel.Fg(ink.Dim.FG))}, w, panel), canvas.Fit(nil, w, ink.Text)}
	var out []canvas.Row
	m.inThread(func() {
		box := m.composer(v, c, w, min(6, max(1, h-len(head)-3)))
		list := m.overlayPop(m.messages(v, c, w, max(0, h-len(head)-len(box))), w)
		out = append(append(head, list...), box...)
	})
	return out
}

// placeholder is what an empty box says.
func (m *Model) placeholder(v store.View, c *store.Conv) string {
	if m.th.in {
		return "reply…"
	}
	return "a message for " + convLabel(v, c)
}

// also is the thread box's "also send to the channel" tick.
func (m *Model) also(v store.View, c *store.Conv) string {
	if m.th.also {
		return "☑ also " + convLabel(v, c)
	}
	return "☐ also " + convLabel(v, c)
}

// threadHints are the hint line's keys in the pane.
func (m *Model) threadHints() [][2]string {
	switch {
	case m.focus == onThread:
		return [][2]string{{"↑↓", "move"}, {".", "actions"}, {"e", "edit"}, {"r", "react"}, {"c l", "copy text, link"}, {"i", "reply"}, {"esc", "close"}}
	case m.th.editing != "":
		return [][2]string{{"enter", "save"}, {"shift+enter", "new line"}, {"esc", "cancel"}}
	}
	return [][2]string{{"enter", "reply"}, {"ctrl+b", "also to channel"}, {"shift+enter", "new line"}, {"esc", "messages"}}
}
