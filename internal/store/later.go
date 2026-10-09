package store

import (
	"context"
	"slices"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// Saved is something saved for later and not yet done.
type Saved struct {
	Type, Conv, TS string        // item_type, item_id, ts: the conversation and message, for a message
	Due            int64         // unix seconds; 0 for none
	Msg            slack.Message // the message, once known; Msg.TS is "" until then
}

func (s *Store) fetchSaved(ctx context.Context, c *slack.Client) error {
	items, err := c.Saved(ctx)
	if err != nil {
		return err
	}
	var list []Saved
	for _, it := range items {
		if it.IsArchived || it.DateCompleted > 0 || (it.State != "" && it.State != "in_progress") {
			continue
		}
		x := Saved{Type: it.Type, Conv: it.ID, TS: it.TS, Due: it.DateDue}
		if it.Message != nil {
			x.Msg = *it.Message
		}
		list = append(list, x)
	}
	var refs []ref
	s.update(func() {
		known := map[ref]slack.Message{}
		for _, x := range s.tabs.saved {
			known[ref{conv: x.Conv, ts: x.TS}] = x.Msg
		}
		for i := range list {
			x := &list[i]
			r := ref{conv: x.Conv, ts: x.TS}
			if x.Msg.TS == "" {
				x.Msg = known[r]
			}
			if w, j := s.held(x.Conv, x.TS); x.Msg.TS == "" && j >= 0 {
				x.Msg = w.Msgs[j]
			}
			// shortcut: a saved reply isn't found this way, as its thread
			// isn't known; it shows without its text.
			if x.Msg.TS == "" && x.Type == "message" {
				refs = append(refs, r)
			}
		}
		s.tabs.saved, s.tabs.loaded[LaterList] = list, true
	})
	s.fill(refs, func(r ref) (slack.Message, error) { return c.Message(ctx, r.conv, r.ts, "") }, func(r ref, m slack.Message) {
		for i := range s.tabs.saved {
			if x := &s.tabs.saved[i]; x.Conv == r.conv && x.TS == r.ts {
				x.Msg = m
			}
		}
	})
	return nil
}

// Saved is the Later tab's list, in Slack's order.
func (v View) Saved() []Saved { return v.s.tabs.saved }

// Unsave takes conv's message at ts off the Later list, as saved.update
// or saved.delete will.
func (s *Store) Unsave(conv, ts string) {
	s.update(func() {
		s.tabs.saved = slices.DeleteFunc(s.tabs.saved, func(x Saved) bool { return x.Conv == conv && x.TS == ts })
	})
}
