package main

import (
	"context"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/loafer/internal/ui"
)

// demo opens loafer on slacktest's made-up workspace, served in-process,
// with some chatter in #dev, where it opens, so it can be tried and screenshotted
// without signing in to anything. It reads no sign-in and writes no cache.
func demo() error {
	srv := slacktest.New()
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go chatter(ctx, srv)
	m := ui.New(ctx, store.New(), srv.Client())
	if os.Getenv("LOAFER_WELCOME") != "" {
		m.Welcome(true)
		slowBoot(srv)
	}
	_, err := tea.NewProgram(m).Run()
	return err
}

// slowBoot has the made-up Slack answer boot's calls over a few seconds,
// as a big workspace's would, so the welcome can be watched filling in.
func slowBoot(srv *slacktest.Server) {
	for method, after := range map[string]time.Duration{
		"client.userBoot":            900 * time.Millisecond,
		"users.channelSections.list": 1600 * time.Millisecond,
		"client.counts":              2200 * time.Millisecond,
		"emoji.list":                 3000 * time.Millisecond,
		"users.list":                 4200 * time.Millisecond,
	} {
		time.AfterFunc(after, srv.Hold(method))
	}
}

// What #dev says, round and round. None of it names you, so none of
// it notifies.
var script = []struct{ who, text string }{
	{slacktest.Jo, "is anyone else's build taking forever"},
	{slacktest.Priya, "mine's fine. did you clear the cache?"},
	{slacktest.Jo, "...it's fine now"},
	{slacktest.Tomas, "the good sourdough place has a queue round the block"},
	{slacktest.Priya, "worth it"},
	{slacktest.Tomas, "for `select * from loaves` you mean"},
	{slacktest.Jo, "standup in 5, *in person* today"},
	{slacktest.Priya, "on my way :wave:"},
}

// chatter has someone type for a couple of seconds, then say the next
// line, every few seconds until ctx ends.
func chatter(ctx context.Context, srv *slacktest.Server) {
	for i := 0; ; i++ {
		line := script[i%len(script)]
		for _, step := range []func(){
			func() { srv.Typing(slacktest.Dev, line.who) },
			func() { srv.Post(slacktest.Dev, line.who, line.text, "") },
		} {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2500 * time.Millisecond):
			}
			step()
		}
	}
}
