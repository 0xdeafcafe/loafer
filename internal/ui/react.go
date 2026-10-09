package ui

import (
	"errors"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/emoji"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/cellw"
)

// r on a message opens a picker over every emoji, yours first; enter adds
// the reaction, or takes it away if it's already yours (marked ✓). On the
// message itself, 1 to 9 toggle its reactions in the order they're drawn.
// Either way the store changes at once, Slack is told in the background,
// and its event, when it comes, finds it done.

type reactor struct {
	on       bool
	conv, ts string
	mine     []string // the names you've already reacted with on the message
	query    []rune
	at       int
	items    []emojiHit
}

type reactedMsg struct{ err error }

const pickCap = 100

// reactPicker opens the picker on msg.
func (m *Model) reactPicker(msg slack.Message) {
	m.emo.pick = reactor{on: true, conv: m.open, ts: msg.TS}
	m.st.Read(func(v store.View) {
		for _, r := range msg.Reactions {
			if slices.Contains(r.Users, v.Self()) {
				m.emo.pick.mine = append(m.emo.pick.mine, r.Name)
			}
		}
		m.findReact(v)
	})
}

func (m *Model) findReact(v store.View) {
	p := &m.emo.pick
	p.items, p.at = m.emo.find(v, string(p.query), true, pickCap), 0
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

func (m *Model) reactKey(k tea.KeyPressMsg) tea.Cmd {
	p := &m.emo.pick
	switch k.String() {
	case "esc", "ctrl+c":
		p.on = false
	case "enter":
		p.on = false
		if p.at < len(p.items) {
			name := emoji.Canon(p.items[p.at].name)
			return m.react(p.conv, p.ts, name, !slices.Contains(p.mine, name))
		}
	case "up", "ctrl+p":
		if n := len(p.items); n > 0 {
			p.at = (p.at + n - 1) % n
		}
	case "down", "ctrl+n":
		if n := len(p.items); n > 0 {
			p.at = (p.at + 1) % n
		}
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

// overlayReact draws the picker over the frame, which goes faint behind
// it, as overlayJump does.
func (m *Model) overlayReact(frame []canvas.Row) []canvas.Row {
	ink, p := m.pal.Main, &m.emo.pick
	bw := min(m.w-4, 40)
	if bw < 24 {
		return frame
	}
	inner := bw - 4
	top := min(m.h/8, 4)
	listH := max(1, min(len(p.items), m.h*2/3-5))
	edge, fill := m.pal.Orange, ink.Text.Bg(ink.Sel.BG)

	line := func(r canvas.Row) canvas.Row {
		return append(append(canvas.Row{canvas.T("│ ", edge.Bg(fill.BG))}, canvas.Fit(r, inner, fill)...), canvas.T(" │", edge.Bg(fill.BG)))
	}
	edgeRow := func(l, label, r string) canvas.Row {
		row := canvas.Row{canvas.T(l+"─ ", edge), canvas.T(label, ink.Sub.With(canvas.Bold))}
		return append(row, canvas.T(" "+strings.Repeat("─", max(0, bw-row.Width()-2))+r, edge))
	}
	box := []canvas.Row{edgeRow("╭", "☺ React", "╮")}
	box = append(box, line(canvas.Row{canvas.T("❯ ", fill.Fg(m.pal.Orange.FG).With(canvas.Bold)), canvas.T(string(p.query), fill.Fg(ink.Bright.FG)), canvas.T("▏", fill.Fg(m.pal.Orange.FG))}))
	box = append(box, line(canvas.Row{canvas.T(strings.Repeat("─", inner), fill.Fg(ink.Faint.FG))}))

	if len(p.items) == 0 {
		box = append(box, line(canvas.Row{canvas.T("  nothing matches", fill.Fg(ink.Dim.FG))}))
	}
	from := max(0, p.at-listH+1)
	for i := from; i < min(len(p.items), from+listH); i++ {
		box = append(box, line(m.reactRow(p.items[i], i == p.at, slices.Contains(p.mine, emoji.Canon(p.items[i].name)), inner, fill)))
	}
	box = append(box, edgeRow("╰", "↑↓ choose · enter react · esc close", "╯"))

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

func (m *Model) reactRow(h emojiHit, sel, mine bool, w int, fill canvas.Style) canvas.Row {
	ink := m.pal.Main
	base, mark := fill, canvas.T("  ", fill)
	if sel {
		base = fill.Bg(ink.Hover.BG)
		mark = canvas.T("▍ ", base.Fg(m.pal.Orange.FG))
	}
	glyph := h.glyph
	if glyph == "" {
		glyph = "◌" // a custom emoji, which has no character
	}
	row := canvas.Row{mark, canvas.T(glyph, base), canvas.T(strings.Repeat(" ", max(1, 3-cellw.String(glyph))), base)}
	for j, r := range []rune(h.name) {
		st := base.Fg(ink.Text.FG)
		if slices.Contains(h.lit, j) {
			st = base.Fg(m.pal.Orange.FG).With(canvas.Bold)
		}
		row = append(row, canvas.T(string(r), st)) // a seg a rune; names are short
	}
	var tail canvas.Row
	if mine {
		tail = canvas.Row{canvas.T("✓ ", base.Fg(m.pal.Green.FG).With(canvas.Bold))}
	}
	return rightAlign(row, tail, w, base)
}
