package ui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
)

// Managing the sidebar (docs/ui.md, Managing conversations): with a
// section or a conversation selected, z folds the section, m mutes, * stars,
// s moves it to another section, x leaves a channel or closes a DM, b
// browses channels you're not in and N starts a DM. Each changes the store
// at once and tells Slack in the background; if Slack says no, it's put
// back.

// managedMsg is Slack's answer to one of them. undo puts the store back;
// it's nil where the change stands whatever Slack says.
type managedMsg struct {
	what string
	err  error
	undo func()
}

type manage struct {
	pk    picker
	ask   string    // the channel x was pressed on, awaiting y
	until time.Time // when the question lapses
}

// managed takes the answers to everything here, and to browse and the
// new DM box.
func (m *Model) managed(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case managedMsg:
		if msg.err != nil {
			if msg.undo != nil {
				msg.undo()
			}
			return m.say("couldn't "+msg.what+": "+msg.err.Error(), true)
		}
	case browsedMsg:
		return m.browsed(msg)
	case dmMsg:
		return m.dmOpened(msg)
	case joinedMsg:
		return m.joined(msg)
	}
	return nil
}

// manageKey takes the sidebar keys that manage it; ok says it did.
func (m *Model) manageKey(s string) (cmd tea.Cmd, ok bool) {
	if m.sideAt < len(m.side) && m.side[m.sideAt].conv == "" && (s == "enter" || s == "l" || s == "right") {
		return m.fold(), true // on a heading, enter opens or shuts it
	}
	switch s {
	case "z", "space":
		return m.fold(), true
	case "m":
		return m.mute(), true
	case "*":
		return m.star(), true
	case "s":
		return m.openMove(), true
	case "x":
		return m.leave(), true
	case "b":
		return m.openBrowse(), true
	case "N":
		return m.openNewDM(), true
	}
	return nil, false
}

// selConv is the conversation selected in the sidebar, "" on a heading.
func (m *Model) selConv() string {
	if m.sideAt < len(m.side) {
		return m.side[m.sideAt].conv
	}
	return ""
}

// --- sections ---

// shown is the conversations of sec the sidebar lists: all of them, or in a
// folded section only those that want you and the one that's open. hidden
// is how many it leaves out.
func (m *Model) shown(v store.View, sec store.Section) (ids []string, hidden int) {
	if !sec.Collapsed {
		return sec.Convs, 0
	}
	for _, id := range sec.Convs {
		if c := v.Conv(id); c != nil && (id == m.open || unreadConv(c)) {
			ids = append(ids, id)
		} else {
			hidden++
		}
	}
	return ids, hidden
}

// fold folds or unfolds the selected section. The cursor lands on its
// heading, since what was under it may have gone.
func (m *Model) fold() tea.Cmd {
	at := min(m.sideAt, len(m.side)-1)
	for at > 0 && m.side[at].conv != "" {
		at--
	}
	if at < 0 || m.side[at].conv != "" {
		return nil
	}
	id, on := m.side[at].id, !m.side[at].folded
	if !m.st.Collapse(id, on) {
		return m.say("that section can't be folded", false)
	}
	m.sideAt = at
	// shortcut: a failed save leaves it folded here, as the method is a guess
	// and a fold that sprang open would be worse than one that doesn't last.
	return func() tea.Msg {
		return managedMsg{what: "save the fold", err: m.api.SetCollapsed(m.ctx, id, on)}
	}
}

// sideHead draws a section's heading: its fold, its name and how many
// conversations a fold hides.
func (m *Model) sideHead(it sideItem, w int, selected bool) canvas.Row {
	ink := m.pal.Side
	name := it.section
	if it.emoji != "" {
		if e, std := emojiText(strings.Trim(it.emoji, ":")); std {
			name = e + " " + name
		}
	}
	base, mark, fold := ink.Text, canvas.T(" ", ink.Text), "▾ "
	if selected && m.focus == onSide {
		base = ink.Hover
		mark = canvas.T(" ", base)
	}
	if selected {
		mark = canvas.T("▍", base.Fg(m.pal.Orange.FG))
	}
	if it.folded {
		fold = "▸ "
	}
	head := canvas.Row{mark, canvas.T(fold, base.Fg(ink.Faint.FG)), canvas.T(name+" ", base.Fg(ink.Sub.FG).With(canvas.Bold))}
	if it.hidden > 0 {
		head = append(head, canvas.T(strconv.Itoa(it.hidden)+" ", base.Fg(ink.Dim.FG)))
	}
	if fill := w - head.Width(); fill > 0 {
		head = append(head, canvas.T(strings.Repeat("─", fill), base.Fg(ink.Faint.FG)))
	}
	return canvas.Fit(head, w, base)
}

