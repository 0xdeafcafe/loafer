package store

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// Live keeps the websocket open and applies what it brings, reconnecting
// when it drops and catching up on what was missed while it was down. It
// returns when ctx ends, or with the error once Slack has signed it out.
func (s *Store) Live(ctx context.Context, c *slack.Client) error {
	to, first := "", true
	for wait := time.Duration(0); ; wait = min(max(2*wait, time.Second), 30*time.Second) {
		s.setLink("connecting")
		hello := false
		err := c.Listen(ctx, to, func(ev slack.Event) error {
			switch ev.Type {
			case "hello":
				hello = true
				// The first hello after a good boot has nothing to catch up on.
				if !first || !s.booted.Load() {
					if err := s.catchUp(ctx, c); slack.SignedOut(err) {
						return err
					}
				}
				first = false
				s.setLink("live")
			case "reconnect_url":
				var e struct {
					URL string `json:"url"`
				}
				if jsonx.Unmarshal(ev.Raw, &e) == nil {
					to = e.URL
				}
			default:
				s.Apply(ev)
			}
			return nil
		})
		if ctx.Err() != nil {
			return nil
		}
		if slack.SignedOut(err) {
			return err
		}
		slog.Warn("ws", "err", err.Error(), "hello", hello)
		if hello {
			wait = 0 // it was up: try again at once, then back off
		} else {
			to = "" // a reconnect_url that won't take goes back to the gateway
			if _, err := c.AuthTest(ctx); slack.SignedOut(err) {
				return err
			}
		}
		s.setLink("offline")
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// catchUp fetches what changed while the socket was down: everything, if
// the boot never got through, else the counts and the conversation looked
// at last (others refresh as they're opened).
func (s *Store) catchUp(ctx context.Context, c *slack.Client) error {
	if !s.booted.Load() {
		return s.Boot(ctx, c)
	}
	n, err := c.Counts(ctx)
	if err != nil {
		return err
	}
	s.ApplyCounts(n)
	last := ""
	s.Read(func(v View) {
		var at uint64
		for id, w := range v.s.windows {
			if w.used > at {
				last, at = id, w.used
			}
		}
	})
	if last == "" {
		return nil
	}
	return s.Refresh(ctx, c, last)
}

func (s *Store) setLink(l string) {
	s.mu.RLock()
	same := s.link == l
	s.mu.RUnlock()
	if !same {
		s.update(func() { s.link = l })
	}
}

// Apply takes one websocket event. Those it doesn't know are dropped.
func (s *Store) Apply(ev slack.Event) {
	switch ev.Type {
	case "message":
		var e struct {
			Channel   string        `json:"channel"`
			Message   slack.Message `json:"message"`
			DeletedTS string        `json:"deleted_ts"`
		}
		if jsonx.Unmarshal(ev.Raw, &e) != nil {
			return
		}
		switch ev.Subtype {
		case "message_changed", "message_replied":
			s.update(func() {
				if w, i := s.held(e.Channel, e.Message.TS); i >= 0 {
					w.Msgs[i] = e.Message
				}
				s.threadSet(e.Channel, e.Message)
			})
		case "message_deleted":
			s.Remove(e.Channel, e.DeletedTS)
		default:
			var m slack.Message
			if jsonx.Unmarshal(ev.Raw, &m) == nil {
				s.Add(e.Channel, m)
				s.alert(e.Channel, m)
			}
		}

	case "reaction_added", "reaction_removed":
		var e struct {
			User     string `json:"user"`
			Reaction string `json:"reaction"`
			Item     struct {
				Channel string `json:"channel"`
				TS      string `json:"ts"`
			} `json:"item"`
		}
		if jsonx.Unmarshal(ev.Raw, &e) != nil {
			return
		}
		s.update(func() {
			if w, i := s.held(e.Item.Channel, e.Item.TS); i >= 0 {
				m := &w.Msgs[i]
				m.Reactions = react(m.Reactions, e.Reaction, e.User, ev.Type == "reaction_added")
			}
			s.inThreads(e.Item.Channel, e.Item.TS, func(m *slack.Message) {
				m.Reactions = react(m.Reactions, e.Reaction, e.User, ev.Type == "reaction_added")
			})
		})

	case "channel_marked", "group_marked", "im_marked", "mpim_marked":
		var e struct {
			Channel  string `json:"channel"`
			TS       string `json:"ts"`
			Mentions *int   `json:"mention_count"`
		}
		if jsonx.Unmarshal(ev.Raw, &e) != nil {
			return
		}
		s.update(func() {
			if c := s.convs[e.Channel]; c != nil {
				c.LastRead, c.Unread = e.TS, c.Latest > e.TS
				if e.Mentions != nil {
					c.Mentions = *e.Mentions
				} else if !c.Unread {
					c.Mentions = 0
				}
			}
		})

	case "channel_joined", "group_joined", "im_created", "channel_rename", "group_rename":
		var e struct {
			Channel slack.Conversation `json:"channel"`
		}
		if jsonx.Unmarshal(ev.Raw, &e) != nil || e.Channel.ID == "" {
			return
		}
		s.update(func() {
			if c := s.convs[e.Channel.ID]; c != nil && strings.HasSuffix(ev.Type, "_rename") {
				c.Name = e.Channel.Name // a rename carries only id and name
				return
			}
			e.Channel.IsMember = true
			s.putConv(e.Channel)
		})

	case "channel_left", "group_left", "im_close", "mpim_close", "channel_archive", "group_archive", "channel_deleted":
		var e struct {
			Channel string `json:"channel"`
		}
		if jsonx.Unmarshal(ev.Raw, &e) == nil {
			s.update(func() { delete(s.convs, e.Channel) })
		}

	case "pin_added", "pin_removed":
		var e struct {
			Channel string `json:"channel_id"`
			Item    struct {
				Message struct {
					TS string `json:"ts"`
				} `json:"message"`
			} `json:"item"`
		}
		if jsonx.Unmarshal(ev.Raw, &e) == nil && e.Item.Message.TS != "" {
			s.SetPinned(e.Channel, e.Item.Message.TS, ev.Type == "pin_added")
		}

	case "pref_change", "dnd_updated", "user_typing":
		s.applyAlert(ev)

	case "user_change":
		var e struct {
			User slack.User `json:"user"`
		}
		if jsonx.Unmarshal(ev.Raw, &e) == nil && e.User.ID != "" {
			s.ApplyPeople([]slack.User{e.User})
		}

	case "activity", "activity_views_updated", "activity_clear_all_completed",
		"saved_added", "saved_updated", "saved_deleted", "saved_clear", "saved_due":
		s.applyTabs(ev)
	}
}

// Add takes a new message in conv, from the websocket or from sending it,
// whichever comes first: the second replaces the first.
func (s *Store) Add(conv string, m slack.Message) {
	s.update(func() {
		s.threadAdd(conv, m)
		// A reply goes to its thread, if that's held, and its parent here
		// counts it. message_replied brings the parent's own count after.
		if m.ThreadTS != "" && m.ThreadTS != m.TS && m.Subtype != "thread_broadcast" {
			if w, i := s.held(conv, m.ThreadTS); i >= 0 && w.Msgs[i].LatestReply < m.TS {
				p := &w.Msgs[i]
				p.ReplyCount++
				p.LatestReply = m.TS
				if !slices.Contains(p.ReplyUsers, m.User) {
					p.ReplyUsers = append(p.ReplyUsers, m.User)
				}
			}
			return
		}
		if w := s.windows[conv]; w != nil && !w.Newer { // a window back in time doesn't reach it
			i, found := slices.BinarySearchFunc(w.Msgs, m.TS, func(x slack.Message, ts string) int { return strings.Compare(x.TS, ts) })
			if found {
				w.Msgs[i] = m
			} else {
				w.Msgs = slices.Insert(w.Msgs, i, m)
			}
		}
		c := s.convs[conv]
		if c == nil && strings.HasPrefix(conv, "D") && m.User != "" && m.User != s.self {
			// A DM that wasn't open: someone's written for the first time in a while.
			c = &Conv{ID: conv, Kind: IM, User: m.User}
			s.convs[conv] = c
		}
		if c == nil {
			return
		}
		if m.TS > c.Latest {
			c.Latest = m.TS
		}
		if c.Kind == IM || c.Kind == MPIM {
			s.tabs.sawDM(conv, m)
		}
		if m.User != s.self && m.TS > c.LastRead {
			c.Unread = true
			if c.Kind == IM || c.Kind == MPIM || strings.Contains(m.Text, "<@"+s.self+">") {
				c.Mentions++
			}
		}
	})
}

// react adds or takes away user's name reaction, once however often it's told.
func react(rs []slack.Reaction, name, user string, add bool) []slack.Reaction {
	i := slices.IndexFunc(rs, func(r slack.Reaction) bool { return r.Name == name })
	switch {
	case add && i < 0:
		return append(rs, slack.Reaction{Name: name, Count: 1, Users: []string{user}})
	case add && !slices.Contains(rs[i].Users, user):
		rs[i].Count++
		rs[i].Users = append(rs[i].Users, user)
	case !add && i >= 0 && slices.Contains(rs[i].Users, user):
		rs[i].Count--
		rs[i].Users = slices.DeleteFunc(rs[i].Users, func(u string) bool { return u == user })
		if rs[i].Count <= 0 {
			return slices.Delete(rs, i, i+1)
		}
	}
	return rs
}
