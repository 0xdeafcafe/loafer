package store

import (
	"context"
	"slices"
	"testing"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/slacktest"
)

// After the socket's been down, the conversation and the thread looked at
// last are fetched again; other conversations held go stale, so what
// arrives live isn't put after a hole, and opening one fetches it again.
func TestCatchUpGap(t *testing.T) {
	srv := slacktest.New()
	defer srv.Close()
	c, ctx := srv.Client(), context.Background()
	s := New()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.Boot(ctx, c))
	must(s.Open(ctx, c, slacktest.General))
	must(s.Open(ctx, c, slacktest.Dev))
	i := slices.IndexFunc(srv.Messages(slacktest.Dev), func(m slack.Message) bool { return m.ReplyCount > 0 })
	parent := srv.Messages(slacktest.Dev)[i].TS
	must(s.OpenThread(ctx, c, slacktest.Dev, parent))

	// Said while it was down, with no socket to hear it.
	srv.Post(slacktest.General, slacktest.Tomas, "missed in general", "")
	srv.Post(slacktest.Dev, slacktest.Priya, "missed in dev", "")
	srv.Post(slacktest.Dev, slacktest.Jo, "missed in the thread", parent)
	must(s.catchUp(ctx, c))

	has := func(msgs []slack.Message, text string) bool {
		return slices.ContainsFunc(msgs, func(m slack.Message) bool { return m.Text == text })
	}
	s.Read(func(v View) {
		if !has(v.Window(slacktest.Dev).Msgs, "missed in dev") || v.Window(slacktest.Dev).stale {
			t.Error("the conversation looked at last wasn't caught up")
		}
		if !has(v.Thread(slacktest.Dev, parent).Msgs, "missed in the thread") {
			t.Error("the thread looked at last wasn't caught up")
		}
		if w := v.Window(slacktest.General); has(w.Msgs, "missed in general") || !w.stale {
			t.Error("another conversation should be left stale")
		}
	})

	s.Add(slacktest.General, slack.Message{TS: "9999999999.000000", User: slacktest.Tomas, Text: "live"})
	s.Read(func(v View) {
		if has(v.Window(slacktest.General).Msgs, "live") {
			t.Error("a live message went after the hole")
		}
	})
	must(s.Open(ctx, c, slacktest.General))
	s.Read(func(v View) {
		if w := v.Window(slacktest.General); !has(w.Msgs, "missed in general") || w.stale {
			t.Error("opening it again should fetch it afresh")
		}
	})
}
