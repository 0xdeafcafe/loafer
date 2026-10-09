package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/store"
)

func press(m *Model, keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		m.Update(k)
		m.render()
	}
}

func r(c rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c, Text: string(c)} }

var (
	tab      = tea.KeyPressMsg{Code: tea.KeyTab}
	esc      = tea.KeyPressMsg{Code: tea.KeyEscape}
	up       = tea.KeyPressMsg{Code: tea.KeyUp}
	altDown  = tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt}
	altLeft  = tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}
	altRight = tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt}
)

func TestMessageCursor(t *testing.T) {
	m := fixture(t)
	ts := func(n int) string { // message n's ts, as fixture makes them
		var got string
		m.st.Read(func(v store.View) { got = v.Window("C1").Msgs[n].TS })
		return got
	}
	press(m, tab) // sidebar → messages: the cursor starts on the newest
	if m.focus != onMsgs || m.sel != ts(39) {
		t.Fatalf("focus %v sel %q", m.focus, m.sel)
	}
	press(m, r('k'))
	if m.sel != ts(38) {
		t.Fatalf("k: sel %q, want %q", m.sel, ts(38))
	}
	// 36 is alex's, 37 and 38 drew's: { goes to the start of drew's run,
	// then to alex's.
	press(m, r('{'))
	if m.sel != ts(37) {
		t.Fatalf("{ from the run's middle: %q", m.sel)
	}
	press(m, r('{'))
	if m.sel != ts(36) {
		t.Fatalf("{ again: %q", m.sel)
	}
	press(m, r('g'))
	if m.sel != ts(0) || m.scroll == 0 {
		t.Fatalf("g: sel %q scroll %d", m.sel, m.scroll)
	}
	if text := strings.Join(plainFrame(m.render()), "\n"); !strings.Contains(text, "▍ Al  alex") || !strings.Contains(text, "message 0 ") {
		t.Fatalf("the cursor's message isn't in view:\n%s", text)
	}
	press(m, r('G'))
	if m.sel != "" || m.scroll != 0 {
		t.Fatalf("G: sel %q scroll %d", m.sel, m.scroll)
	}
}

func TestHistoryAndDrafts(t *testing.T) {
	m := fixture(t)
	press(m, tab, tab, r('h'), r('i')) // to the composer, write "hi"
	press(m, altDown)
	if m.open != "D1" || len(m.input) != 0 {
		t.Fatalf("alt+down: open %q input %q", m.open, string(m.input))
	}
	press(m, altLeft)
	if m.open != "C1" || string(m.input) != "hi" {
		t.Fatalf("alt+left: open %q input %q", m.open, string(m.input))
	}
	press(m, altRight)
	if m.open != "D1" {
		t.Fatalf("alt+right: open %q", m.open)
	}
}

func TestEditLast(t *testing.T) {
	m := fixture(t)
	press(m, tab, tab, up) // empty composer, ↑ edits your last message
	if !strings.HasPrefix(string(m.input), "message 39 ") || m.editing == "" {
		t.Fatalf("editing %q: %q", m.editing, string(m.input))
	}
	press(m, esc)
	if m.editing != "" || len(m.input) != 0 {
		t.Fatalf("esc should cancel: %q", string(m.input))
	}
}

func TestEditKeepsCursor(t *testing.T) {
	m := fixture(t)
	press(m, tab, r('e')) // the cursor starts on the newest, which is yours
	edited := m.editing
	if edited == "" || m.focus != onCompose {
		t.Fatalf("e should edit: %q", edited)
	}
	m.setFocus(onMsgs)
	if m.sel != edited {
		t.Fatalf("back from editing, the cursor moved: %q, want %q", m.sel, edited)
	}
}

// A name typed out comes first, then names it starts, however recent or
// unread the fuzzy hits are.
func TestJumpNameFirst(t *testing.T) {
	m := fixture(t)
	var b slack.UserBoot
	b.Self.ID = "U0"
	b.Channels = []slack.Conversation{
		{ID: "C1", Name: "dev", IsChannel: true, IsMember: true},
		{ID: "C2", Name: "alerts", IsChannel: true, IsMember: true},
		{ID: "C3", Name: "devops", IsChannel: true, IsMember: true},
		{ID: "C4", Name: "d-everything-v", IsChannel: true, IsMember: true},
		{ID: "C5", Name: "deploy-events", IsChannel: true, IsMember: true},
	}
	m.st.ApplyBoot(b)
	m.open, m.back = "C2", []string{"C3", "C5", "C4"}
	m.render()
	press(m, tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}, r('d'), r('e'), r('v'))
	var got []string
	for _, it := range m.bar.items {
		got = append(got, it.conv)
	}
	if len(got) < 3 || got[0] != "C1" || got[1] != "C3" {
		t.Fatalf("dev found %v", got)
	}

	// From #dev itself, typing dev still finds it.
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m.open = "C1"
	press(m, tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}, r('d'), r('e'), r('v'))
	if len(m.bar.items) == 0 || m.bar.items[0].conv != "C1" {
		t.Fatalf("dev from #dev found %+v", m.bar.items)
	}
}

func TestJump(t *testing.T) {
	m := fixture(t)
	press(m, tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if !m.bar.on {
		t.Fatal("ctrl+k should open the bar")
	}
	if text := strings.Join(plainFrame(m.render()), "\n"); !strings.Contains(text, "Jump") {
		t.Fatalf("no bar drawn:\n%s", text)
	}
	press(m, r('a'), r('l'))
	if len(m.bar.items) == 0 || m.bar.items[0].conv != "C2" {
		t.Fatalf("al: %+v", m.bar.items)
	}
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.bar.on || m.open != "C2" || m.focus != onCompose {
		t.Fatalf("enter: on %v open %q focus %v", m.bar.on, m.open, m.focus)
	}
}
