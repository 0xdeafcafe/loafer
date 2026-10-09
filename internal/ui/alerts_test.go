package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/notify"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

func typed(t *testing.T, m *Model, conv, user string) {
	t.Helper()
	raw := []byte(`{"type":"user_typing","channel":"` + conv + `","user":"` + user + `"}`)
	ev := slack.Event{Type: "user_typing", Raw: raw}
	if err := jsonx.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	ev.Raw = raw
	m.st.Apply(ev)
}

func TestTypingLine(t *testing.T) {
	m := fixture(t)
	has := func(s string) bool { return strings.Contains(strings.Join(plainFrame(m.render()), "\n"), s) }
	if has("typing") {
		t.Fatal("nobody is typing yet")
	}
	typed(t, m, "C1", "U1")
	if !has("drew is typing…") {
		t.Fatal("the open conversation should show who's typing")
	}
	typed(t, m, "C1", "U0") // you
	typed(t, m, "C2", "U1") // elsewhere
	if !has("drew is typing…") || has("alex is typing") {
		t.Fatal("you and other conversations shouldn't show")
	}
	typed(t, m, "C1", "U2")
	if !has("drew and U2 are typing…") {
		t.Fatal("two typists")
	}
	if cmd := m.watchTyping(); cmd == nil || !m.al.typing {
		t.Fatal("typing should arm the tick that clears it")
	}
	if cmd := m.watchTyping(); cmd != nil {
		t.Fatal("one tick at a time")
	}
	m.alert(typingMsg{})
	if !m.al.typing {
		t.Fatal("it should arm again while someone's still typing")
	}
}

func TestFocusAndNotes(t *testing.T) {
	m := fixture(t)
	m.update(tea.BlurMsg{})
	if !m.al.blurred {
		t.Fatal("blur")
	}
	m.update(tea.FocusMsg{})
	if m.al.blurred {
		t.Fatal("focus")
	}
	_, cmd := m.update(noteMsg(notify.Note{Title: "#dev", Body: "one"}))
	if cmd == nil || m.al.flushing {
		t.Fatal("the first note should go straight out")
	}
	_, cmd = m.update(noteMsg(notify.Note{Title: "#dev", Body: "two"}))
	if cmd == nil || !m.al.flushing {
		t.Fatal("the second should be held, with a tick to flush it")
	}
	m.update(flushMsg{})
	if m.al.flushing {
		t.Fatal("flushed")
	}
}
