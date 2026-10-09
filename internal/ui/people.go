package ui

import (
	"strings"
	"time"

	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/fuzzy"
	"github.com/0xdeafcafe/photon/theme"
)

// People on screen (docs/ui.md, Presence and status): the dot beside a DM,
// the status emoji beside a name, user groups in the @ popup, and what
// the websocket is asked to say about who. The profile card is profile.go.

// peopleUI is what the model keeps for them.
type peopleUI struct {
	watching watched
	card     profileCard
}

// watched is what presence was last asked for, so asking again waits for
// something to have changed.
type watched struct {
	open, last string
	sides, n   int
}

// watchPeople asks the socket for the presence of the people in your DMs
// and those writing in the open conversation. shortcut: every DM in the
// sidebar rather than the ones on screen; the web client sends the
// visible ones, so look at that if a workspace has hundreds of DMs.
func (m *Model) watchPeople() {
	if m.api == nil {
		return
	}
	m.st.Read(func(v store.View) {
		key := watched{open: m.open, sides: len(m.side)}
		w := v.Window(m.open)
		if w != nil && len(w.Msgs) > 0 {
			key.last, key.n = w.Msgs[len(w.Msgs)-1].TS, len(w.Msgs)
		}
		if key == m.ppl.watching {
			return
		}
		m.ppl.watching = key
		var ids []string
		for _, it := range m.side {
			if c := v.Conv(it.conv); c != nil && c.Kind == store.IM && !v.Person(c.User).Bot {
				ids = append(ids, c.User)
			}
		}
		if w != nil {
			for i := range w.Msgs {
				ids = append(ids, w.Msgs[i].User)
			}
		}
		m.api.Watch(ids)
	})
}

// dmMark is the marker for the DM c and its colour: ● green while they're
// active, ○ faint away, ● dim until Slack has said, and ◇ for an app.
// ink and green are those of the ground it sits on.
func dmMark(v store.View, c *store.Conv, ink Inks, green canvas.Style) (string, theme.RGB) {
	if v.Person(c.User).Bot {
		return "◇", ink.Dim.FG
	}
	switch v.Presence(c.User) {
	case "active":
		return "●", green.FG
	case "away":
		return "○", ink.Faint.FG
	}
	return "●", ink.Dim.FG
}

// statusGlyph is id's status emoji as a character, or "" when they have
// none, it's run out, or it's a custom emoji (which waits for images).
func statusGlyph(v store.View, id string) string {
	if id == "" {
		return ""
	}
	p := v.Person(id)
	if p.StatusEmoji == "" || (p.StatusUntil != 0 && p.StatusUntil < time.Now().Unix()) {
		return ""
	}
	if e, std := emojiText(strings.Trim(p.StatusEmoji, ":")); std {
		return e
	}
	return ""
}

// dmTitle is a DM's header: its marker, their name, status and presence.
func (m *Model) dmTitle(v store.View, c *store.Conv) canvas.Row {
	ink := m.pal.Main
	mark, fg := dmMark(v, c, ink, m.pal.Green)
	bg := m.pal.Panel
	row := canvas.Row{canvas.T(" ", bg), canvas.T(mark, bg.Fg(fg)), canvas.T(" ", bg), canvas.T(v.Title(c), bg.Fg(ink.Bright.FG).With(canvas.Bold))}
	if g := statusGlyph(v, c.User); g != "" {
		row = append(row, canvas.T(" "+g, bg), canvas.T(" "+v.Person(c.User).StatusText, bg.Fg(ink.Dim.FG)))
	}
	if pr := v.Presence(c.User); pr != "" {
		row = append(row, canvas.T("   "+pr, bg.Fg(ink.Dim.FG)))
	}
	return row
}

// groups finds the user groups q means, for the @ popup.
func (m *Model) groups(v store.View, q string, out []item) []item {
	v.EachGroup(func(g store.Group) {
		s1, lit, ok1 := fuzzy.Match(q, g.Handle)
		s2, _, ok2 := fuzzy.Match(q, g.Name)
		if !ok1 && !ok2 {
			return
		}
		it := item{label: "@" + g.Handle, detail: g.Name, code: "<!subteam^" + g.ID + "|@" + g.Handle + ">", score: max(s1, s2)}
		if ok1 {
			for _, i := range lit {
				it.lit = append(it.lit, i+1)
			}
		}
		out = append(out, it)
	})
	return out
}
