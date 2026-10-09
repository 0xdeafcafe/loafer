// Package store is the one owner of a workspace's state: who's who, the
// conversations and their read state, the sidebar, and windows of recent
// messages for the conversations looked at lately. Network code applies
// what it learns; the UI reads through Read, holding a read lock only for
// as long as it looks. Every change bumps Version and wakes Changed, once,
// however many changes land before the UI next looks.
package store

import (
	"context"
	"encoding/json/jsontext"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// keepWindows is how many conversations keep messages in memory.
const keepWindows = 20

type Store struct {
	mu       sync.RWMutex
	self     string // your user id
	team     slack.Team
	people   map[string]*Person
	convs    map[string]*Conv
	preview  *Conv // a channel being read before joining (manage.go)
	sections []Section
	emoji    map[string]string
	windows  map[string]*Window
	threads  map[string]*Window // the threads looked at lately (thread.go)
	clock    uint64
	names    uint64 // goes up when people or emoji change, which drawn messages show
	link     string // the websocket: connecting, live or offline; "" before it's tried
	tabs     tabs   // the DMs, Activity and Later lists (tabs.go)
	folk     folk   // presence and user groups (presence.go, groups.go)
	md       modals // apps' modals (modal.go)

	booted atomic.Bool // a boot has got through
	al     alertState  // notification rules and typing (alert.go)

	version atomic.Uint64
	changed chan struct{}
	dirty   chan struct{} // wakes the cache writer
}

func New() *Store {
	return &Store{
		people:  map[string]*Person{},
		convs:   map[string]*Conv{},
		emoji:   map[string]string{},
		windows: map[string]*Window{},
		al:      newAlertState(),
		changed: make(chan struct{}, 1),
		dirty:   make(chan struct{}, 1),
	}
}

// Changed is signalled after changes; it holds at most one signal.
func (s *Store) Changed() <-chan struct{} { return s.changed }

// Version goes up with every change.
func (s *Store) Version() uint64 { return s.version.Load() }

func (s *Store) bump() {
	s.version.Add(1)
	for _, c := range []chan struct{}{s.changed, s.dirty} {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}

// update runs f under the write lock, then signals the change.
func (s *Store) update(f func()) {
	s.mu.Lock()
	f()
	s.mu.Unlock()
	s.bump()
}

// ApplyBoot takes client.userBoot: you, the team, and your conversations.
func (s *Store) ApplyBoot(b slack.UserBoot) {
	s.update(func() {
		s.self, s.team = b.Self.ID, b.Team
		p := person(b.Self)
		s.people[p.ID] = &p
		s.al.prefs.Load(b.Prefs, b.DND)
		for _, list := range [][]slack.Conversation{b.Channels, b.IMs} {
			for _, c := range list {
				s.putConv(c)
			}
		}
	})
}

func (s *Store) putConv(c slack.Conversation) {
	if c.IsArchived || (!c.IsMember && !c.IsIM && !c.IsMPIM) {
		delete(s.convs, c.ID)
		return
	}
	v := s.convs[c.ID]
	if v == nil {
		v = &Conv{ID: c.ID}
		s.convs[c.ID] = v
	}
	v.Name, v.User, v.Topic, v.Members = c.Name, c.User, c.Topic.Value, c.NumMembers
	v.Muted = s.al.prefs.Muted(c.ID)
	if s.preview != nil && s.preview.ID == c.ID {
		s.preview = nil // joined
	}
	switch {
	case c.IsIM:
		v.Kind = IM
	case c.IsMPIM:
		v.Kind, v.Name = MPIM, mpimName(c.Name)
	case c.IsPrivate || c.IsGroup:
		v.Kind = Private
	default:
		v.Kind = Channel
	}
	if c.LastRead != "" {
		v.LastRead = c.LastRead
	}
	if ts := latestTS(c.Latest); ts != "" {
		v.Latest = ts
	}
}

// latestTS reads "latest", which is a ts on some endpoints and a whole
// message on others.
func latestTS(v jsontext.Value) string {
	switch v.Kind() {
	case '"':
		var s string
		if jsonx.Unmarshal(v, &s) == nil {
			return s
		}
	case '{':
		var m struct {
			TS string `json:"ts"`
		}
		if jsonx.Unmarshal(v, &m) == nil {
			return m.TS
		}
	}
	return ""
}

// mpimName turns "mpdm-alex--rogerio--sergio-1" into "alex, rogerio,
// sergio"; the UI drops your own name when it has it.
func mpimName(n string) string {
	n = strings.TrimSuffix(strings.TrimPrefix(n, "mpdm-"), "-1")
	return strings.ReplaceAll(n, "--", ", ")
}

// ApplyCounts takes client.counts: unread and mention state.
func (s *Store) ApplyCounts(c slack.Counts) {
	s.update(func() {
		for _, list := range [][]slack.Snapshot{c.Channels, c.MPIMs, c.IMs} {
			for _, n := range list {
				if v := s.convs[n.ID]; v != nil {
					v.LastRead, v.Latest = n.LastRead, n.Latest
					v.Mentions, v.Unread = n.MentionCount, n.HasUnreads
				}
			}
		}
		s.tabs.counts(c)
	})
}

// ApplyPeople takes a page of users.list.
func (s *Store) ApplyPeople(us []slack.User) {
	s.update(func() {
		for _, u := range us {
			p := person(u)
			s.people[p.ID] = &p
		}
		s.names++
	})
}

// ApplyEmoji takes emoji.list.
func (s *Store) ApplyEmoji(e map[string]string) {
	s.update(func() { s.emoji, s.names = e, s.names+1 })
}

// ApplySections takes users.channelSections.list, in Slack's linked
// order (each names the next).
func (s *Store) ApplySections(ss []slack.Section) {
	byID := map[string]slack.Section{}
	pointed := map[string]bool{}
	for _, x := range ss {
		byID[x.ID] = x
		pointed[x.Next] = true
	}
	var out []Section
	for _, x := range ss {
		if pointed[x.ID] {
			continue
		}
		for id := x.ID; id != "" && len(out) < len(ss); id = byID[id].Next {
			y, ok := byID[id]
			if !ok {
				break
			}
			out = append(out, Section{ID: y.ID, Name: y.Name, Emoji: y.Emoji, Type: y.Type,
				Convs: y.Channels.IDs, Collapsed: y.Collapsed})
		}
		break
	}
	if len(out) < len(ss) {
		// The links didn't hold together: keep Slack's order as sent.
		out = out[:0]
		for _, y := range ss {
			out = append(out, Section{ID: y.ID, Name: y.Name, Emoji: y.Emoji, Type: y.Type,
				Convs: y.Channels.IDs, Collapsed: y.Collapsed})
		}
	}
	s.update(func() { s.sections = out })
}

// SetWindow takes a page from History (newest first, as Slack sends it)
// and holds it oldest first: replacing the window, or with older, going
// in front of what's held.
func (s *Store) SetWindow(conv string, newestFirst []slack.Message, more, older bool) {
	msgs := make([]slack.Message, len(newestFirst))
	for i, m := range newestFirst {
		msgs[len(msgs)-1-i] = m
	}
	s.update(func() {
		w := s.windows[conv]
		if w == nil {
			w = &Window{}
			s.windows[conv] = w
		}
		if older {
			w.Msgs = append(msgs, w.Msgs...)
		} else {
			w.Msgs, w.Newer, w.stale = msgs, false, false
		}
		w.More = more
		s.touch(conv)
	})
}

// touch marks conv as just looked at and lets go of the least recently
// looked at windows past keepWindows. Call with the write lock held.
func (s *Store) touch(conv string) {
	s.clock++
	if w := s.windows[conv]; w != nil {
		w.used = s.clock
	}
	for len(s.windows) > keepWindows {
		oldest, at := "", ^uint64(0)
		for id, w := range s.windows {
			if w.used < at {
				oldest, at = id, w.used
			}
		}
		delete(s.windows, oldest)
	}
}

// Refresh fetches conv's latest page again, keeping older ones held.
func (s *Store) Refresh(ctx context.Context, c *slack.Client, conv string) error {
	msgs, _, err := c.History(ctx, conv, "", 30)
	if err != nil {
		return err
	}
	s.update(func() {
		w := s.windows[conv]
		if w == nil {
			w = &Window{More: true}
			s.windows[conv] = w
		}
		// Keep what's older than the page; take the page as the rest.
		oldest := ""
		if len(msgs) > 0 {
			oldest = msgs[len(msgs)-1].TS
		}
		keep := w.Msgs[:0:0]
		for _, m := range w.Msgs {
			if m.TS < oldest {
				keep = append(keep, m)
			}
		}
		// A full page that doesn't reach what's held leaves a gap: let
		// what's held go, and scrolling up fetches the gap.
		if gap := len(msgs) == 30 && len(keep) == len(w.Msgs) && len(keep) > 0; gap {
			keep, w.More = keep[:0], true
		}
		for i := len(msgs) - 1; i >= 0; i-- {
			keep = append(keep, msgs[i])
		}
		w.Msgs, w.Newer, w.stale = keep, false, false
		s.touch(conv)
	})
	return nil
}

// MarkRead notes conv read up to ts, as conversations.mark will.
func (s *Store) MarkRead(conv, ts string) {
	s.update(func() {
		if c := s.convs[conv]; c != nil && ts > c.LastRead {
			c.LastRead, c.Unread, c.Mentions = ts, false, 0
		}
	})
}

// held finds the message at ts in conv's window. Call with the lock held.
func (s *Store) held(conv, ts string) (*Window, int) {
	w := s.windows[conv]
	if w == nil {
		return nil, -1
	}
	i, ok := slices.BinarySearchFunc(w.Msgs, ts, func(m slack.Message, ts string) int { return strings.Compare(m.TS, ts) })
	if !ok {
		return w, -1
	}
	return w, i
}

// Edit changes the text of the message at ts, as chat.update will.
func (s *Store) Edit(conv, ts, text string) {
	s.update(func() {
		if w, i := s.held(conv, ts); i >= 0 {
			w.Msgs[i].Text, w.Msgs[i].Edited = text, &struct{}{}
		}
		s.inThreads(conv, ts, func(m *slack.Message) { m.Text, m.Edited = text, &struct{}{} })
	})
}

// Remove lets go of the message at ts, as chat.delete will.
func (s *Store) Remove(conv, ts string) {
	s.update(func() {
		if w, i := s.held(conv, ts); i >= 0 {
			w.Msgs = slices.Delete(w.Msgs, i, i+1)
		}
		s.threadRemove(conv, ts)
	})
}
