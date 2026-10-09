package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/loafer/internal/store"
)

// e2e runs a Model against slacktest's fake Slack as bubbletea would:
// commands run on goroutines of their own and what they return comes back
// through Update, one at a time. Nothing sleeps; waiting is for the next
// message, which store changes bring through the model's own wait on
// Changed.
type e2e struct {
	t    *testing.T
	m    *Model
	srv  *slacktest.Server
	ctx  context.Context
	msgs chan tea.Msg
	sent int    // sentMsgs handled: posts, edits and deletes Slack has answered
	text string // the last frame, plain
}

func newE2E(t *testing.T) *e2e {
	srv := slacktest.New()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); srv.Close() })
	d := &e2e{t: t, srv: srv, ctx: ctx, msgs: make(chan tea.Msg, 64)}
	d.m = New(ctx, store.New(), srv.Client())
	d.m.w, d.m.h = 120, 60
	d.run(d.m.Init())
	d.until("boot and the socket", func() bool { return d.has("● live") && d.m.open != "" })
	return d
}

func (d *e2e) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				d.run(c)
			}
			return
		}
		if msg != nil {
			select {
			case d.msgs <- msg:
			case <-d.ctx.Done():
			}
		}
	}()
}

func (d *e2e) update(msg tea.Msg) {
	if _, ok := msg.(sentMsg); ok {
		d.sent++
	}
	_, cmd := d.m.Update(msg)
	d.run(cmd)
	d.text = strings.Join(plainFrame(d.m.render()), "\n")
}

func (d *e2e) press(keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		d.update(k)
	}
}

func (d *e2e) typed(s string) {
	for _, c := range s {
		d.press(r(c))
	}
}

func (d *e2e) has(s string) bool { return strings.Contains(d.text, s) }

// until handles what comes in until ok, or fails after a while.
func (d *e2e) until(what string, ok func() bool) {
	d.t.Helper()
	d.text = strings.Join(plainFrame(d.m.render()), "\n")
	deadline := time.After(3 * time.Second)
	for !ok() {
		select {
		case msg := <-d.msgs:
			d.update(msg)
		case <-deadline:
			d.t.Fatalf("waited for %s:\n%s", what, d.text)
		}
	}
}

// jump opens a conversation through ctrl+k, leaving focus on the composer.
func (d *e2e) jump(query, conv string) {
	d.t.Helper()
	d.press(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	d.typed(query)
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.m.open != conv {
		d.t.Fatalf("jumped to %q for %q", d.m.open, query)
	}
}

func (d *e2e) unread(conv string) (unread bool) {
	d.m.st.Read(func(v store.View) { unread = v.Conv(conv).Unread })
	return unread
}

func TestE2ESidebar(t *testing.T) {
	d := newE2E(t)
	for _, want := range []string{"Crumb & Co", "▾ Starred", "▾ 🍞 Team", "▾ Channels", "▾ Direct messages", "▾ Apps",
		"# general", "⊡ design", "⁂ priya, jo", "◇ deploybot", "3 unread", "@ 2 mentions"} {
		if !d.has(want) {
			t.Errorf("sidebar lacks %q", want)
		}
	}
	if t.Failed() {
		t.Fatal("\n" + d.text)
	}
}

func TestE2EOpen(t *testing.T) {
	d := newE2E(t)
	d.jump("dev", slacktest.Dev)
	d.until("#dev's messages", func() bool { return d.has("deployed loafer@4f2a9c1") })
	for _, want := range []string{
		"# dev", "the cache rewrite is up for review",
		"func (s *Store) touch(conv string) {", // a go fence
		"cold start", "410ms",                  // a table
		"why not an LRU from the stdlib? (edited)",
		"3 replies", "deploybot", "3 services, none failed", // a bot, its attachment
	} {
		if !d.has(want) {
			t.Errorf("#dev lacks %q", want)
		}
	}
	if t.Failed() {
		t.Fatal("\n" + d.text)
	}

	// #general has a mention waiting; opening it reads it, here and at Slack.
	d.jump("general", slacktest.General)
	d.until("#general read", func() bool { return d.has("projector cable") && !d.unread(slacktest.General) && d.has("@ 1 mentions") })
	d.until("the mark echoed", func() bool {
		for _, c := range d.srv.Calls() {
			if c.Method == "conversations.mark" && c.Form.Get("channel") == slacktest.General {
				return true
			}
		}
		return false
	})
}

func TestE2ELive(t *testing.T) {
	d := newE2E(t)
	d.jump("dev", slacktest.Dev)

	// Somewhere not open: it goes unread.
	d.srv.Post(slacktest.Alerts, slacktest.Tomas, "is staging down for anyone else", "")
	d.until("#alerts unread", func() bool { return d.unread(slacktest.Alerts) && d.has("4 unread") })

	// Where you're looking: who's typing, then what they wrote, which
	// stays read.
	d.srv.Typing(slacktest.Dev, slacktest.Priya)
	d.until("priya typing", func() bool { return d.has("priya is typing…") })
	m := d.srv.Post(slacktest.Dev, slacktest.Priya, "fresh out of the oven", "")
	d.until("the message in #dev", func() bool {
		return d.has("fresh out of the oven") && !d.has("is typing") && !d.unread(slacktest.Dev)
	})

	// Someone else's edit and delete arrive too.
	d.srv.Edit(slacktest.Dev, m.TS, "fresh out of the oven, still warm")
	d.until("their edit", func() bool { return d.has("still warm (edited)") })
	d.srv.Delete(slacktest.Dev, m.TS)
	d.until("their delete", func() bool { return !d.has("fresh out of the oven") })
}

func TestE2ESendEditDelete(t *testing.T) {
	d := newE2E(t)
	d.jump("general", slacktest.General)
	d.until("#general", func() bool { return d.has("projector cable") })

	d.typed("cable's in my bag")
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.until("the post", func() bool { return d.sent == 1 && d.has("cable's in my bag") })
	// Something said after it comes down the socket after its echo, so
	// once it's here the echo's been applied.
	d.srv.Post(slacktest.General, slacktest.Tomas, "legend", "")
	d.until("the reply", func() bool { return d.has("legend") })
	if n := strings.Count(d.text, "cable's in my bag"); n != 1 {
		t.Fatalf("the post and its echo should show once, not %d times:\n%s", n, d.text)
	}

	// ↑ in the empty composer edits your last message.
	d.press(up)
	if d.m.editing == "" {
		t.Fatalf("not editing:\n%s", d.text)
	}
	for range d.m.input {
		d.press(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	d.typed("cable's on your desk")
	d.press(tea.KeyPressMsg{Code: tea.KeyEnter})
	d.until("the edit", func() bool { return d.sent == 2 && d.has("cable's on your desk (edited)") && !d.has("in my bag") })

	// tab twice comes round to the messages, on the newest (tomás's); k
	// is yours, and d twice bins it.
	d.press(tab, tab, r('k'), r('d'), r('d'))
	d.until("the delete", func() bool { return d.sent == 3 && !d.has("on your desk") })
	for _, m := range d.srv.Messages(slacktest.General) {
		if strings.HasPrefix(m.Text, "cable's") {
			t.Fatalf("Slack still has it: %+v", m)
		}
	}
}
