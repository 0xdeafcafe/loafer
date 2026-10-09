package store

import (
	"slices"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// What the message actions change here at once, as Slack's events will
// when they come (docs/ui.md, Message actions menu).

// MarkUnread moves conv's read marker back to ts, as conversations.mark
// will, so the messages after it are unread again.
func (s *Store) MarkUnread(conv, ts string) {
	s.update(func() {
		if c := s.convs[conv]; c != nil {
			c.LastRead, c.Unread = ts, true
		}
	})
}

// SetPinned notes the message at ts in conv pinned or not.
func (s *Store) SetPinned(conv, ts string, pinned bool) {
	set := func(m *slack.Message) {
		m.PinnedTo = slices.DeleteFunc(slices.Clone(m.PinnedTo), func(c string) bool { return c == conv }) // a thread's copy shares the array
		if pinned {
			m.PinnedTo = append(m.PinnedTo, conv)
		}
	}
	s.update(func() {
		if w, i := s.held(conv, ts); i >= 0 {
			set(&w.Msgs[i])
		}
		s.inThreads(conv, ts, set)
	})
}

// IsSaved says whether the message is on the Later list. It can't know
// before the list has been fetched (Loaded).
func (v View) IsSaved(conv, ts string) bool {
	return slices.ContainsFunc(v.s.tabs.saved, func(x Saved) bool { return x.Type == "message" && x.Conv == conv && x.TS == ts })
}

// SaveLater puts msg on the Later list, due at the unix time due (0 for
// none), if the list is held; else the fetch will find it.
func (s *Store) SaveLater(conv string, msg slack.Message, due int64) {
	s.update(func() {
		if !s.tabs.loaded[LaterList] {
			return
		}
		at := slices.IndexFunc(s.tabs.saved, func(x Saved) bool { return x.Type == "message" && x.Conv == conv && x.TS == msg.TS })
		switch {
		case at >= 0:
			if due > 0 {
				s.tabs.saved[at].Due = due
			}
		default:
			msg.Reactions = slices.Clone(msg.Reactions) // each holder changes its own
			s.tabs.saved = slices.Insert(s.tabs.saved, 0, Saved{Type: "message", Conv: conv, TS: msg.TS, Due: due, Msg: msg})
		}
	})
}
