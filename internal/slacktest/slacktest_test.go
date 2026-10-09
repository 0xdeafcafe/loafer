package slacktest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// The fake answers as internal/slack decodes, pages history as Slack does,
// and pushes what changes down the socket.
func TestServer(t *testing.T) {
	srv := New()
	defer srv.Close()
	c := srv.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if me, err := c.AuthTest(ctx); err != nil || me.UserID != Self || me.Team != "Crumb & Co" {
		t.Fatalf("auth.test: %+v %v", me, err)
	}
	b, err := c.UserBoot(ctx)
	if err != nil || b.Self.ID != Self || len(b.Channels) != 6 || len(b.IMs) != 3 {
		t.Fatalf("userBoot: %d channels, %d ims, %v", len(b.Channels), len(b.IMs), err)
	}
	n, err := c.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	unread, mentions := 0, 0
	for _, list := range [][]slack.Snapshot{n.Channels, n.MPIMs, n.IMs} {
		for _, x := range list {
			if x.HasUnreads {
				unread++
			}
			mentions += x.MentionCount
		}
	}
	if unread != 3 || mentions != 2 {
		t.Fatalf("counts: %d unread, %d mentions", unread, mentions)
	}

	page, more, err := c.History(ctx, Random, "", 100)
	if err != nil || len(page) != 100 || !more || page[0].TS < page[99].TS {
		t.Fatalf("history: %d, more %v, %v", len(page), more, err)
	}
	rest, more, err := c.History(ctx, Random, page[99].TS, 100)
	if err != nil || len(rest) != 20 || more || rest[0].TS >= page[99].TS {
		t.Fatalf("older: %d, more %v, %v", len(rest), more, err)
	}
	dev, _, _ := c.History(ctx, Dev, "", 10)
	if p := dev[2]; p.ReplyCount != 3 || len(p.ReplyUsers) != 3 || p.Edited != nil {
		t.Fatalf("thread parent: %+v", p)
	}
	if e := dev[3]; e.Edited == nil {
		t.Fatalf("edited: %+v", e)
	}

	// Posting echoes down the socket, as Slack's does.
	got := make(chan slack.Event, 8)
	go func() {
		_ = c.Listen(ctx, "", func(ev slack.Event) error {
			got <- ev
			return nil
		})
	}()
	if ev := <-got; ev.Type != "hello" {
		t.Fatalf("first event %q", ev.Type)
	}
	m, err := c.Post(ctx, General, "hi", "")
	if err != nil || m.User != Self {
		t.Fatalf("post: %+v %v", m, err)
	}
	var echo struct {
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	}
	if ev := <-got; ev.Type != "message" || jsonx.Unmarshal(ev.Raw, &echo) != nil || echo.Channel != General || echo.TS != m.TS {
		t.Fatalf("echo: %s", ev.Raw)
	}
	if err := c.Update(ctx, General, m.TS, "hello"); err != nil {
		t.Fatal(err)
	}
	if ev := <-got; ev.Subtype != "message_changed" {
		t.Fatalf("edit echo: %s", ev.Raw)
	}
	var notYours *slack.Error
	if err := c.Delete(ctx, General, srv.Messages(General)[0].TS); !errors.As(err, &notYours) || notYours.Code != "cant_delete_message" {
		t.Fatalf("deleting someone else's: %v", err)
	}
	if err := c.Delete(ctx, General, m.TS); err != nil {
		t.Fatal(err)
	}
	if ev := <-got; ev.Subtype != "message_deleted" {
		t.Fatalf("delete echo: %s", ev.Raw)
	}

	calls := srv.Calls()
	if last := calls[len(calls)-1]; last.Method != "chat.delete" || last.Form.Get("ts") != m.TS || last.Form.Has("token") {
		t.Fatalf("logged call: %+v", last)
	}
}
