package store

import (
	"cmp"
	"context"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// Activity is something that happened to you: a mention, a reply in a
// thread you're in, a reaction to your message, an app's message.
type Activity struct {
	Key, Type, FeedTS  string
	Unread             bool
	Conv, TS, ThreadTS string        // the message it's about
	Reactor, Reaction  string        // for a reaction: who, and which
	Msg                slack.Message // the message, once known; Msg.TS is "" until then
}

func (s *Store) fetchActivity(ctx context.Context, c *slack.Client) error {
	items, err := c.Activity(ctx)
	if err != nil {
		return err
	}
	list := make([]Activity, 0, len(items))
	for _, it := range items {
		a := Activity{Key: it.Key, Type: it.Item.Type, FeedTS: it.FeedTS, Unread: it.IsUnread}
		n := it.Item.Message
		if b := it.Item.Bundle; b != nil {
			if t := b.Payload.Thread; t != nil {
				a.Conv, a.TS, a.ThreadTS = t.Channel, t.Latest, t.ThreadTS
			}
			n = cmp.Or(n, b.Payload.Message)
		}
		if n != nil {
			a.Conv, a.TS, a.ThreadTS = cmp.Or(n.Channel, a.Conv), cmp.Or(n.TS, a.TS), cmp.Or(n.ThreadTS, a.ThreadTS)
			if n.Text != "" {
				a.Msg = slack.Message{TS: a.TS, User: cmp.Or(n.Author, n.User), Text: n.Text, ThreadTS: a.ThreadTS}
			}
		}
		if r := it.Item.Reaction; r != nil {
			a.Reactor, a.Reaction = r.User, r.Name
		}
		list = append(list, a)
	}
	var refs []ref
	s.update(func() {
		// What's known already isn't fetched again.
		known := map[ref]slack.Message{}
		for _, a := range s.tabs.activity {
			known[ref{a.Conv, a.TS, a.ThreadTS}] = a.Msg
		}
		for i := range list {
			a := &list[i]
			r := ref{a.Conv, a.TS, a.ThreadTS}
			if a.Msg.TS == "" {
				a.Msg = known[r]
			}
			if w, j := s.held(a.Conv, a.TS); a.Msg.TS == "" && j >= 0 {
				a.Msg = w.Msgs[j]
			}
			if a.Msg.TS == "" && a.Conv != "" && a.TS != "" {
				refs = append(refs, r)
			}
		}
		s.tabs.activity, s.tabs.loaded[ActivityList] = list, true
	})
	s.fill(refs, func(r ref) (slack.Message, error) { return c.Message(ctx, r.conv, r.ts, r.thread) }, func(r ref, m slack.Message) {
		for i := range s.tabs.activity {
			if a := &s.tabs.activity[i]; (ref{a.Conv, a.TS, a.ThreadTS}) == r {
				a.Msg = m
			}
		}
	})
	return nil
}

// Activity is the Activity tab's list, newest first.
func (v View) Activity() []Activity { return v.s.tabs.activity }

// ReadActivity marks the activity at key read, as activity.markRead will.
func (s *Store) ReadActivity(key string) {
	s.update(func() {
		for i := range s.tabs.activity {
			if s.tabs.activity[i].Key == key {
				s.tabs.activity[i].Unread = false
			}
		}
	})
}
