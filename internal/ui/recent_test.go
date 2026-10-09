package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/loafer/internal/store"
)

var ctrlEnd = tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl}

// newest is the open window's newest message's ts.
func newestTS(m *Model) (ts string) {
	m.st.Read(func(v store.View) { w := v.Window(m.open); ts = w.Msgs[len(w.Msgs)-1].TS })
	return ts
}

// Scrolled up, a pill says how many new messages are below; ctrl+end goes
// back down. A window a search left back in time says only that there are
// newer ones.
func TestNewerPill(t *testing.T) {
	m := fixture(t)
	m.st.MarkRead("C1", newestTS(m))
	if strings.Contains(frameText(m), "newer messages") {
		t.Fatal("a pill at the newest")
	}
	m.Update(tea.MouseWheelMsg{X: 60, Y: 20, Button: tea.MouseWheelUp})
	if text := frameText(m); !strings.Contains(text, "↓ newer messages  ctrl+end") {
		t.Fatalf("no pill scrolled up:\n%s", text)
	}
	m.st.Add("C1", slack.Message{TS: "1999999999.000001", User: "U1", Text: "below you"})
	m.st.Add("C1", slack.Message{TS: "1999999999.000002", User: "U0", Text: "yours don't count"})
	m.Update(storeMsg{})
	if text := frameText(m); !strings.Contains(text, "↓ 1 new message  ctrl+end") {
		t.Fatalf("no count:\n%s", text)
	}
	unread := false
	m.st.Read(func(v store.View) { unread = v.Conv("C1").Unread })
	if !unread {
		t.Fatal("read while scrolled up")
	}
	press(m, ctrlEnd)
	if text := frameText(m); m.scroll != 0 || strings.Contains(text, "new message") || !strings.Contains(text, "below you") {
		t.Fatalf("ctrl+end:\n%s", text)
	}

	m.st.Read(func(v store.View) { v.Window("C1").Newer = true })
	press(m, tab) // in the messages, the key is G
	if text := frameText(m); !strings.Contains(text, "↓ newer messages  G") {
		t.Fatalf("no pill back in time:\n%s", text)
	}
}

// With the terminal in the background, what arrives isn't read; it is
// once the terminal has focus again.
func TestReadOnlyWhenFocused(t *testing.T) {
	m := fixture(t)
	unread := func() (u bool) {
		m.st.Read(func(v store.View) { u = v.Conv("C1").Unread })
		return u
	}
	m.Update(tea.BlurMsg{})
	m.st.Add("C1", slack.Message{TS: "1999999999.000001", User: "U1", Text: "while you were out"})
	m.Update(storeMsg{})
	if !unread() {
		t.Fatal("read in the background")
	}
	m.Update(tea.FocusMsg{})
	if unread() {
		t.Fatal("not read on coming back")
	}
}

// The wheel scrolls the thread when the pointer's over it.
func TestWheelOverThread(t *testing.T) {
	m := fixture(t)
	press(m, tab, r('k'), r('k'), r('t'))
	m.Update(tea.MouseWheelMsg{X: m.w - 5, Y: 20, Button: tea.MouseWheelUp})
	if m.th.scroll != 3 || m.scroll != 0 {
		t.Fatalf("over the thread: thread %d, conversation %d", m.th.scroll, m.scroll)
	}
	m.Update(tea.MouseWheelMsg{X: 40, Y: 20, Button: tea.MouseWheelUp})
	if m.th.scroll != 3 || m.scroll != 3 {
		t.Fatalf("over the conversation: thread %d, conversation %d", m.th.scroll, m.scroll)
	}
}

// Narrow, the thread hides the conversation, so no key puts focus there
// behind it: going to the conversation closes the thread.
func TestNarrowFocus(t *testing.T) {
	m := fixture(t)
	m.w = 80
	press(m, tab, r('k'))
	parent := m.sel
	press(m, r('t'))
	press(m, esc) // its box → its messages
	if m.focus != onThread || m.th.ts == "" {
		t.Fatalf("esc: focus %v", m.focus)
	}
	press(m, r('h')) // ← goes to the conversation, so the thread goes
	if m.th.ts != "" || m.focus != onMsgs || m.sel != parent {
		t.Fatalf("←: thread %q focus %v sel %q", m.th.ts, m.focus, m.sel)
	}

	press(m, r('t'), tab) // its box → the sidebar
	if m.focus != onSide || m.th.ts == "" {
		t.Fatalf("tab: focus %v", m.focus)
	}
	press(m, r('i'))
	if m.th.ts != "" || m.focus != onCompose {
		t.Fatalf("i: thread %q focus %v", m.th.ts, m.focus)
	}
}

// Against the fake Slack: scrolled up in #dev, what's said there shows as
// a pill and stays unread, and ctrl+end goes down to it and reads it.
func TestE2ENewerPill(t *testing.T) {
	d := newE2E(t)
	d.jump("dev", slacktest.Dev)
	d.until("#dev", func() bool { return d.has("3 replies") && !d.unread(slacktest.Dev) })
	d.m.h = 20
	for range 3 {
		d.update(tea.MouseWheelMsg{X: 60, Y: 10, Button: tea.MouseWheelUp})
	}
	d.srv.Post(slacktest.Dev, slacktest.Priya, "while you were up there", "")
	d.until("the pill", func() bool { return d.has("↓ 1 new message") && d.unread(slacktest.Dev) })
	d.press(ctrlEnd)
	d.until("down and read", func() bool {
		return d.has("while you were up there") && !d.has("new message") && !d.unread(slacktest.Dev)
	})
}
