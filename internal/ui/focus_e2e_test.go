package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slacktest"
)

// The arrows go between the panes as Slack's do: → or enter from the
// sidebar into the messages, ← back, ↓ past the newest into the box, ←
// from an empty box to the sidebar, ↑ from an empty box with nothing of
// yours to edit up to the messages, and esc out of the box.
func TestE2EArrowsBetweenPanes(t *testing.T) {
	d := newE2E(t)
	left, right := tea.KeyPressMsg{Code: tea.KeyLeft}, tea.KeyPressMsg{Code: tea.KeyRight}
	down, enter, esc := tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: tea.KeyEscape}
	at := func(want focus, after string) {
		t.Helper()
		if d.m.focus != want {
			t.Fatalf("after %s focus is %d, want %d:\n%s", after, d.m.focus, want, d.text)
		}
	}

	d.selectSide(slacktest.Alerts)
	d.press(enter)
	at(onMsgs, "enter in the sidebar")
	d.until("#alerts", func() bool { return d.m.open == slacktest.Alerts && d.has("staging is fine again") })
	d.press(left)
	at(onSide, "← in the messages")
	d.press(right)
	at(onMsgs, "→ in the sidebar")
	d.until("the cursor", func() bool { return d.m.sel != "" })
	d.press(down)
	at(onCompose, "↓ past the newest")
	d.press(left)
	at(onSide, "← in an empty box")
	d.press(r('i'))
	at(onCompose, "i in the sidebar")
	d.press(up)
	at(onMsgs, "↑ in an empty box with nothing to edit")
	d.press(r('i'), esc)
	at(onMsgs, "esc in the box")

	// Something typed keeps ← for the cursor.
	d.press(r('i'), r('x'), left)
	at(onCompose, "← with text")
}
