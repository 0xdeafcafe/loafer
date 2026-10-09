package store

import (
	"maps"
	"slices"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// React adds or takes away your own reaction name on the message at ts
// here at once, as Slack's event would; the event then finds it done.
func (s *Store) React(conv, ts, name string, add bool) {
	s.update(func() {
		if w, i := s.held(conv, ts); i >= 0 {
			m := &w.Msgs[i]
			m.Reactions = react(m.Reactions, name, s.self, add)
		}
		s.inThreads(conv, ts, func(m *slack.Message) { m.Reactions = react(m.Reactions, name, s.self, add) })
	})
}

// CustomEmoji is the workspace's own emoji names, aliases too, sorted.
func (v View) CustomEmoji() []string {
	return slices.Sorted(maps.Keys(v.s.emoji))
}
