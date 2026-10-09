package store

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// Boot fetches what the client starts with, all at once, applying each as
// it lands. Boot and counts must work; the rest (people, emoji, sections)
// only log when they don't, and the UI makes do. A signed-out error comes
// back as is, so the caller can say so.
func (s *Store) Boot(ctx context.Context, c *slack.Client) error {
	began := time.Now()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	must := func(err error) {
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	}
	may := func(what string, err error) {
		if err != nil {
			slog.Warn("boot", "what", what, "err", err)
		}
	}
	wg.Go(func() {
		b, err := c.UserBoot(ctx)
		if err == nil {
			s.ApplyBoot(b)
			// Counts name conversations boot brought, so they go after it.
			var n slack.Counts
			if n, err = c.Counts(ctx); err == nil {
				s.ApplyCounts(n)
			}
		}
		must(err)
	})
	wg.Go(func() { may("users", c.Users(ctx, s.ApplyPeople)) })
	wg.Go(func() {
		e, err := c.Emoji(ctx)
		if err == nil {
			s.ApplyEmoji(e)
		}
		may("emoji", err)
	})
	wg.Go(func() {
		ss, err := c.Sections(ctx)
		if err == nil {
			s.ApplySections(ss)
		}
		may("sections", err)
	})
	wg.Go(func() {
		gs, err := c.UserGroups(ctx)
		if err == nil {
			s.ApplyGroups(gs)
		}
		may("usergroups", err)
	})
	wg.Wait()
	if len(errs) == 0 {
		s.booted.Store(true)
	}
	slog.Info("boot", "ms", time.Since(began).Milliseconds(), "ok", len(errs) == 0)
	return errors.Join(errs...)
}

// Open fetches conv's latest page into its window, unless one's held.
func (s *Store) Open(ctx context.Context, c *slack.Client, conv string) error {
	held := false
	s.update(func() {
		held = s.windows[conv] != nil
		s.touch(conv)
	})
	if held {
		return s.Refresh(ctx, c, conv) // what's held may be from the cache
	}
	msgs, more, err := c.History(ctx, conv, "", 100)
	if err != nil {
		return err
	}
	s.SetWindow(conv, msgs, more, false)
	return nil
}

// Older fetches the page before the oldest message held for conv, if
// Slack has one.
func (s *Store) Older(ctx context.Context, c *slack.Client, conv string) error {
	before := ""
	s.Read(func(v View) {
		if w := v.Window(conv); w != nil && w.More && len(w.Msgs) > 0 {
			before = w.Msgs[0].TS
		}
	})
	if before == "" {
		return nil
	}
	msgs, more, err := c.History(ctx, conv, before, 100)
	if err != nil {
		return err
	}
	s.SetWindow(conv, msgs, more, true)
	return nil
}
