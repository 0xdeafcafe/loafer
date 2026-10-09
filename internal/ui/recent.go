package ui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
)

// Keeping up with the newest (docs/ui.md, Main screen): a pill at the foot
// of the conversation while you're away from its newest message, and
// marking it read only while you're watching it.

// newerPill puts "↓ 3 new messages" on the last of list's rows when it's
// scrolled up or a search left it back in time, with the key back down.
// The count is what's arrived since it was last read, where the window
// reaches that far.
func (m *Model) newerPill(v store.View, c *store.Conv, list []canvas.Row, w int) []canvas.Row {
	win := v.Window(c.ID)
	if win == nil || len(list) == 0 || (m.scroll == 0 && !win.Newer) {
		return list
	}
	text := "↓ newer messages"
	if !win.Newer {
		n := 0
		for i := len(win.Msgs) - 1; i >= 0 && win.Msgs[i].TS > c.LastRead; i-- {
			if win.Msgs[i].User != v.Self() {
				n++
			}
		}
		switch {
		case n == 1:
			text = "↓ 1 new message"
		case n > 1:
			text = "↓ " + strconv.Itoa(n) + " new messages"
		}
	}
	key := "ctrl+end"
	if m.focus == onMsgs {
		key = "G"
	}
	pill := canvas.Row{canvas.T(" "+text+"  ", m.pal.Chip), canvas.T(key+" ", m.pal.Chip.With(canvas.Bold))}
	if pw := pill.Width(); pw < w {
		last := len(list) - 1
		list[last] = canvas.Splice(list[last], (w-pw)/2, pill)
	}
	return list
}

// readIfWatching marks the open conversation read if you're looking at its
// newest message in a focused terminal: a backgrounded loafer reads nothing.
func (m *Model) readIfWatching() tea.Cmd {
	if m.open == "" || m.scroll != 0 || m.al.blurred {
		return nil
	}
	return m.markRead()
}
