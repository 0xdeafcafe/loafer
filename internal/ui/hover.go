package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/cellw"
)

// Hover (docs/ui.md, Messages): the message under the pointer takes the
// hover ground, with a faint hint of its actions at the right of its
// first row, which a click works. It's cheap by construction: the list
// notes which message each of its rows is as it draws them, so a motion
// is a lookup; a motion within the same message changes nothing and
// keeps the last frame; and the hover is a restyled copy of the
// message's kept rows, so no message is drawn again for it.

type hoverUI struct {
	ts    string   // the message under the pointer; "" for none
	x, y  int      // where the conversation's list starts on screen
	w     int      // its width; 0 when it isn't showing
	rows  []string // which message each row drawn is ("" for dividers)
	from  int      // the first of rows on screen
	lead  int      // blank rows above them, when the list is short
	above int      // the last block's divider rows
	n     int      // messages drawn (renderMessage), for the tests
}

// hoverHint is the actions offered at the right of a hovered message,
// each with the key a click on it presses.
var hoverHint = []struct{ label, key string }{{"☺ react", "r"}, {"↩ reply", "t"}, {"⋯", "."}}

// under is the message at (x, y) on screen, and which of its rows.
func (m *Model) under(x, y int) (ts string, row int) {
	h := &m.hov
	i := y - h.y - h.lead + h.from
	if h.w == 0 || x < h.x || x >= h.x+h.w || y < h.y+h.lead || i >= len(h.rows) || i < 0 {
		return "", 0
	}
	ts = h.rows[i]
	for i > 0 && h.rows[i-1] == ts {
		i, row = i-1, row+1
	}
	return ts, row
}

// hoverAt takes the pointer moving to (x, y); false when the hover is
// as it was, and there's nothing to draw.
func (m *Model) hoverAt(x, y int) bool {
	ts, _ := m.under(x, y)
	if ts == m.hov.ts {
		return false
	}
	m.hov.ts = ts
	return true
}

// click on a message picks it; on one of its hint's actions, it does
// that too.
func (m *Model) click(msg tea.MouseClickMsg) tea.Cmd {
	ts, row := m.under(msg.X, msg.Y)
	if ts == "" || msg.Button != tea.MouseLeft {
		return nil
	}
	m.setFocus(onMsgs)
	m.sel, m.hov.ts = ts, ""
	if row != 0 {
		return nil
	}
	at := m.hov.x + m.hov.w - hintWidth() - 1
	for i, a := range hoverHint {
		w := cellw.String(a.label)
		if msg.X >= at && msg.X < at+w {
			return m.key(tea.KeyPressMsg{Code: rune(a.key[0]), Text: a.key})
		}
		at += w
		if i < len(hoverHint)-1 {
			at += 3 // " · "
		}
	}
	return nil
}

func hintWidth() int {
	n := 0
	for i, a := range hoverHint {
		n += cellw.String(a.label)
		if i > 0 {
			n += 3
		}
	}
	return n
}

// hovered is a message's kept rows on the hover ground, with the hint at
// the right of the first; chips keep their own ground.
func (m *Model) hovered(rows []canvas.Row, w int) []canvas.Row {
	ink := m.pal.Main
	out := make([]canvas.Row, len(rows))
	for i, r := range rows {
		nr := make(canvas.Row, len(r))
		for j, s := range r {
			if s.St.BG == ink.Text.BG {
				s.St = s.St.Bg(ink.Hover.BG)
			}
			nr[j] = s
		}
		out[i] = nr
	}
	if len(out) > 0 && w > hintWidth()+20 {
		hint := make(canvas.Row, 0, 2*len(hoverHint)+1)
		hint = append(hint, canvas.T(" ", ink.Hover))
		for i, a := range hoverHint {
			if i > 0 {
				hint = append(hint, canvas.T(" · ", ink.Hover.Fg(ink.Faint.FG)))
			}
			hint = append(hint, canvas.T(a.label, ink.Hover.Fg(ink.Dim.FG)))
		}
		out[0] = canvas.Splice(out[0], w-hintWidth()-2, hint)
	}
	return out
}
