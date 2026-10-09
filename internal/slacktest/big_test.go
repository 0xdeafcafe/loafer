package slacktest

import (
	"context"
	"testing"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// The big workspace boots at size, pages people and history, and its
// stream sends a bit of everything.
func TestBig(t *testing.T) {
	if testing.Short() {
		t.Skip("big")
	}
	srv := NewBig()
	defer srv.Close()
	c := srv.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	b, err := c.UserBoot(ctx)
	if err != nil || len(b.Channels) != 6+BigJoined+1 || len(b.IMs) != 3+BigDMs {
		t.Fatalf("userBoot: %d channels, %d ims, %v", len(b.Channels), len(b.IMs), err)
	}
	people, pages := 0, 0
	if err := c.Users(ctx, func(us []slack.User) { people += len(us); pages++ }); err != nil || people != 5+BigPeople || pages < 2 {
		t.Fatalf("users: %d in %d pages, %v", people, pages, err)
	}
	page, more, err := c.History(ctx, Big, "", 100)
	if err != nil || len(page) != 100 || !more {
		t.Fatalf("history: %d, more %v, %v", len(page), more, err)
	}
	if n := len(srv.Messages(Big)); n != BigHistory {
		t.Fatalf("#firehose holds %d", n)
	}
	ss, err := c.Sections(ctx)
	if err != nil || len(ss) != 7 {
		t.Fatalf("sections: %d, %v", len(ss), err)
	}

	seen := map[string]int{}
	stream, stop := context.WithCancel(ctx)
	defer stop()
	err = c.Listen(ctx, "", func(ev slack.Event) error {
		if ev.Type == "hello" {
			go srv.Stream(stream, 400)
			return nil
		}
		seen[ev.Type]++
		if len(seen) == 5 {
			return context.Canceled
		}
		return nil
	})
	if len(seen) != 5 {
		t.Fatalf("stream sent %v, %v", seen, err)
	}
}
