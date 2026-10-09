package store

import (
	"context"
	"slices"
	"strings"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// Around fetches the messages either side of ts in conv into its window,
// for going to a message that isn't held (a search result, say). What's
// after ts comes too when it runs out before a page does; else the window
// stops at ts and says Newer, and the next Refresh lets it go by its gap
// rule.
func (s *Store) Around(ctx context.Context, c *slack.Client, conv, ts string) error {
	before, more, err := c.Span(ctx, conv, "", ts, 50)
	if err != nil {
		return err
	}
	after, newer, err := c.Span(ctx, conv, ts, "", 50)
	if err != nil {
		return err
	}
	if newer {
		after = nil // that's the newest page, not what follows ts: there'd be a hole
	}
	msgs := append(before, after...)
	slices.SortFunc(msgs, func(a, b slack.Message) int { return strings.Compare(a.TS, b.TS) })
	msgs = slices.CompactFunc(msgs, func(a, b slack.Message) bool { return a.TS == b.TS })
	s.update(func() {
		w := s.windows[conv]
		if w == nil {
			w = &Window{}
			s.windows[conv] = w
		}
		w.Msgs, w.More, w.Newer = msgs, more, newer
		s.touch(conv)
	})
	return nil
}

// Newest brings conv's window back to the newest messages, if Around
// left it in the past.
func (s *Store) Newest(ctx context.Context, c *slack.Client, conv string) error {
	newer := false
	s.Read(func(v View) {
		if w := v.Window(conv); w != nil {
			newer = w.Newer
		}
	})
	if !newer {
		return nil
	}
	return s.Refresh(ctx, c, conv)
}

// Holds says whether conv's window has the message at ts.
func (v View) Holds(conv, ts string) bool {
	_, i := v.s.held(conv, ts)
	return i >= 0
}