// --- mute and star ---

func (m *Model) mute() tea.Cmd {
	id := m.selConv()
	if id == "" {
		return nil
	}
	var on bool
	var title string
	m.st.Read(func(v store.View) {
		if c := v.Conv(id); c != nil {
			on, title = !c.Muted, convLabel(v, c)
		}
	})
	if title == "" {
		return nil
	}
	value := m.st.Mute(id, on)
	say := "muted " + title
	if !on {
		say = "unmuted " + title
	}
	return tea.Batch(m.say(say, false), func() tea.Msg {
		return managedMsg{what: "mute that", err: m.api.SetPref(m.ctx, "muted_channels", value),
			undo: func() { m.st.Mute(id, !on) }}
	})
}

// star moves the selected conversation into the Starred section, or back
// to where its kind puts it if it's there already.
func (m *Model) star() tea.Cmd {
	id := m.selConv()
	if id == "" {
		return nil
	}
	stars, here := "", false
	m.st.Read(func(v store.View) {
		for _, sec := range v.Sections() {
			if sec.Type == "stars" {
				stars = sec.ID
			}
		}
		here = v.SectionOf(id) == stars
	})
	switch {
	case stars == "":
		return m.say("no Starred section yet", false)
	case here:
		return m.moveTo(id, "", "unstarred")
	}
	return m.moveTo(id, stars, "starred")
}

// moveTo puts conv in section to ("" for where its kind does), saying what
// was done.
func (m *Model) moveTo(conv, to, done string) tea.Cmd {
	from, ok := m.st.Move(conv, to)
	if !ok {
		return m.say("that section has gone", true)
	}
	return tea.Batch(m.say(done, false), func() tea.Msg {
		return managedMsg{what: "move that", err: m.api.MoveConv(m.ctx, conv, from, to),
			undo: func() { m.st.Move(conv, from) }}
	})
}

// --- leaving ---

// leave closes the selected DM, or asks before leaving the channel.
func (m *Model) leave() tea.Cmd {
	id := m.selConv()
	var c store.Conv
	var title string
	m.st.Read(func(v store.View) {
		if x := v.Conv(id); x != nil {
			c, title = *x, convLabel(v, x)
		}
	})
	switch {
	case title == "":
		return nil
	case c.Kind == store.IM || c.Kind == store.MPIM:
		return m.drop(id, false)
	}
	m.mg.ask, m.mg.until = id, time.Now().Add(6*time.Second)
	return m.say("leave "+title+"?  y leave · any other key keeps it", false)
}

// askKey is the answer to leave's question; any key is.
func (m *Model) askKey(s string) tea.Cmd {
	id, until := m.mg.ask, m.mg.until
	m.mg.ask = ""
	m.flash = ""
	if s == "y" && time.Now().Before(until) {
		return m.drop(id, true)
	}
	return nil
}

// drop takes conv out of the sidebar, going to the next conversation if it
// was open, and leaves or closes it at Slack.
func (m *Model) drop(id string, leaving bool) tea.Cmd {
	next := ""
	if id == m.open {
		next = m.step(1, anyConv)
	}
	title := ""
	m.st.Read(func(v store.View) {
		if c := v.Conv(id); c != nil {
			title = convLabel(v, c)
		}
	})
	c, ok := m.st.Drop(id)
	if !ok {
		return nil
	}
	m.back = slices.DeleteFunc(m.back, func(x string) bool { return x == id })
	m.fwd = slices.DeleteFunc(m.fwd, func(x string) bool { return x == id })
	var cmd tea.Cmd
	if id == m.open {
		if next != "" {
			cmd = m.openConv(next)
		} else {
			m.open = ""
		}
	}
	what, done := "close that", "closed "
	if leaving {
		what, done = "leave that", "left "
	}
	return tea.Batch(cmd, m.say(done+title, false), func() tea.Msg {
		var err error
		if leaving {
			err = m.api.Leave(m.ctx, id)
		} else {
			err = m.api.Close(m.ctx, id)
		}
		return managedMsg{what: what, err: err, undo: func() { m.st.Restore(c) }}
	})
}
