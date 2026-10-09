package store

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// The cache is what the first frame is drawn from: everything the sidebar
// needs and the last messages of the conversations looked at lately. It
// holds no credentials. One file a workspace, written behind.

const cacheMsgs = 50 // messages kept per window in the cache

type snapshot struct {
	Self     string                     `json:"self"`
	Team     slack.Team                 `json:"team"`
	People   []Person                   `json:"people"`
	Convs    []Conv                     `json:"convs"`
	Sections []Section                  `json:"sections"`
	Emoji    map[string]string          `json:"emoji"`
	Windows  map[string][]slack.Message `json:"windows"`
}

// CachePath is where teamID's cache lives.
func CachePath(teamID string) string {
	d, _ := os.UserCacheDir()
	return filepath.Join(d, "loafer", teamID, "state.json")
}

// Load reads a cache written by Save; a missing one is no error.
func (s *Store) Load(path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var snap snapshot
	if err := jsonx.Unmarshal(b, &snap); err != nil {
		return err
	}
	s.update(func() {
		s.self, s.team, s.sections, s.emoji = snap.Self, snap.Team, snap.Sections, snap.Emoji
		if s.emoji == nil {
			s.emoji = map[string]string{}
		}
		s.names++
		for i := range snap.People {
			s.people[snap.People[i].ID] = &snap.People[i]
		}
		for i := range snap.Convs {
			s.convs[snap.Convs[i].ID] = &snap.Convs[i]
		}
		for id, msgs := range snap.Windows {
			s.windows[id] = &Window{Msgs: msgs, More: true, stale: true} // it's been a while
			s.touch(id)
		}
	})
	return nil
}

// Save writes the cache to path, atomically.
func (s *Store) Save(path string) error {
	snap := snapshot{Windows: map[string][]slack.Message{}}
	s.mu.RLock()
	snap.Self, snap.Team, snap.Sections, snap.Emoji = s.self, s.team, s.sections, s.emoji
	for _, p := range s.people {
		snap.People = append(snap.People, *p)
	}
	for _, c := range s.convs {
		snap.Convs = append(snap.Convs, *c)
	}
	for id, w := range s.windows {
		snap.Windows[id] = w.Msgs[max(0, len(w.Msgs)-cacheMsgs):]
	}
	b, err := jsonx.Marshal(snap)
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WriteBehind saves to path a little after changes stop arriving (and at
// most every so often while they keep coming), until ctx ends, when it
// saves once more.
func (s *Store) WriteBehind(ctx context.Context, path string) {
	const quiet, most = 2 * time.Second, 30 * time.Second
	var t <-chan time.Time
	var first time.Time
	save := func() {
		if err := s.Save(path); err != nil {
			slog.Warn("cache save", "err", err)
		}
		t, first = nil, time.Time{}
	}
	for {
		select {
		case <-ctx.Done():
			if t != nil {
				save()
			}
			return
		case <-s.dirty:
			if first.IsZero() {
				first = time.Now()
			}
			t = time.After(min(quiet, most-time.Since(first)))
		case <-t:
			save()
		}
	}
}
