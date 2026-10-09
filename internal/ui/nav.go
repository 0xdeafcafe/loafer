package ui

import (
	"log/slog"
	"net/url"
	"os/exec"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/mrkdwn"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
)

// Where the keys take you (docs/ui.md, Keys): conversations by sidebar
// order, by unread and by what needs you; back and forward through those
// visited; and a cursor over the open conversation's messages, with
// jumps by author, to what's new and to mentions of you.

type olderMsg struct{ err error }

// visit opens id as a new step in the history.
func (m *Model) visit(id string) tea.Cmd {
	if id == "" || id == m.open {
		return nil
	}
	if m.open != "" {
		m.back = append(m.back, m.open)
		m.fwd = m.fwd[:0]
	}
	return m.openConv(id)
}

func (m *Model) goBack() tea.Cmd {
	if len(m.back) == 0 {
		return m.say("nothing to go back to", false)
	}
	id := m.back[len(m.back)-1]
	m.back = m.back[:len(m.back)-1]
	m.fwd = append(m.fwd, m.open)
	return m.openConv(id)
}

func (m *Model) goForward() tea.Cmd {
	if len(m.fwd) == 0 {
		return nil
	}
	id := m.fwd[len(m.fwd)-1]
	m.fwd = m.fwd[:len(m.fwd)-1]
	m.back = append(m.back, m.open)
	return m.openConv(id)
}

// step finds the next conversation after the open one in the sidebar,
// going d (±1) and wrapping, that ok accepts; "" if none does.
func (m *Model) step(d int, ok func(*store.Conv) bool) string {
	n := len(m.side)
	at := m.sideAt
	if i := slices.IndexFunc(m.side, func(it sideItem) bool { return it.conv == m.open && it.conv != "" }); i >= 0 {
		at = i
	}
	found := ""
	m.st.Read(func(v store.View) {
		for k := 1; k <= n; k++ {
			it := m.side[((at+d*k)%n+n)%n]
			if it.conv == "" || it.conv == m.open {
				continue
			}
			if c := v.Conv(it.conv); c != nil && ok(c) {
				found = it.conv
				return
			}
		}
	})
	return found
}

func anyConv(*store.Conv) bool      { return true }
func unreadConv(c *store.Conv) bool { return c.Unread || c.Mentions > 0 }

// needsYou is a mention, or anything new in a DM.
func needsYou(c *store.Conv) bool {
	return c.Mentions > 0 || (c.Unread && (c.Kind == store.IM || c.Kind == store.MPIM))
}

func (m *Model) jump(d int, ok func(*store.Conv) bool, none string) tea.Cmd {
	if id := m.step(d, ok); id != "" {
		return m.visit(id)
	}
	return m.say(none, false)
}

// --- the message cursor ---

// msgIndex is where ts is in msgs (oldest first); len(msgs) for "",
// which is past the newest.
func msgIndex(msgs []slack.Message, ts string) int {
	if ts == "" {
		return len(msgs)
	}
	i, _ := slices.BinarySearchFunc(msgs, ts, func(m slack.Message, ts string) int { return strings.Compare(m.TS, ts) })
	return i
}

// pick moves the cursor to the message f chooses, given the open
// window's messages and the cursor's index in them; f gives -1 to leave
// it, len(msgs) to go past the newest. Reaching the oldest fetches older.
func (m *Model) pick(f func(msgs []slack.Message, at int) int) tea.Cmd {
	var cmd tea.Cmd
	m.st.Read(func(v store.View) {
		w := m.window(v)
		if w == nil {
			return
		}
		i := f(w.Msgs, msgIndex(w.Msgs, m.sel))
		switch {
		case i < 0:
			return
		case i >= len(w.Msgs):
			m.sel, m.scroll = "", 0
			if w.Newer && !m.fetching { // back from where a search went
				m.fetching = true
				conv := m.open
				cmd = func() tea.Msg { return olderMsg{m.st.Newest(m.ctx, m.api, conv)} }
			}
		default:
			m.sel = w.Msgs[i].TS
			if i == 0 && w.More && !m.fetching {
				m.fetching = true
				conv := m.open
				cmd = func() tea.Msg { return olderMsg{m.st.Older(m.ctx, m.api, conv)} }
			}
		}
		m.follow = true
	})
	return cmd
}

func by(d int) func([]slack.Message, int) int {
	return func(ms []slack.Message, at int) int { return min(len(ms), max(0, at+d)) }
}

func oldest([]slack.Message, int) int      { return 0 }
func newest(ms []slack.Message, _ int) int { return len(ms) }

// prevGroup is the first message of the run the cursor is in, or of the
// run before when it's already first; nextGroup the next run's first.
func prevGroup(ms []slack.Message, at int) int {
	i := min(at, len(ms)) - 1
	for i > 0 && followsOn(&ms[i-1], &ms[i]) {
		i--
	}
	return max(0, i)
}

func nextGroup(ms []slack.Message, at int) int {
	i := at + 1
	for i < len(ms) && followsOn(&ms[i-1], &ms[i]) {
		i++
	}
	return min(i, len(ms))
}

// mentions says whether msg calls on self.
func mentions(msg *slack.Message, self string) bool {
	t := msg.Text
	return strings.Contains(t, "<@"+self+">") || strings.Contains(t, "<@"+self+"|") ||
		strings.Contains(t, "<!here") || strings.Contains(t, "<!channel") || strings.Contains(t, "<!everyone")
}

