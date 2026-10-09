package store

import (
	"cmp"
	"context"
	"slices"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// dmPreviews is how many DMs, newest first, have their latest message
// fetched for the list. shortcut: older ones show none until something
// arrives in them or they're opened; fetch them as they scroll into view
// if that's missed.
const dmPreviews = 25

func (s *Store) fetchDMs(ctx context.Context, c *slack.Client) error {
	var refs []ref
	s.Read(func(v View) {
		ids := v.DMs()
		for _, id := range ids[:min(dmPreviews, len(ids))] {
			if _, ok := v.LastMsg(id); !ok {
				refs = append(refs, ref{conv: id})
			}
		}
	})
	s.fill(refs, func(r ref) (slack.Message, error) {
		msgs, _, err := c.History(ctx, r.conv, "", 1)
		if len(msgs) == 0 {
			return slack.Message{}, err
		}
		return msgs[0], err
	}, func(r ref, m slack.Message) { s.tabs.sawDM(r.conv, m) })
	s.update(func() { s.tabs.loaded[DMList] = true })
	return nil
}

// sawDM keeps m as conv's preview if it's the newest yet. Call with the
// write lock held.
func (t *tabs) sawDM(conv string, m slack.Message) {
	if m.TS == "" || m.TS < t.dmLast[conv].TS {
		return
	}
	if t.dmLast == nil {
		t.dmLast = map[string]slack.Message{}
	}
	t.dmLast[conv] = m
}

// DMs is your DMs and group DMs, newest first.
func (v View) DMs() []string {
	var ids []string
	for id, c := range v.s.convs {
		if c.Kind == IM || c.Kind == MPIM {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b string) int {
		return cmp.Or(cmp.Compare(v.s.convs[b].Latest, v.s.convs[a].Latest), cmp.Compare(a, b))
	})
	return ids
}

// LastMsg is conv's newest message, from its window if that's held, else
// as last seen.
func (v View) LastMsg(conv string) (slack.Message, bool) {
	m, ok := v.s.tabs.dmLast[conv]
	if w := v.s.windows[conv]; w != nil && len(w.Msgs) > 0 && w.Msgs[len(w.Msgs)-1].TS >= m.TS {
		return w.Msgs[len(w.Msgs)-1], true
	}
	return m, ok
}
