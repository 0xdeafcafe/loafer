package store

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// The DMs, Activity and Later tabs' lists. Each is fetched the first time
// its tab is opened and kept live after: DMs by the messages arriving,
// Activity and Later by fetching again when the websocket says they've
// changed, since what those events carry wasn't found.

// List is one of the tabs' lists.
type List uint8

const (
	DMList List = iota
	ActivityList
	LaterList
)

type tabs struct {
	dmLast        map[string]slack.Message // each DM's newest message, for its preview
	activity      []Activity
	saved         []Saved
	loaded, stale [3]bool
	badge         [3]int // client.counts' badges, until the lists come
}

// Fetch gets list l. Changes the websocket brings from here on mark it to
// be fetched again.
func (s *Store) Fetch(ctx context.Context, c *slack.Client, l List) error {
	s.update(func() { s.tabs.stale[l] = false })
	switch l {
	case DMList:
		return s.fetchDMs(ctx, c)
	case ActivityList:
		return s.fetchActivity(ctx, c)
	}
	return s.fetchSaved(ctx, c)
}

// applyTabs takes the websocket's word that Activity or Later changed.
func (s *Store) applyTabs(ev slack.Event) {
	l := ActivityList
	if strings.HasPrefix(ev.Type, "saved_") {
		l = LaterList
	}
	s.update(func() { s.tabs.stale[l] = s.tabs.loaded[l] })
}

// counts keeps client.counts' badges. Call with the write lock held.
func (t *tabs) counts(c slack.Counts) {
	t.badge[ActivityList], t.badge[LaterList] = slack.Badge(c.Activity), c.Saved.Uncompleted
}

// ref is a message to fetch: in conv at ts, in thread's thread if it's a reply.
type ref struct{ conv, ts, thread string }

// fill fetches what refs point at, four at a time, putting each under the
// write lock as it comes. What can't be fetched is logged and left.
func (s *Store) fill(refs []ref, get func(ref) (slack.Message, error), put func(ref, slack.Message)) {
	sem := make(chan struct{}, 4)
	seen := map[ref]bool{}
	var wg sync.WaitGroup
	for _, r := range refs {
		if seen[r] {
			continue
		}
		seen[r] = true
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			m, err := get(r)
			if err != nil {
				slog.Warn("fetch message", "conv", r.conv, "err", err)
				return
			}
			s.update(func() { put(r, m) })
		})
	}
	wg.Wait()
}

func (v View) Loaded(l List) bool { return v.s.tabs.loaded[l] }

// Stale says list l has changed since it was fetched.
func (v View) Stale(l List) bool { return v.s.tabs.stale[l] }

// Badge is the count on list l's tab: unread DMs, unread activity, saved
// items in progress. Before a list is fetched, it's client.counts' word.
func (v View) Badge(l List) int {
	n := 0
	switch {
	case l == DMList:
		for _, c := range v.s.convs {
			if c.Unread && (c.Kind == IM || c.Kind == MPIM) {
				n++
			}
		}
	case !v.s.tabs.loaded[l]:
		n = v.s.tabs.badge[l]
	case l == ActivityList:
		for _, a := range v.s.tabs.activity {
			if a.Unread {
				n++
			}
		}
	default:
		n = len(v.s.tabs.saved)
	}
	return n
}
