package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/canvas"
)

var shiftTab = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}

func TestThreadKeys(t *testing.T) {
	m := fixture(t)
	press(m, tab, r('k'))
	parent := m.sel
	press(m, r('t'))
	if m.th.ts != parent || m.th.conv != "C1" || m.focus != onReply || m.sel != "" {
		t.Fatalf("t: thread %q focus %v sel %q", m.th.ts, m.focus, m.sel)
	}
	for _, size := range [][2]int{{120, 40}, {80, 24}} {
		m.w, m.h = size[0], size[1]
		rows := m.render()
		for i, r := range rows {
			if r.Width() != m.w {
				t.Fatalf("%v: row %d is %d wide: %q", size, i, r.Width(), plainFrame([]canvas.Row{r}))
			}
		}
		text := strings.Join(plainFrame(rows), "\n")
		narrow := !strings.Contains(text, "a message for # dev")
		if !strings.Contains(text, "thread  # dev") || !strings.Contains(text, "reply in thread") || narrow != (m.w < 100) {
			t.Fatalf("%v: pane not drawn as it should be:\n%s", size, text)
		}
	}
	m.w, m.h = 120, 40

	// The box is the thread's: what's written there stays there.
	press(m, r('h'), r('i'))
	if string(m.th.input) != "hi" || len(m.input) != 0 {
		t.Fatalf("typed into %q, the conversation's has %q", string(m.th.input), string(m.input))
	}
	press(m, tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if !m.th.also || !strings.Contains(strings.Join(plainFrame(m.render()), "\n"), "☑ also # dev") {
		t.Fatal("ctrl+b should tick also send to the channel")
	}

	// tab goes round: sidebar, conversation, its box, the thread, its box.
	var got []focus
	for range 5 {
		press(m, tab)
		got = append(got, m.focus)
	}
	if want := []focus{onSide, onMsgs, onCompose, onThread, onReply}; !slices.Equal(got, want) {
		t.Fatalf("tab: %v, want %v", got, want)
	}
	press(m, shiftTab)
	if m.focus != onThread {
		t.Fatalf("shift+tab: %v", m.focus)
	}
	if hints := strings.Join(plainFrame([]canvas.Row{m.hints(viewOf(m))}), ""); !strings.Contains(hints, "esc close") {
		t.Fatalf("hints: %q", hints)
	}

	// esc closes it, back on the parent; t opens it again with its draft.
	press(m, esc)
	if m.th.ts != "" || m.focus != onMsgs || m.sel != parent {
		t.Fatalf("esc: thread %q focus %v sel %q", m.th.ts, m.focus, m.sel)
	}
	press(m, r('t'))
	if string(m.th.input) != "hi" {
		t.Fatalf("the thread's draft: %q", string(m.th.input))
	}
	// Going elsewhere closes it.
	press(m, altDown)
	if m.th.ts != "" || m.focus != onCompose {
		t.Fatalf("alt+down: thread %q focus %v", m.th.ts, m.focus)
	}
}

func viewOf(m *Model) (v store.View) {
	m.st.Read(func(x store.View) { v = x })
	return v
}

// A thread opened against the fake Slack: its replies, one arriving
// live, and one sent from its box.
func TestE2EThread(t *testing.T) {
	d := newE2E(t)
	d.jump("dev", slacktest.Dev)
	d.until("#dev's messages", func() bool { return d.has("3 replies") })
	parent := d.srv.Messages(slacktest.Dev)[3].TS

	for d.m.focus != onMsgs { // round to the messages, on the newest
		d.press(tab)
	}
	d.press(r('k'), r('k'))
	if d.m.sel != parent {
		t.Fatalf("the cursor's on %q, not the parent %q (focus %v)\n%s", d.m.sel, parent, d.m.focus, d.text)
	}
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.until("the thread", func() bool { return d.has("thread  # dev") && d.has("container/list") && d.has("noted") })

	d.srv.Post(slacktest.Dev, slacktest.Jo, "same here", parent)
	d.until("the live reply", func() bool { return d.has("same here") && d.has("4 replies") })
	if testing.Verbose() {
		t.Log("\n" + d.text)
	}

	d.typed("on it")
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.until("the reply sent", func() bool { return d.sent == 1 && d.has("on it") && d.has("5 replies") })
	posted, marked := false, false
	for _, c := range d.srv.Calls() {
		posted = posted || (c.Method == "chat.postMessage" && c.Form.Get("thread_ts") == parent && c.Form.Get("text") == "on it")
		marked = marked || (c.Method == "subscriptions.thread.mark" && c.Form.Get("thread_ts") == parent)
	}
	if !posted || !marked {
		t.Fatalf("posted to the thread %v, marked it read %v", posted, marked)
	}
	for _, m := range d.srv.Messages(slacktest.Dev) {
		if m.Text == "on it" {
			t.Fatal("a reply isn't the channel's")
		}
	}

	d.press(esc, esc)
	if d.has("thread  # dev") || d.m.sel != parent {
		t.Fatalf("esc esc should close it, back on the parent:\n%s", d.text)
	}

	// a in the thread asks Claude about the whole of it.
	d.m.claude.link = &fakeRush{}
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.until("the thread again", func() bool { return d.has("on it") })
	d.press(esc, r('a'))
	if c := d.m.claude; d.m.tabs.on != tabClaude || len(c.srcs) != 1 || !strings.Contains(c.srcs[0].what, "thread in") || !strings.Contains(c.srcs[0].what, "5 replies") {
		t.Fatalf("tab %v chips %v", d.m.tabs.on, c.labels())
	}
}
