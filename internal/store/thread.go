package store

import (
	"context"
	"slices"
	"strings"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// Threads are held as windows of their own, parent first then the
// replies, keyed by conversation and the parent's ts: only the few looked
// at last, and never cached, since they're fetched whole on opening.

// keepThreads is how many threads keep their messages in memory.
const keepThreads = 10

func threadKey(conv, ts string) string { return conv + "/" + ts }

// Thread is the thread at ts in conv, parent first; nil if it isn't held.
func (v View) Thread(conv, ts string) *Window { return v.s.threads[threadKey(conv, ts)] }

// OpenThread fetches the thread at ts in conv. Its parent shows at once
// if the conversation holds it, the replies once Slack answers.
func (s *Store) OpenThread(ctx context.Context, c *slack.Client, conv, ts string) error {
	k := threadKey(conv, ts)
	s.update(func() {
		if s.threads[k] == nil {
			cw, i := s.held(conv, ts)
			if i < 0 {
				return // "loading…" until Slack answers
			}
			p := cw.Msgs[i]
			p.Reactions = slices.Clone(p.Reactions) // each window changes its own
			s.putThread(k, []slack.Message{p})
		}
		s.touchThread(k)
	})
	msgs, err := c.Replies(ctx, conv, ts)
	if err != nil {
		return err
	}
	s.update(func() {
		s.putThread(k, msgs)
		s.touchThread(k)
	})
	return nil
}

// putThread holds msgs as thread k. Call with the write lock held.
func (s *Store) putThread(k string, msgs []slack.Message) {
	if s.threads == nil {
		s.threads = map[string]*Window{}
	}
	if w := s.threads[k]; w != nil {
		w.Msgs = msgs
		return
	}
	s.threads[k] = &Window{Msgs: msgs}
}

// touchThread marks thread k as just looked at and lets go of the least
// recently looked at past keepThreads. Call with the write lock held.
func (s *Store) touchThread(k string) {
	s.clock++
	s.threads[k].used = s.clock
	for len(s.threads) > keepThreads {
		oldest, at := "", ^uint64(0)
		for id, w := range s.threads {
			if w.used < at {
				oldest, at = id, w.used
			}
		}
		delete(s.threads, oldest)
	}
}

// inThreads calls f with the message at ts in each held thread of conv.
// Call with the write lock held.
func (s *Store) inThreads(conv, ts string, f func(*slack.Message)) {
	for k, w := range s.threads {
		if !strings.HasPrefix(k, conv+"/") {
			continue
		}
		if i, ok := slices.BinarySearchFunc(w.Msgs, ts, byTS); ok {
			f(&w.Msgs[i])
		}
	}
}

func byTS(m slack.Message, ts string) int { return strings.Compare(m.TS, ts) }

// threadAdd files a reply in its thread, if that's held, counting it on
// the parent there as Add does on the conversation's. Call with the write
// lock held.
func (s *Store) threadAdd(conv string, m slack.Message) {
	if m.ThreadTS == "" || m.ThreadTS == m.TS {
		return
	}
	w := s.threads[threadKey(conv, m.ThreadTS)]
	if w == nil {
		return
	}
	m.Reactions = slices.Clone(m.Reactions)
	i, found := slices.BinarySearchFunc(w.Msgs, m.TS, byTS)
	if found {
		w.Msgs[i] = m
		return
	}
	w.Msgs = slices.Insert(w.Msgs, i, m)
	if p := &w.Msgs[0]; p.TS == m.ThreadTS && p.LatestReply < m.TS {
		p.ReplyCount++
		p.LatestReply = m.TS
	}
}

// threadSet replaces m wherever a held thread of conv has it, as
// message_changed does. Call with the write lock held.
func (s *Store) threadSet(conv string, m slack.Message) {
	s.inThreads(conv, m.TS, func(x *slack.Message) {
		*x = m
		x.Reactions = slices.Clone(m.Reactions)
	})
}

// threadRemove lets go of the message at ts in conv's held threads. Call
// with the write lock held.
func (s *Store) threadRemove(conv, ts string) {
	for k, w := range s.threads {
		if !strings.HasPrefix(k, conv+"/") {
			continue
		}
		if i, ok := slices.BinarySearchFunc(w.Msgs, ts, byTS); ok {
			w.Msgs = slices.Delete(w.Msgs, i, i+1)
		}
	}
}
