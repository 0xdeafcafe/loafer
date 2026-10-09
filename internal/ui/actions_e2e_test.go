package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/loafer/internal/store"
)

// calls is how many times Slack has been asked for method.
func (d *e2e) calls(method string) (n int) {
	for _, c := range d.srv.Calls() {
		if c.Method == method {
			n++
		}
	}
	return n
}

// toMessages tabs round to the conversation's messages.
func (d *e2e) toMessages() {
	for range 4 {
		if d.m.focus == onMsgs {
			return
		}
		d.press(tab)
	}
	d.t.Fatalf("tab never reached the messages:\n%s", d.text)
}

// oldestInDev opens #dev with the cursor on its oldest message, Jo's,
// which has a link in it.
func (d *e2e) oldestInDev() slack.Message {
	d.jump("dev", slacktest.Dev)
	d.until("#dev's messages", func() bool { return d.has("deployed loafer@4f2a9c1") })
	d.toMessages()
	d.press(r('g'))
	if d.m.focus != onMsgs || d.m.sel == "" {
		d.t.Fatalf("not on a message:\n%s", d.text)
	}
	return d.srv.Messages(slacktest.Dev)[0]
}

func TestE2EMenu(t *testing.T) {
	d := newE2E(t)
	jo := d.oldestInDev()

	d.press(r('.'))
	for _, want := range []string{"Actions", "react", "reply in thread", "ask Claude", "save for later", "remind me ▸", "pin", "mark unread from here", "copy link", "copy text", "open link"} {
		if !d.has(want) {
			t.Errorf("the menu lacks %q:\n%s", want, d.text)
		}
	}
	if d.has("delete") {
		t.Error("Jo's message isn't yours to delete")
	}
	if t.Failed() {
		t.FailNow()
	}
	d.press(esc)
	if d.m.acts.menu.on || d.has("Actions") || d.m.sel != jo.TS {
		t.Fatalf("esc should close the menu and nothing else:\n%s", d.text)
	}

	// Pin through the menu's key, then unpin by the plain one.
	d.press(r('.'), r('p'))
	d.until("the pin", func() bool { return d.has("⚑ pinned") })
	d.until("Slack's word", func() bool { return slices.Contains(d.srv.Messages(slacktest.Dev)[0].PinnedTo, slacktest.Dev) })
	d.until("the pin event applied", func() bool { return d.calls("pins.add") == 1 })
	d.press(r('.'))
	if !d.has("unpin") {
		t.Fatalf("the menu should offer to unpin:\n%s", d.text)
	}
	d.press(esc, r('p'))
	d.until("the unpin", func() bool { return !d.has("⚑ pinned") && len(d.srv.Messages(slacktest.Dev)[0].PinnedTo) == 0 })
}

func TestE2ESaveAndRemind(t *testing.T) {
	d := newE2E(t)
	jo := d.oldestInDev()

	// L fetches Later first, to know whether it's saved, then saves.
	d.press(r('L'))
	d.until("saved", func() bool { return len(d.srv.Saved()) == 1 })
	if got := d.srv.Saved()[0]; got.ID != slacktest.Dev || got.TS != jo.TS || got.DateDue != 0 {
		t.Fatalf("saved %+v", got)
	}
	d.until("on the list here", func() bool {
		saved := false
		d.m.st.Read(func(v store.View) { saved = v.IsSaved(slacktest.Dev, jo.TS) })
		return saved
	})
	d.press(r('.'))
	if !d.has("remove from later") {
		t.Fatalf("the menu should offer to take it off:\n%s", d.text)
	}
	d.press(esc, r('L'))
	d.until("unsaved", func() bool { return len(d.srv.Saved()) == 0 && d.calls("saved.delete") == 1 })

	// Remind me: the chooser, then a time. It's saved with a due time, an
	// hour off.
	d.press(r('m'))
	for _, want := range []string{"Remind me", "in 20 minutes", "in 1 hour", "in 3 hours", "tomorrow at 9", "next week"} {
		if !d.has(want) {
			t.Errorf("the chooser lacks %q:\n%s", want, d.text)
		}
	}
	d.press(r('2'))
	d.until("a reminder", func() bool { return len(d.srv.Saved()) == 1 })
	if due := time.Unix(d.srv.Saved()[0].DateDue, 0); time.Until(due) < 59*time.Minute || time.Until(due) > time.Hour {
		t.Fatalf("due %v, not an hour off", due)
	}

	// Another time on one already saved moves its due time.
	d.press(r('m'), r('3'))
	d.until("moved", func() bool {
		return len(d.srv.Saved()) == 1 && time.Until(time.Unix(d.srv.Saved()[0].DateDue, 0)) > 2*time.Hour
	})
}

func TestE2EMarkUnread(t *testing.T) {
	d := newE2E(t)
	d.oldestInDev()
	msgs := d.srv.Messages(slacktest.Dev)
	d.press(r('j'), r('j')) // your own, the third
	d.press(r('.'))
	if !d.has("edit") || !d.has("delete") {
		t.Fatalf("your own message can be edited and deleted:\n%s", d.text)
	}
	d.press(r('u'))
	if d.m.acts.menu.on {
		t.Fatal("u should have closed the menu")
	}
	if !d.unread(slacktest.Dev) {
		t.Fatalf("not unread here:\n%s", d.text)
	}
	d.until("Slack told", func() bool { return d.calls("conversations.mark") >= 1 && d.has("marked unread from here") })
	var marked string
	for _, c := range d.srv.Calls() {
		if c.Method == "conversations.mark" && c.Form.Get("channel") == slacktest.Dev {
			marked = c.Form.Get("ts")
		}
	}
	if marked != msgs[1].TS {
		t.Fatalf("marked at %q, want the message before: %q", marked, msgs[1].TS)
	}

	// Watching it come in isn't reading it, now.
	d.srv.Post(slacktest.Dev, slacktest.Priya, "late to the party", "")
	d.until("the new message", func() bool { return d.has("late to the party") })
	before := d.calls("conversations.mark")
	d.srv.Post(slacktest.Dev, slacktest.Priya, "and another", "")
	d.until("another", func() bool { return d.has("and another") })
	if !d.unread(slacktest.Dev) || d.calls("conversations.mark") != before {
		t.Fatalf("it was read again:\n%s", d.text)
	}
}

func TestE2EMenuInThread(t *testing.T) {
	d := newE2E(t)
	d.jump("dev", slacktest.Dev)
	d.until("#dev's messages", func() bool { return d.has("3 replies") })
	d.toMessages()
	d.press(r('k'), r('k'), r('t')) // tomás's, which has replies
	d.until("the replies", func() bool { return d.has("container/list") })
	d.press(shiftTab) // from the thread's box to its messages, on the newest reply
	if d.m.focus != onThread || d.m.th.sel == "" {
		t.Fatalf("not on a reply:\n%s", d.text)
	}
	reply := d.m.th.sel

	d.press(r('.'))
	if !d.has("Actions") || d.has("↩ reply in thread") || d.has("● mark unread") || !d.has("✗ delete") {
		t.Fatalf("a reply's menu is the conversation's less threading and unread, plus yours:\n%s", d.text)
	}
	d.press(r('p'))
	d.until("the reply pinned", func() bool { return d.has("⚑ pinned") && d.calls("pins.add") == 1 })
	for _, c := range d.srv.Calls() {
		if c.Method == "pins.add" && c.Form.Get("timestamp") != reply {
			t.Fatalf("pinned %v, not the reply %s", c, reply)
		}
	}
}