// toMention goes to the mention of self before the cursor, wrapping to
// the newest.
func (m *Model) toMention() tea.Cmd {
	self, found := "", false
	m.st.Read(func(v store.View) { self = v.Self() })
	cmd := m.pick(func(ms []slack.Message, at int) int {
		for k := 1; k <= len(ms); k++ {
			i := ((at-k)%len(ms) + len(ms)) % len(ms)
			if ms[i].User != self && mentions(&ms[i], self) {
				found = true
				return i
			}
		}
		return -1
	})
	if !found {
		return m.say("nobody's mentioned you in here", false)
	}
	return cmd
}

// toNew goes to the first message after where you'd read to on opening.
func (m *Model) toNew() tea.Cmd {
	self, found := "", false
	m.st.Read(func(v store.View) { self = v.Self() })
	cmd := m.pick(func(ms []slack.Message, at int) int {
		for i := range ms {
			if ms[i].TS > m.newAt && ms[i].User != self {
				found = true
				return i
			}
		}
		return -1
	})
	if !found {
		return m.say("nothing new here", false)
	}
	return cmd
}

// selected is the message under the cursor.
func (m *Model) selected() (msg slack.Message, ok bool) {
	m.st.Read(func(v store.View) {
		if w := m.window(v); w != nil && m.sel != "" {
			if i := msgIndex(w.Msgs, m.sel); i < len(w.Msgs) && w.Msgs[i].TS == m.sel {
				msg, ok = w.Msgs[i], true
			}
		}
	})
	return msg, ok
}

// lastOwn is your newest message in the open conversation.
func (m *Model) lastOwn() (msg slack.Message, ok bool) {
	m.st.Read(func(v store.View) {
		if w := m.window(v); w != nil {
			for i := len(w.Msgs) - 1; i >= 0; i-- {
				if w.Msgs[i].User == v.Self() && w.Msgs[i].Subtype == "" {
					msg, ok = w.Msgs[i], true
					return
				}
			}
		}
	})
	return msg, ok
}

func (m *Model) own(msg slack.Message) bool {
	self := ""
	m.st.Read(func(v store.View) { self = v.Self() })
	return msg.User == self && msg.Subtype == ""
}

// --- what to do with a message ---

// edit puts msg in the composer to be changed.
func (m *Model) edit(msg slack.Message) tea.Cmd {
	if !m.own(msg) {
		return m.say("that one's not yours to edit", false)
	}
	m.keep(m.open) // what was being written comes back after
	m.editing = msg.TS
	m.st.Read(func(v store.View) { m.load(decode(v, msg.Text)) })
	m.focus = onCompose
	return nil
}

func (m *Model) cancelEdit() {
	m.editing = ""
	m.restore(m.open)
}

// remove deletes msg, on the second press of d.
func (m *Model) remove(msg slack.Message) tea.Cmd {
	if !m.own(msg) {
		return m.say("that one's not yours to bin", false)
	}
	if m.deleting != msg.TS {
		m.deleting = msg.TS
		return m.say("d again to bin it", false)
	}
	m.deleting = ""
	conv, ts := m.open, msg.TS
	m.st.Remove(conv, ts)
	return func() tea.Msg {
		err := m.api.Delete(m.ctx, conv, ts)
		if err != nil {
			err = m.st.Refresh(m.ctx, m.api, conv) // bring it back
		}
		return sentMsg{err}
	}
}

// plainText is msg as it reads, for the clipboard.
func plainText(v store.View, text string) string {
	var b strings.Builder
	for i, l := range mrkdwn.Parse(text) {
		if i > 0 {
			b.WriteByte('\n')
		}
		if l.Quote {
			b.WriteString("> ")
		}
		for _, s := range l.Spans {
			b.WriteString(spanText(v, s))
		}
	}
	return b.String()
}

// spanText is how a span reads.
func spanText(v store.View, s mrkdwn.Span) string {
	switch s.Kind {
	case mrkdwn.User:
		return "@" + v.Person(s.Target).Name
	case mrkdwn.Channel:
		if s.Text != "" {
			return "#" + s.Text
		}
		if c := v.Conv(s.Target); c != nil {
			return "#" + c.Name
		}
		return "#" + s.Target
	case mrkdwn.Group:
		if g, ok := v.Group(s.Target); ok {
			return "@" + g.Handle
		}
		if s.Text == "" {
			return "@" + s.Target
		}
	case mrkdwn.Emoji:
		e, _ := emojiText(s.Text)
		return e
	}
	return s.Text
}

func (m *Model) copyText(msg slack.Message) tea.Cmd {
	var text string
	m.st.Read(func(v store.View) { text = plainText(v, msg.Text) })
	return tea.Batch(tea.SetClipboard(text), m.say("copied the message", false))
}

// permalink is the message's link in Slack.
func (m *Model) permalink(msg slack.Message) string {
	var domain string
	m.st.Read(func(v store.View) { domain = v.Team().Domain })
	link := "https://" + domain + ".slack.com/archives/" + m.open + "/p" + strings.Replace(msg.TS, ".", "", 1)
	if msg.ThreadTS != "" && msg.ThreadTS != msg.TS {
		link += "?thread_ts=" + msg.ThreadTS + "&cid=" + m.open // a reply's, as Slack makes them
	}
	return link
}

// openLink opens the first web link in msg in the browser. Only http,
// https and mailto: messages are from anyone, and other schemes can
// start apps.
func (m *Model) openLink(msg slack.Message) tea.Cmd {
	for _, l := range mrkdwn.Parse(msg.Text) {
		for _, s := range l.Spans {
			if s.Kind != mrkdwn.Link {
				continue
			}
			u, err := url.Parse(s.Target)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
				continue
			}
			target := u.String()
			return tea.Batch(m.say("opening "+target, false), func() tea.Msg {
				if err := exec.Command("open", target).Run(); err != nil {
					slog.Warn("open link", "err", err)
				}
				return nil
			})
		}
	}
	return m.say("no link in that one", false)
}
