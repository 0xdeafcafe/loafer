package ui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/loafer/internal/store"
)

// Every action's key is one bubbletea would write, so pressing it for an
// action is pressing it.
func TestActionKeys(t *testing.T) {
	for _, a := range allActions {
		if got := keyPress(a.key).String(); got != a.key {
			t.Errorf("%s: %q reads back as %q", a.label, a.key, got)
		}
	}
}

var ctrlK = tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}

// `>` in ctrl+k lists the actions, narrows them as you type, and enter
// does one: on the message under the cursor, on the conversation, or
// anywhere.
func TestE2EBarActions(t *testing.T) {
	d := newE2E(t)
	d.oldestInDev()

	d.press(ctrlK)
	if !d.has("type > for actions, # to browse channels") {
		t.Fatalf("the empty bar should say what > and # do:\n%s", d.text)
	}
	d.typed(">")
	for _, want := range []string{"▸ Actions", "▾ Message", "react", "save for later", "open in Slack", "▾ Conversation", "mute", "▾ Everywhere", "search messages", "ctrl+f"} {
		if !d.has(want) {
			t.Errorf("> lacks %q", want)
		}
	}
	if t.Failed() {
		t.Fatal("\n" + d.text)
	}
	if testing.Verbose() {
		t.Log("\n" + d.text)
	}

	// The message's: pin it.
	d.typed("pin")
	if it := d.m.bar.items[0]; it.act == nil || it.label != "pin" {
		t.Fatalf(">pin's first is %+v", it)
	}
	d.press(enter)
	d.until("the pin", func() bool { return slices.Contains(d.srv.Messages(slacktest.Dev)[0].PinnedTo, slacktest.Dev) })

	// The conversation's: mute #dev.
	d.press(ctrlK)
	d.typed(">mute")
	d.press(enter)
	d.until("#dev muted", func() bool {
		muted := false
		d.m.st.Read(func(v store.View) { muted = v.Conv(slacktest.Dev).Muted })
		return muted
	})

	// Anywhere's: its key, pressed.
	d.press(ctrlK)
	d.typed(">search mess")
	d.press(enter)
	d.until("search open", func() bool { return d.m.find.on })
	d.press(esc)

	// From the composer there's no message: its actions are faint, and
	// doing one says why.
	d.m.setFocus(onCompose)
	d.press(ctrlK)
	d.typed(">react")
	if it := d.m.bar.items[0]; it.label != "react" || !it.off {
		t.Fatalf("react without a message: %+v", it)
	}
	d.press(enter)
	if !d.has("pick a message first") || d.m.emo.pick.on {
		t.Fatalf("react without a message should say so:\n%s", d.text)
	}
}
